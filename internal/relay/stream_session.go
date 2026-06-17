package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	dbmodel "github.com/lingyuins/octopus/internal/model"
	"github.com/lingyuins/octopus/internal/op/setting"
	"github.com/lingyuins/octopus/internal/server/resp"
)

var (
	errRelayConversationBusy    = errors.New("conversation already has an active generation")
	errRelayReplayWindowExpired = errors.New("relay stream replay window expired")

	overrideStreamSessionTTL       *time.Duration
	overrideStreamSessionMaxEvents *int
	overrideStreamSessionMaxBytes  *int
)

const (
	// relayStreamDoneRetention 是会话完成（Finish）后其 replay 缓冲区的保留时长。
	// 完成会话的缓冲区仅用于断线重连重放——客户端在生成结束后短时间内重连读取
	// 已生成内容。这个窗口远短于会话 TTL：之前完成会话会连同最多 16MB 的缓冲区
	// 一起驻留整个 TTL（默认 30 分钟），高并发流式下常驻内存累积到数百 MB 并
	// 触发 swap（见 issue #46）。改为最多保留 2 分钟即清理，把大缓冲的驻留时间
	// 压缩一个数量级，同时保留断线重连重放语义。
	relayStreamDoneRetention = 2 * time.Minute

	// relayStreamMaxSessions 是全局会话 map 的硬上限。超过时主动驱逐最旧的已完成
	// 会话，防止 map 在会话获取频率低、清理迟滞时无界增长。
	relayStreamMaxSessions = 4096
)

func getStreamSessionTTL() time.Duration {
	if overrideStreamSessionTTL != nil {
		return *overrideStreamSessionTTL
	}
	if v, err := setting.GetInt(dbmodel.SettingKeyStreamSessionTTLMinutes); err == nil && v > 0 {
		return time.Duration(v) * time.Minute
	}
	return 30 * time.Minute
}

func getStreamSessionMaxEvents() int {
	if overrideStreamSessionMaxEvents != nil {
		return *overrideStreamSessionMaxEvents
	}
	if v, err := setting.GetInt(dbmodel.SettingKeyStreamSessionMaxEvents); err == nil && v > 0 {
		return v
	}
	return 4096
}

func getStreamSessionMaxBytes() int {
	if overrideStreamSessionMaxBytes != nil {
		return *overrideStreamSessionMaxBytes
	}
	if v, err := setting.GetInt(dbmodel.SettingKeyStreamSessionMaxBytesMB); err == nil && v > 0 {
		return v << 20
	}
	return 4 << 20
}

type relayStreamEvent struct {
	Sequence int64
	Payload  []byte
}

type relayStreamSession struct {
	store             *relayStreamSessionStore
	key               string
	conversationID    string
	conversationScope string
	requestHash       uint64
	createdAt         time.Time

	mu               sync.RWMutex
	updatedAt        time.Time
	done             bool
	err              error
	nextSeq          int64
	droppedBeforeSeq int64
	bufferBytes      int
	events           []relayStreamEvent
	subscribers      map[chan struct{}]struct{}
}

type relayStreamSessionStore struct {
	mu                   sync.RWMutex
	byKey                map[string]*relayStreamSession
	activeByConversation map[string]string
}

var relayStreamSessions = relayStreamSessionStore{
	byKey:                make(map[string]*relayStreamSession),
	activeByConversation: make(map[string]string),
}

func buildRelayStreamSessionKey(conversationID string, requestHash uint64) string {
	return strings.TrimSpace(conversationID) + ":" + strconv.FormatUint(requestHash, 16)
}

func buildRelayConversationScope(conversationID string, apiKeyID int) string {
	return strconv.Itoa(apiKeyID) + ":" + strings.TrimSpace(conversationID)
}

func acquireRelayStreamSession(conversationID string, apiKeyID int, requestHash uint64) (*relayStreamSession, bool, error) {
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" || requestHash == 0 {
		return nil, false, nil
	}

	now := time.Now()
	store := &relayStreamSessions
	conversationScope := buildRelayConversationScope(conversationID, apiKeyID)
	key := buildRelayStreamSessionKey(conversationScope, requestHash)

	store.mu.Lock()
	defer store.mu.Unlock()

	store.cleanupLocked(now)

	if session, ok := store.byKey[key]; ok {
		return session, false, nil
	}

	if activeKey, ok := store.activeByConversation[conversationScope]; ok && activeKey != key {
		if activeSession, exists := store.byKey[activeKey]; exists && !activeSession.isDoneLocked() {
			return nil, false, errRelayConversationBusy
		}
		delete(store.activeByConversation, conversationScope)
	}

	session := &relayStreamSession{
		store:             store,
		key:               key,
		conversationID:    conversationID,
		conversationScope: conversationScope,
		requestHash:       requestHash,
		createdAt:         now,
		updatedAt:         now,
		subscribers:       make(map[chan struct{}]struct{}),
	}
	store.byKey[key] = session
	store.activeByConversation[conversationScope] = key
	store.enforceSessionLimitLocked()
	return session, true, nil
}

// enforceSessionLimitLocked 在会话总数超过 relayStreamMaxSessions 时，驱逐最旧的
// 已完成会话，防止全局 map 在清理迟滞时无界增长。调用方必须持有 store.mu 写锁。
// 只驱逐 done 会话，避免中断正在进行的生成；若全是活跃会话则不驱逐（活跃会话的
// buffer 受单会话上限约束，且会在 Finish 后释放）。
func (s *relayStreamSessionStore) enforceSessionLimitLocked() {
	if relayStreamMaxSessions <= 0 || len(s.byKey) <= relayStreamMaxSessions {
		return
	}

	type doneSession struct {
		key       string
		scope     string
		updatedAt time.Time
	}
	doneList := make([]doneSession, 0, len(s.byKey))
	for key, session := range s.byKey {
		session.mu.RLock()
		done := session.done
		updatedAt := session.updatedAt
		scope := session.conversationScope
		session.mu.RUnlock()
		if done {
			doneList = append(doneList, doneSession{key: key, scope: scope, updatedAt: updatedAt})
		}
	}
	sort.Slice(doneList, func(i, j int) bool {
		return doneList[i].updatedAt.Before(doneList[j].updatedAt)
	})

	excess := len(s.byKey) - relayStreamMaxSessions
	for i := 0; i < len(doneList) && excess > 0; i++ {
		d := doneList[i]
		session, ok := s.byKey[d.key]
		if !ok {
			continue
		}
		delete(s.byKey, d.key)
		if activeKey, ok := s.activeByConversation[d.scope]; ok && activeKey == session.key {
			delete(s.activeByConversation, d.scope)
		}
		excess--
	}
}

// doneSessionRetention 返回已完成会话条目在 map 中的保留时长：取
// relayStreamDoneRetention 与配置 TTL 的较小值。已完成会话的大缓冲在 Finish
// 时已清空，这里控制的是空壳元数据条目的清理时机，与 Finish 调度的窗口一致。
func doneSessionRetention() time.Duration {
	retention := relayStreamDoneRetention
	if ttl := getStreamSessionTTL(); ttl > 0 && ttl < retention {
		retention = ttl
	}
	return retention
}

func (s *relayStreamSessionStore) cleanupLocked(now time.Time) {
	// 保留时长在循环外读取一次，避免在持有 store 写锁期间对每个 session 重复
	// 读取 setting（map 查找 + Atoi）。清理可能遍历大量 session，每次循环都
	// 读 setting 会线性放大写锁的持有时间，阻塞所有新流式会话获取。
	retention := doneSessionRetention()
	for key, session := range s.byKey {
		session.mu.RLock()
		done := session.done
		updatedAt := session.updatedAt
		conversationScope := session.conversationScope
		sessionKey := session.key
		session.mu.RUnlock()

		if !done {
			continue
		}
		if now.Sub(updatedAt) < retention {
			continue
		}

		delete(s.byKey, key)
		if activeKey, ok := s.activeByConversation[conversationScope]; ok && activeKey == sessionKey {
			delete(s.activeByConversation, conversationScope)
		}
	}
}

func (s *relayStreamSessionStore) removeIfExpired(key string, conversationScope string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.byKey[key]
	if !ok {
		return
	}

	session.mu.RLock()
	done := session.done
	updatedAt := session.updatedAt
	sessionKey := session.key
	sessionScope := session.conversationScope
	session.mu.RUnlock()

	if !done || sessionScope != conversationScope || time.Since(updatedAt) < doneSessionRetention() {
		return
	}

	delete(s.byKey, key)
	if activeKey, ok := s.activeByConversation[sessionScope]; ok && activeKey == sessionKey {
		delete(s.activeByConversation, sessionScope)
	}
}

func (s *relayStreamSession) isDoneLocked() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.done
}

func (s *relayStreamSession) IsDone() bool {
	if s == nil {
		return true
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.done
}

func (s *relayStreamSession) AddPayload(payload []byte) []relayStreamEvent {
	if s == nil || len(payload) == 0 {
		return nil
	}

	frames := splitRelaySSEPayload(payload)
	if len(frames) == 0 {
		return nil
	}

	events := make([]relayStreamEvent, 0, len(frames))

	s.mu.Lock()
	for _, frame := range frames {
		s.nextSeq++
		event := relayStreamEvent{
			Sequence: s.nextSeq,
			Payload:  frame,
		}
		s.events = append(s.events, event)
		s.bufferBytes += len(frame)
		events = append(events, event)
	}
	s.trimEventsLocked()
	s.updatedAt = time.Now()

	subscribers := make([]chan struct{}, 0, len(s.subscribers))
	for ch := range s.subscribers {
		subscribers = append(subscribers, ch)
	}
	s.mu.Unlock()

	for _, ch := range subscribers {
		select {
		case ch <- struct{}{}:
		default:
		}
	}

	return events
}

func (s *relayStreamSession) trimEventsLocked() {
	// maxEvents / maxBytes 在循环外读取一次：trimEventsLocked 由每个流式帧的
	// AddPayload 调用，循环内重复读 setting 会在热路径上放大开销。
	maxEvents := getStreamSessionMaxEvents()
	maxBytes := getStreamSessionMaxBytes()
	for len(s.events) > 0 {
		tooManyEvents := maxEvents > 0 && len(s.events) > maxEvents
		tooManyBytes := maxBytes > 0 && s.bufferBytes > maxBytes && len(s.events) > 1
		if !tooManyEvents && !tooManyBytes {
			return
		}

		dropped := s.events[0]
		s.droppedBeforeSeq = dropped.Sequence
		s.bufferBytes -= len(dropped.Payload)
		if s.bufferBytes < 0 {
			s.bufferBytes = 0
		}
		s.events[0].Payload = nil
		s.events = s.events[1:]
	}
}

func (s *relayStreamSession) Snapshot(afterSeq int64) ([]relayStreamEvent, bool, error) {
	if s == nil {
		return nil, true, nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if afterSeq < s.droppedBeforeSeq {
		return nil, s.done, errRelayReplayWindowExpired
	}

	idx := 0
	for idx < len(s.events) && s.events[idx].Sequence <= afterSeq {
		idx++
	}

	events := make([]relayStreamEvent, 0, len(s.events)-idx)
	for ; idx < len(s.events); idx++ {
		event := s.events[idx]
		event.Payload = append([]byte(nil), event.Payload...)
		events = append(events, event)
	}

	return events, s.done, s.err
}

func (s *relayStreamSession) Subscribe() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	if s == nil {
		close(ch)
		return ch, func() {}
	}

	s.mu.Lock()
	if s.done {
		s.mu.Unlock()
		close(ch)
		return ch, func() {}
	}
	s.subscribers[ch] = struct{}{}
	s.mu.Unlock()

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			s.mu.Lock()
			if _, ok := s.subscribers[ch]; ok {
				delete(s.subscribers, ch)
				close(ch)
			}
			s.mu.Unlock()
		})
	}

	return ch, unsubscribe
}

func (s *relayStreamSession) Finish(err error) {
	if s == nil {
		return
	}

	s.mu.Lock()
	if s.done {
		s.mu.Unlock()
		return
	}
	s.done = true
	s.err = err
	s.updatedAt = time.Now()

	// 不在此处清空 events：完成的会话其 replay 缓冲仍需支持断线重连重放
	// （客户端重连后先重放已缓冲事件，再收到 terminal error）。内存控制改由
	// 缩短的保留窗口实现——见 relayStreamDoneRetention：done 会话最多保留 2 分钟
	// （而非完整 TTL 30 分钟）即被清理，把大缓冲的驻留时间压缩一个数量级
	// （见 issue #46 的内存暴涨）。

	subscribers := make([]chan struct{}, 0, len(s.subscribers))
	for ch := range s.subscribers {
		subscribers = append(subscribers, ch)
	}
	s.subscribers = make(map[chan struct{}]struct{})
	s.mu.Unlock()

	s.store.mu.Lock()
	if activeKey, ok := s.store.activeByConversation[s.conversationScope]; ok && activeKey == s.key {
		delete(s.store.activeByConversation, s.conversationScope)
	}
	s.store.mu.Unlock()

	// 用较短的完成保留窗口调度清理（取 doneRetention 与 TTL 的较小值），
	// 而非完整 TTL，缩短已完成会话条目在 map 中的驻留时间。
	retention := relayStreamDoneRetention
	if ttl := getStreamSessionTTL(); ttl > 0 && ttl < retention {
		retention = ttl
	}
	if retention > 0 {
		time.AfterFunc(retention, func() {
			s.store.removeIfExpired(s.key, s.conversationScope)
		})
	}

	for _, ch := range subscribers {
		close(ch)
	}
}

func splitRelaySSEPayload(payload []byte) [][]byte {
	trimmed := bytes.TrimLeft(payload, "\r\n")
	if len(trimmed) == 0 {
		return nil
	}

	parts := bytes.Split(trimmed, []byte("\n\n"))
	frames := make([][]byte, 0, len(parts))
	for _, part := range parts {
		frame := bytes.TrimLeft(part, "\r\n")
		if len(bytes.TrimSpace(frame)) == 0 {
			continue
		}
		cloned := append([]byte(nil), frame...)
		if !bytes.HasSuffix(cloned, []byte("\n\n")) {
			cloned = append(cloned, '\n', '\n')
		}
		frames = append(frames, cloned)
	}
	return frames
}

func formatRelaySSEEvent(sequence int64, payload []byte) []byte {
	frame := make([]byte, 0, len(payload)+32)
	frame = append(frame, []byte("id: "+strconv.FormatInt(sequence, 10)+"\n")...)
	frame = append(frame, payload...)
	if !bytes.HasSuffix(frame, []byte("\n\n")) {
		frame = append(frame, '\n', '\n')
	}
	return frame
}

func writeSSEErrorEvent(w io.Writer, message string) {
	data, _ := json.Marshal(map[string]string{"error": message})
	fmt.Fprintf(w, "event: error\ndata: %s\n\n", data)
}

func serveRelayStreamSession(c *gin.Context, req *relayRequest) {
	if req == nil || req.streamSession == nil {
		resp.Error(c, http.StatusBadRequest, "missing relay stream session")
		return
	}

	clientCtx := c.Request.Context()
	lastSeq := req.internalRequest.ResumeFromEventID
	headersWritten := false

	writeHeaders := func() {
		if headersWritten {
			return
		}
		headersWritten = true
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		c.Header("X-Accel-Buffering", "no")
		c.Header("X-Conversation-ID", req.internalRequest.ConversationID)
	}

	writeEvents := func(events []relayStreamEvent) bool {
		for _, event := range events {
			writeHeaders()
			if _, err := c.Writer.Write(formatRelaySSEEvent(event.Sequence, event.Payload)); err != nil {
				return false
			}
			c.Writer.Flush()
			lastSeq = event.Sequence
		}
		return true
	}

	sub, unsubscribe := req.streamSession.Subscribe()
	defer unsubscribe()

	for {
		events, done, sessionErr := req.streamSession.Snapshot(lastSeq)
		if errors.Is(sessionErr, errRelayReplayWindowExpired) {
			if !headersWritten {
				resp.Error(c, http.StatusConflict, sessionErr.Error())
			} else {
				writeHeaders()
				writeSSEErrorEvent(c.Writer, sessionErr.Error())
				c.Writer.Flush()
			}
			return
		}
		if len(events) > 0 {
			if !writeEvents(events) {
				return
			}
		}

		if done {
			if sessionErr != nil {
				if !headersWritten {
					statusCode := http.StatusBadGateway
					if errors.Is(sessionErr, context.DeadlineExceeded) {
						statusCode = http.StatusGatewayTimeout
					}
					resp.Error(c, statusCode, sessionErr.Error())
				} else {
					writeHeaders()
					writeSSEErrorEvent(c.Writer, sessionErr.Error())
					c.Writer.Flush()
				}
			}
			return
		}

		select {
		case <-clientCtx.Done():
			return
		case _, ok := <-sub:
			if !ok {
				continue
			}
		}
	}
}

// ActiveSessionCount returns the count of active (not yet done) stream sessions.
func ActiveSessionCount() int {
	relayStreamSessions.mu.RLock()
	defer relayStreamSessions.mu.RUnlock()
	count := 0
	for _, s := range relayStreamSessions.byKey {
		if !s.IsDone() {
			count++
		}
	}
	return count
}

// PurgeExpiredStreamSessions proactively removes finished stream sessions whose
// retention window has elapsed. It is invoked by a periodic background task so
// cleanup does not depend solely on a new session being acquired (lazy
// cleanupLocked) or on per-session AfterFunc timers. This bounds the global
// session map under sustained streaming load (see issue #46).
func PurgeExpiredStreamSessions() {
	store := &relayStreamSessions
	store.mu.Lock()
	defer store.mu.Unlock()
	store.cleanupLocked(time.Now())
}

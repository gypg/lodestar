package webauthn

import (
	"errors"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

// S-8 会话表容量与摊还清理。
//
// 入口是 saveSession，它是 BeginLogin/BeginRegistration 唯一的会话写入点；
// 另有一组 handler 级测试（handlers 包）走 HTTP 路由验证 429 映射与限流接线。

func withCleanSessions(t *testing.T) {
	t.Helper()
	sessionsMu.Lock()
	origSessions := sessions
	origPurge := lastPurgeAt
	sessions = make(map[string]*pendingSession)
	lastPurgeAt = time.Time{}
	sessionsMu.Unlock()

	origClock := sessionsClock
	t.Cleanup(func() {
		sessionsMu.Lock()
		sessions = origSessions
		lastPurgeAt = origPurge
		sessionsMu.Unlock()
		sessionsClock = origClock
	})
}

func newLiveSession() *pendingSession {
	return &pendingSession{
		data:   &webauthn.SessionData{},
		kind:   "login",
		expiry: sessionsClock().Add(sessionTTL),
	}
}

// 会话表达到上限后必须拒绝新增，而不是无限增长直到 OOM。
func TestSaveSessionRejectsBeyondCapacity(t *testing.T) {
	withCleanSessions(t)

	for i := 0; i < maxPendingSessions; i++ {
		if _, err := saveSession(newLiveSession()); err != nil {
			t.Fatalf("saveSession #%d unexpectedly failed: %v", i, err)
		}
	}

	sessionsMu.Lock()
	n := len(sessions)
	sessionsMu.Unlock()
	if n != maxPendingSessions {
		t.Fatalf("len(sessions) = %d, want %d", n, maxPendingSessions)
	}

	// 第 maxPendingSessions+1 条必须被拒。
	if _, err := saveSession(newLiveSession()); !errors.Is(err, ErrTooManySessions) {
		t.Fatalf("saveSession beyond capacity: err = %v, want ErrTooManySessions", err)
	}

	// 副作用断言：被拒时表不能继续增长（哪怕返回了 error，若仍写入就没堵住内存）。
	sessionsMu.Lock()
	n = len(sessions)
	sessionsMu.Unlock()
	if n != maxPendingSessions {
		t.Fatalf("after rejection len(sessions) = %d, want %d (rejected session must not be stored)",
			n, maxPendingSessions)
	}
}

// ★ 表满时必须强制清理再判上限，否则一批过期会话把上限永久占死，
// 合法用户再也开不出 challenge —— 把内存 DoS 换成功能 DoS。
func TestSaveSessionReclaimsExpiredWhenFull(t *testing.T) {
	withCleanSessions(t)

	base := time.Now()
	sessionsClock = func() time.Time { return base }

	// 灌满，全部是 5 分钟后过期的会话。
	for i := 0; i < maxPendingSessions; i++ {
		if _, err := saveSession(newLiveSession()); err != nil {
			t.Fatalf("saveSession #%d: %v", i, err)
		}
	}
	if _, err := saveSession(newLiveSession()); !errors.Is(err, ErrTooManySessions) {
		t.Fatalf("expected capacity rejection while full, got %v", err)
	}

	// 时间推过 TTL：此时全表都已过期，新请求必须能成功（回收后有位置）。
	sessionsClock = func() time.Time { return base.Add(sessionTTL + time.Second) }
	if _, err := saveSession(newLiveSession()); err != nil {
		t.Fatalf("saveSession after all entries expired: %v, want success (expired must be reclaimed)", err)
	}

	sessionsMu.Lock()
	n := len(sessions)
	sessionsMu.Unlock()
	// 回收掉全部过期项后只剩刚存的这一条。
	if n != 1 {
		t.Fatalf("len(sessions) = %d, want 1 (expired entries must be purged)", n)
	}
}

// ★ 表满时的**强制**清理必须独立于间隔清理存在。
//
// 上面那个测试抓不住"删掉强制清理"这个变异（M10 第一轮存活）：它把时钟直接
// 推过 TTL(5min)，而清理间隔只有 30s，`now-lastPurgeAt >= interval` 必然成立，
// 间隔清理抢先回收，强制清理被完全掩盖。
//
// 要让强制清理成为唯一出路，必须先在"临近过期但未过期"时发一次请求，把
// lastPurgeAt 刷新到 TTL 边界附近，再让时钟只前进一点点跨过 TTL —— 此时
// 全表过期但间隔未到，只有强制清理能救。缺了它，这批过期会话会把容量占死。
func TestSaveSessionForcedPurgeWhenIntervalNotElapsed(t *testing.T) {
	withCleanSessions(t)

	base := time.Now()

	// ① 灌满，全部在 base+TTL 过期。
	sessionsClock = func() time.Time { return base }
	for i := 0; i < maxPendingSessions; i++ {
		if _, err := saveSession(&pendingSession{
			data: &webauthn.SessionData{}, kind: "login", expiry: base.Add(sessionTTL),
		}); err != nil {
			t.Fatalf("fill #%d: %v", i, err)
		}
	}

	// ② 在尚未过期时发一次请求，把 lastPurgeAt 刷新到 TTL 边界附近。
	preExpiry := base.Add(sessionTTL - 10*time.Second)
	sessionsClock = func() time.Time { return preExpiry }
	if _, err := saveSession(newLiveSession()); !errors.Is(err, ErrTooManySessions) {
		t.Fatalf("while full with nothing expired: err = %v, want ErrTooManySessions", err)
	}

	sessionsMu.Lock()
	lastPurge := lastPurgeAt
	sessionsMu.Unlock()

	// ③ 只跨过 TTL 一点：全表过期，但距上次清理仅 10s < interval(30s)。
	now := base.Add(sessionTTL).Add(time.Nanosecond)
	if gap := now.Sub(lastPurge); gap >= sessionPurgeInterval {
		t.Fatalf("test setup broken: gap %v must stay below the purge interval %v",
			gap, sessionPurgeInterval)
	}

	sessionsClock = func() time.Time { return now }
	if _, err := saveSession(newLiveSession()); err != nil {
		t.Fatalf("saveSession = %v; a full table of fully expired sessions must be reclaimed "+
			"even when the purge interval has not elapsed (otherwise expired entries "+
			"permanently occupy the capacity and lock legitimate users out)", err)
	}

	sessionsMu.Lock()
	n := len(sessions)
	sessionsMu.Unlock()
	if n != 1 {
		t.Fatalf("len(sessions) = %d, want 1 after the forced purge", n)
	}
}

// 未到清理间隔时不做全表扫描（摊还），但**功能上仍不能丢会话**：
// 未过期的会话必须始终可取用。
func TestSaveSessionAmortizedPurgeKeepsLiveSessions(t *testing.T) {
	withCleanSessions(t)

	base := time.Now()
	sessionsClock = func() time.Time { return base }

	tok, err := saveSession(newLiveSession())
	if err != nil {
		t.Fatalf("saveSession: %v", err)
	}
	// 间隔内再存若干条，不应影响已存在的会话。
	for i := 0; i < 10; i++ {
		if _, err := saveSession(newLiveSession()); err != nil {
			t.Fatalf("saveSession #%d: %v", i, err)
		}
	}
	if s := takeSession(tok, "login"); s == nil {
		t.Fatal("live session was dropped by the amortized purge")
	}
}

// 过期会话最终会被清理，表不会因为"摊还"而永久堆积。
func TestSaveSessionPurgesExpiredAfterInterval(t *testing.T) {
	withCleanSessions(t)

	base := time.Now()
	sessionsClock = func() time.Time { return base }

	const n = 50
	for i := 0; i < n; i++ {
		if _, err := saveSession(newLiveSession()); err != nil {
			t.Fatalf("saveSession #%d: %v", i, err)
		}
	}
	sessionsMu.Lock()
	got := len(sessions)
	sessionsMu.Unlock()
	if got != n {
		t.Fatalf("len(sessions) = %d, want %d", got, n)
	}

	// 推过 TTL 与清理间隔，下一次 saveSession 应回收掉全部过期项。
	sessionsClock = func() time.Time { return base.Add(sessionTTL + sessionPurgeInterval + time.Second) }
	if _, err := saveSession(newLiveSession()); err != nil {
		t.Fatalf("saveSession after interval: %v", err)
	}

	sessionsMu.Lock()
	got = len(sessions)
	sessionsMu.Unlock()
	if got != 1 {
		t.Fatalf("len(sessions) = %d, want 1 (expired entries must be purged after the interval)", got)
	}
}

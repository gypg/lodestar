package balancer

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// SessionEntry 会话保持条目
type SessionEntry struct {
	ChannelID    int
	ChannelKeyID int
	Timestamp    time.Time
}

// 全局会话存储
var globalSession sync.Map // key: string -> value: *SessionEntry

// sessionKey 生成会话键：apiKeyID:requestModel
func sessionKey(apiKeyID int, requestModel string) string {
	return fmt.Sprintf("%d:%s", apiKeyID, requestModel)
}

// GetSticky 获取粘性通道（ttl 内有效）
// ttl 由 Group.SessionKeepTime 决定，返回 nil 表示无有效会话
func GetSticky(apiKeyID int, requestModel string, ttl time.Duration) *SessionEntry {
	key := sessionKey(apiKeyID, requestModel)
	v, ok := globalSession.Load(key)
	if !ok {
		return nil
	}
	entry, ok := v.(*SessionEntry)
	if !ok {
		return nil
	}

	if time.Since(entry.Timestamp) > ttl {
		// 过期，惰性清除
		globalSession.Delete(key)
		return nil
	}

	return entry
}

// SetSticky 写入/更新粘性记录
func SetSticky(apiKeyID int, requestModel string, channelID, keyID int) {
	key := sessionKey(apiKeyID, requestModel)
	globalSession.Store(key, &SessionEntry{
		ChannelID:    channelID,
		ChannelKeyID: keyID,
		Timestamp:    time.Now(),
	})
}

// RemoveAPIKeySticky deletes all sticky session entries for the given API key.
// Called when an API key is deleted to prevent globalSession from growing unbounded.
func RemoveAPIKeySticky(apiKeyID int) {
	prefix := fmt.Sprintf("%d:", apiKeyID)
	globalSession.Range(func(key, _ any) bool {
		if k, ok := key.(string); ok && len(k) > 0 && strings.HasPrefix(k, prefix) {
			globalSession.Delete(key)
		}
		return true
	})
}

// PurgeIdleEntries 删除时间戳早于 idleFor 之前的粘性会话条目。globalSession 的 key
// 含客户端请求携带的 modelName（基数不受控），缺少周期回收会导致 map 无界增长
// （见 issue #46）。粘性会话本身有 TTL 惰性失效，但只在该 (apiKey, model) 再次被
// 请求时才触发；对一次性/随机 model 名，条目会一直驻留。返回删除的条目数。
func PurgeIdleSessions(idleFor time.Duration) int {
	if idleFor <= 0 {
		return 0
	}
	now := time.Now()
	removed := 0
	globalSession.Range(func(key, value any) bool {
		entry, ok := value.(*SessionEntry)
		if !ok {
			globalSession.Delete(key)
			removed++
			return true
		}
		if now.Sub(entry.Timestamp) >= idleFor {
			globalSession.Delete(key)
			removed++
		}
		return true
	})
	return removed
}

// StickyCount returns the number of entries in the global sticky session store.
func StickyCount() int {
	count := 0
	globalSession.Range(func(key, value any) bool {
		count++
		return true
	})
	return count
}

package ratelimitstore

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gypg/lodestar/internal/utils/ratelimit"
)

var (
	requestBuckets sync.Map // "apiKeyID:modelName" -> *ratelimit.TokenBucket
	tokenBuckets   sync.Map // "apiKeyID:modelName" -> *ratelimit.TokenBucket
)

func rateLimitKey(apiKeyID int, modelName string) string {
	return fmt.Sprintf("%d:%s", apiKeyID, modelName)
}

// CheckRateLimit checks if the request is within the rate limits.
// Returns: allowed, remaining requests, retry-after seconds.
func CheckRateLimit(apiKeyID int, modelName string, rpm int, tpm int, tokenCount int) (allowed bool, remaining int, retryAfter int) {
	key := rateLimitKey(apiKeyID, modelName)

	if rpm > 0 {
		reqBucket := getOrCreateBucket(&requestBuckets, key, rpm, rpm)
		if !reqBucket.Allow() {
			return false, 0, int(reqBucket.ResetAt().Unix())
		}
	}

	if tpm > 0 {
		tokenBucket := getOrCreateBucket(&tokenBuckets, key, tpm, tpm)
		if tokenCount <= 0 {
			tokenCount = 1
		}
		if !tokenBucket.AllowN(tokenCount) {
			return false, 0, int(tokenBucket.ResetAt().Unix())
		}
	}

	if rpm > 0 {
		reqBucket := getOrCreateBucket(&requestBuckets, key, rpm, rpm)
		remaining = reqBucket.TokensRemaining()
	}
	return true, remaining, 0
}

// ConsumeTokens deducts the actual token count from the rate limit bucket after a successful request.
func ConsumeTokens(apiKeyID int, modelName string, tpm int, tokenCount int) {
	if tpm <= 0 || tokenCount <= 0 {
		return
	}
	key := rateLimitKey(apiKeyID, modelName)
	tokenBucket := getOrCreateBucket(&tokenBuckets, key, tpm, tpm)
	tokenBucket.AllowN(tokenCount)
}

func getOrCreateBucket(m *sync.Map, key string, ratePerMinute int, burst int) *ratelimit.TokenBucket {
	if v, ok := m.Load(key); ok {
		if b, ok := v.(*ratelimit.TokenBucket); ok {
			return b
		}
	}
	bucket := ratelimit.NewTokenBucket(ratePerMinute, burst)
	actual, _ := m.LoadOrStore(key, bucket)
	if b, ok := actual.(*ratelimit.TokenBucket); ok {
		return b
	}
	return bucket
}

// PurgeStaleBuckets 清理长时间未活动的限流 bucket，防止全局 map 无界增长。
// bucket 的 key 含客户端请求携带的 modelName（基数不受控），刷量/随机 model 名下只增不删。
// maxAge 为最大空闲时长，超过则删除。由 relay log flush 定时任务周期性调用。
func PurgeStaleBuckets(maxAge time.Duration) int {
	if maxAge <= 0 {
		return 0
	}
	cutoff := time.Now().Add(-maxAge)
	removed := 0
	purgeOne := func(m *sync.Map) {
		m.Range(func(k, v any) bool {
			b, ok := v.(*ratelimit.TokenBucket)
			if !ok {
				m.Delete(k)
				removed++
				return true
			}
			if b.LastUpdate().Before(cutoff) {
				m.Delete(k)
				removed++
			}
			return true
		})
	}
	purgeOne(&requestBuckets)
	purgeOne(&tokenBuckets)
	return removed
}

// RemoveAPIKeyBuckets 删除指定 API key 的所有限流 bucket（跨模型）。
// 在 API key 被删除时调用，避免其 bucket 残留驻留。
func RemoveAPIKeyBuckets(apiKeyID int) {
	if apiKeyID <= 0 {
		return
	}
	prefix := fmt.Sprintf("%d:", apiKeyID)
	removeOne := func(m *sync.Map) {
		m.Range(func(k, _ any) bool {
			if s, ok := k.(string); ok && strings.HasPrefix(s, prefix) {
				m.Delete(k)
			}
			return true
		})
	}
	removeOne(&requestBuckets)
	removeOne(&tokenBuckets)
}

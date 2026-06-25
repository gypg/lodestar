package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/setting"
	"github.com/gypg/lodestar/internal/server/resp"
	"github.com/gypg/lodestar/internal/utils/cache"
	"github.com/gypg/lodestar/internal/utils/log"
	"github.com/gypg/lodestar/internal/utils/ratelimit"
)

const loginRateLimitCleanupInterval = 10 * time.Minute

// loginRateLimitRedisKeyPrefix is the Redis key prefix for login rate limit entries.
const loginRateLimitRedisKeyPrefix = "login_ratelimit:"

func getLoginRateLimitWindow() time.Duration {
	if v, err := setting.GetInt(model.SettingKeyLoginRateLimitWindow); err == nil && v > 0 {
		return time.Duration(v) * time.Minute
	}
	return 10 * time.Minute
}

func getLoginRateLimitMaxFailed() int {
	if v, err := setting.GetInt(model.SettingKeyLoginRateLimitMaxFailed); err == nil && v > 0 {
		return v
	}
	return 5
}

type loginAttempt struct {
	FailedCount  int       `json:"failed_count"`
	BlockedUntil time.Time `json:"blocked_until"`
	LastFailedAt time.Time `json:"last_failed_at"`
}

// loginAttemptStore abstracts the storage backend for login attempts.
// When Redis is available it provides persistence across restarts;
// otherwise the in-memory implementation is used.
type loginAttemptStore interface {
	get(key string, now time.Time) (*loginAttempt, bool)
	set(key string, attempt *loginAttempt, ttl time.Duration)
	delete(key string)
}

// ---------------------------------------------------------------------------
// in-memory store (original behaviour, fallback when Redis is unavailable)
// ---------------------------------------------------------------------------

type memoryLoginStore struct {
	mu    sync.Mutex
	items map[string]*loginAttempt
}

func newMemoryLoginStore() *memoryLoginStore {
	return &memoryLoginStore{items: make(map[string]*loginAttempt)}
}

func (s *memoryLoginStore) get(key string, now time.Time) (*loginAttempt, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	attempt, ok := s.items[key]
	if !ok {
		return nil, false
	}
	if now.Sub(attempt.LastFailedAt) > getLoginRateLimitWindow() {
		delete(s.items, key)
		return nil, false
	}
	return attempt, true
}

func (s *memoryLoginStore) set(key string, attempt *loginAttempt, _ time.Duration) {
	s.mu.Lock()
	s.items[key] = attempt
	s.mu.Unlock()
}

func (s *memoryLoginStore) delete(key string) {
	s.mu.Lock()
	delete(s.items, key)
	s.mu.Unlock()
}

// purge removes all entries older than the rate-limit window.
func (s *memoryLoginStore) purge(now time.Time) {
	s.mu.Lock()
	for key, attempt := range s.items {
		if now.Sub(attempt.LastFailedAt) > getLoginRateLimitWindow() {
			delete(s.items, key)
		}
	}
	s.mu.Unlock()
}

// ---------------------------------------------------------------------------
// Redis store
// ---------------------------------------------------------------------------

type redisLoginStore struct{}

func (s *redisLoginStore) redisKey(key string) string {
	return loginRateLimitRedisKeyPrefix + key
}

func (s *redisLoginStore) get(key string, now time.Time) (*loginAttempt, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	data, err := cache.RedisClient.Get(ctx, s.redisKey(key)).Bytes()
	if err != nil {
		return nil, false
	}
	var attempt loginAttempt
	if err := json.Unmarshal(data, &attempt); err != nil {
		return nil, false
	}
	if now.Sub(attempt.LastFailedAt) > getLoginRateLimitWindow() {
		// Expired entry; delete asynchronously.
		go func() {
			_ = cache.RedisClient.Del(context.Background(), s.redisKey(key)).Err()
		}()
		return nil, false
	}
	return &attempt, true
}

func (s *redisLoginStore) set(key string, attempt *loginAttempt, ttl time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	data, err := json.Marshal(attempt)
	if err != nil {
		log.Warnf("login ratelimit: failed to marshal attempt for key %s: %v", key, err)
		return
	}
	if err := cache.RedisClient.Set(ctx, s.redisKey(key), data, ttl).Err(); err != nil {
		log.Warnf("login ratelimit: redis SET failed for key %s: %v", key, err)
	}
}

func (s *redisLoginStore) delete(key string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := cache.RedisClient.Del(ctx, s.redisKey(key)).Err(); err != nil {
		log.Warnf("login ratelimit: redis DEL failed for key %s: %v", key, err)
	}
}

// ---------------------------------------------------------------------------
// global store selection
// ---------------------------------------------------------------------------

var loginStore loginAttemptStore

func initLoginStore() {
	if cache.IsRedisAvailable() {
		loginStore = &redisLoginStore{}
		log.Infof("login rate limiter: using Redis-backed persistence")
	} else {
		loginStore = newMemoryLoginStore()
		log.Infof("login rate limiter: using in-memory store (non-persistent)")
	}
}

// LoginRateLimit returns a gin middleware that enforces per-IP login attempt limits.
func LoginRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		key := c.ClientIP()
		if key == "" {
			key = c.RemoteIP()
		}
		if isLoginBlocked(key, time.Now()) {
			resp.Error(c, http.StatusTooManyRequests, resp.ErrTooManyRequests)
			c.Abort()
			return
		}
		c.Set("login_rate_limit_key", key)
		c.Next()
	}
}

// RecordLoginFailure increments the failed-login counter for the given key.
func RecordLoginFailure(key string, now time.Time) {
	if key == "" {
		return
	}

	attempt, ok := loginStore.get(key, now)
	if !ok {
		attempt = &loginAttempt{}
	}

	attempt.FailedCount++
	attempt.LastFailedAt = now
	if attempt.FailedCount >= getLoginRateLimitMaxFailed() {
		attempt.BlockedUntil = now.Add(getLoginRateLimitWindow())
	}

	loginStore.set(key, attempt, getLoginRateLimitWindow())
}

// ClearLoginFailures removes all recorded failures for the given key.
func ClearLoginFailures(key string) {
	if key == "" {
		return
	}
	loginStore.delete(key)
}

func isLoginBlocked(key string, now time.Time) bool {
	if key == "" {
		return false
	}

	attempt, ok := loginStore.get(key, now)
	if !ok {
		return false
	}
	if !attempt.BlockedUntil.IsZero() && now.Before(attempt.BlockedUntil) {
		return true
	}
	return false
}

// startLoginRateLimitCleanup periodically purges expired entries from the
// in-memory login attempt cache. When Redis is used, Redis TTL handles
// expiry, so this goroutine is a no-op (memory store is nil-guarded).
func startLoginRateLimitCleanup() {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				// cleanup goroutine is best-effort; re-panics are not propagated
			}
		}()
		ticker := time.NewTicker(loginRateLimitCleanupInterval)
		defer ticker.Stop()
		for range ticker.C {
			memStore, ok := loginStore.(*memoryLoginStore)
			if ok {
				memStore.purge(time.Now())
			}
			// Redis store entries are auto-expired by TTL; no cleanup needed.
		}
	}()
}

func init() {
	initLoginStore()
	startLoginRateLimitCleanup()
	startEmailCodeCleanup()
}

// ---------------------------------------------------------------------------
// Email verification code rate limiter
// ---------------------------------------------------------------------------
// Limits: 3 sends per email per hour, 10 sends per IP per hour.

const (
	emailCodeMaxPerEmail = 3  // per-email hourly limit
	emailCodeMaxPerIP    = 10 // per-IP hourly limit
)

type emailCodeEntry struct {
	bucket    *ratelimit.TokenBucket
	createdAt time.Time
}

var emailCodeBuckets sync.Map // key -> *emailCodeEntry

// EmailCodeRateLimit returns a gin middleware that enforces per-email and per-IP
// send limits for the /send-email-code endpoint.
func EmailCodeRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Email string `json:"email"`
		}
		// Bind JSON to read the email. If binding fails the handler will
		// reject anyway, so just pass through.
		_ = c.ShouldBindJSON(&req)

		ip := c.ClientIP()
		if ip == "" {
			ip = c.RemoteIP()
		}

		email := strings.ToLower(strings.TrimSpace(req.Email))

		// IP-level limit (always enforced).
		ipKey := fmt.Sprintf("emailcode:ip:%s", ip)
		ipEntry := getOrCreateEmailCodeEntry(ipKey, emailCodeMaxPerIP)
		if !ipEntry.bucket.Allow() {
			resp.Error(c, http.StatusTooManyRequests, resp.ErrTooManyRequests)
			c.Abort()
			return
		}

		// Email-level limit (only when email is non-empty).
		if email != "" {
			emailKey := fmt.Sprintf("emailcode:email:%s", email)
			emailEntry := getOrCreateEmailCodeEntry(emailKey, emailCodeMaxPerEmail)
			if !emailEntry.bucket.Allow() {
				resp.Error(c, http.StatusTooManyRequests, resp.ErrTooManyRequests)
				c.Abort()
				return
			}
		}

		c.Next()
	}
}

func getOrCreateEmailCodeEntry(key string, ratePerHour int) *emailCodeEntry {
	if v, ok := emailCodeBuckets.Load(key); ok {
		return v.(*emailCodeEntry)
	}
	entry := &emailCodeEntry{
		bucket:    ratelimit.NewTokenBucket(ratePerHour, ratePerHour),
		createdAt: time.Now(),
	}
	actual, _ := emailCodeBuckets.LoadOrStore(key, entry)
	return actual.(*emailCodeEntry)
}

// startEmailCodeCleanup periodically removes expired email-code rate-limit
// entries to prevent unbounded memory growth.
func startEmailCodeCleanup() {
	const emailCodeEntryTTL = 2 * time.Hour
	go func() {
		defer func() {
			if r := recover(); r != nil {
				// cleanup goroutine is best-effort
			}
		}()
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			now := time.Now()
			emailCodeBuckets.Range(func(key, value any) bool {
				entry := value.(*emailCodeEntry)
				if now.Sub(entry.createdAt) > emailCodeEntryTTL {
					emailCodeBuckets.Delete(key)
				}
				return true
			})
		}
	}()
}

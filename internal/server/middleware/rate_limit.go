package middleware

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gypg/lodestar/internal/model"
	"github.com/gypg/lodestar/internal/op/setting"
	"github.com/gypg/lodestar/internal/server/resp"
	"github.com/gypg/lodestar/internal/utils/ratelimit"
)

const loginRateLimitCleanupInterval = 10 * time.Minute

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
	FailedCount  int
	BlockedUntil time.Time
	LastFailedAt time.Time
}

var loginAttemptCache = struct {
	sync.Mutex
	items map[string]*loginAttempt
}{
	items: make(map[string]*loginAttempt),
}

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

func RecordLoginFailure(key string, now time.Time) {
	if key == "" {
		return
	}

	loginAttemptCache.Lock()
	defer loginAttemptCache.Unlock()

	attempt, ok := loginAttemptCache.items[key]
	if !ok || now.Sub(attempt.LastFailedAt) > getLoginRateLimitWindow() {
		attempt = &loginAttempt{}
		loginAttemptCache.items[key] = attempt
	}

	attempt.FailedCount++
	attempt.LastFailedAt = now
	if attempt.FailedCount >= getLoginRateLimitMaxFailed() {
		attempt.BlockedUntil = now.Add(getLoginRateLimitWindow())
	}
}

func ClearLoginFailures(key string) {
	if key == "" {
		return
	}
	loginAttemptCache.Lock()
	delete(loginAttemptCache.items, key)
	loginAttemptCache.Unlock()
}

func isLoginBlocked(key string, now time.Time) bool {
	if key == "" {
		return false
	}

	loginAttemptCache.Lock()
	defer loginAttemptCache.Unlock()

	attempt, ok := loginAttemptCache.items[key]
	if !ok {
		return false
	}
	if !attempt.BlockedUntil.IsZero() && now.Before(attempt.BlockedUntil) {
		return true
	}
	if now.Sub(attempt.LastFailedAt) > getLoginRateLimitWindow() {
		delete(loginAttemptCache.items, key)
		return false
	}
	return false
}

// startLoginRateLimitCleanup periodically purges expired entries from the login attempt cache.
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
			now := time.Now()
			loginAttemptCache.Lock()
			for key, attempt := range loginAttemptCache.items {
				if now.Sub(attempt.LastFailedAt) > getLoginRateLimitWindow() {
					delete(loginAttemptCache.items, key)
				}
			}
			loginAttemptCache.Unlock()
		}
	}()
}

func init() {
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

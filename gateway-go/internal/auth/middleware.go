// Package auth rate limiting for token auth attempts.
package auth

import (
	"net/http"
	"strings"
	"sync"
	"time"
)

// AuthRateLimiter tracks failed auth attempts per IP with a sliding window.
type AuthRateLimiter struct {
	mu          sync.Mutex
	failures    map[string]*ipFailures
	maxFailures int
	windowMs    int64
	lockoutMs   int64
	stopCh      chan struct{}
}

type ipFailures struct {
	count    int
	firstAt  int64 // unix ms
	lockedAt int64 // unix ms; 0 = not locked
}

// NewAuthRateLimiter creates a rate limiter for auth failures.
// maxFailures: max failures before lockout. windowMs: rolling window. lockoutMs: lockout duration.
func NewAuthRateLimiter(maxFailures int, windowMs, lockoutMs int64) *AuthRateLimiter {
	rl := &AuthRateLimiter{
		failures:    make(map[string]*ipFailures),
		maxFailures: maxFailures,
		windowMs:    windowMs,
		lockoutMs:   lockoutMs,
		stopCh:      make(chan struct{}),
	}
	go rl.gcLoop()
	return rl
}

// Close stops background GC.
func (rl *AuthRateLimiter) Close() {
	select {
	case <-rl.stopCh:
	default:
		close(rl.stopCh)
	}
}

func (rl *AuthRateLimiter) gcLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-rl.stopCh:
			return
		case <-ticker.C:
			rl.mu.Lock()
			now := time.Now().UnixMilli()
			for ip, f := range rl.failures {
				if f.lockedAt > 0 && now-f.lockedAt > rl.lockoutMs {
					delete(rl.failures, ip)
				} else if now-f.firstAt > rl.windowMs {
					delete(rl.failures, ip)
				}
			}
			rl.mu.Unlock()
		}
	}
}

// Check returns whether the IP is allowed to attempt auth.
func (rl *AuthRateLimiter) Check(ip string) (allowed bool, retryAfterMs int64) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	f := rl.failures[ip]
	if f == nil {
		return true, 0
	}
	now := time.Now().UnixMilli()
	if f.lockedAt > 0 {
		remaining := rl.lockoutMs - (now - f.lockedAt)
		if remaining > 0 {
			return false, remaining
		}
		delete(rl.failures, ip)
		return true, 0
	}
	if now-f.firstAt > rl.windowMs {
		delete(rl.failures, ip)
		return true, 0
	}
	return true, 0
}

// maxRateLimitEntries caps the failure tracking map to prevent unbounded
// growth during sustained brute-force attacks.
const maxRateLimitEntries = 10000

// RecordFailure records a failed auth attempt.
func (rl *AuthRateLimiter) RecordFailure(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now().UnixMilli()

	if len(rl.failures) >= maxRateLimitEntries {
		if _, exists := rl.failures[ip]; !exists {
			return
		}
	}

	f := rl.failures[ip]
	if f == nil {
		rl.failures[ip] = &ipFailures{count: 1, firstAt: now}
		return
	}
	if now-f.firstAt > rl.windowMs {
		f.count = 1
		f.firstAt = now
		f.lockedAt = 0
		return
	}
	f.count++
	if f.count >= rl.maxFailures {
		f.lockedAt = now
	}
}

// Reset clears failure tracking for an IP.
func (rl *AuthRateLimiter) Reset(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.failures, ip)
}

// GetBearerToken extracts a Bearer token from an HTTP request.
func GetBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return ""
	}
	const prefix = "Bearer "
	if len(auth) > len(prefix) && strings.EqualFold(auth[:len(prefix)], prefix) {
		return auth[len(prefix):]
	}
	return ""
}

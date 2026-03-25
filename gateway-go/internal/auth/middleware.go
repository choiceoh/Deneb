// Package auth middleware wires token/password auth into HTTP routes and WebSocket handshakes.
//
// This mirrors src/gateway/server/http-auth.ts and src/gateway/auth/auth.ts.
package auth

import (
	"crypto/subtle"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// AuthMode determines how the gateway authenticates clients.
type AuthMode string

const (
	AuthModeNone     AuthMode = "none"
	AuthModeToken    AuthMode = "token"
	AuthModePassword AuthMode = "password"
)

// ResolvedAuth holds the resolved authentication configuration for the gateway.
type ResolvedAuth struct {
	Mode           AuthMode `json:"mode"`
	Token          string   `json:"-"` // never serialized
	Password       string   `json:"-"`
	AllowTailscale bool     `json:"allowTailscale"`
}

// AuthResult describes the outcome of an authentication attempt.
type AuthResult struct {
	OK           bool   `json:"ok"`
	Method       string `json:"method,omitempty"` // "none", "token", "password", "local"
	User         string `json:"user,omitempty"`
	Reason       string `json:"reason,omitempty"`
	RateLimited  bool   `json:"rateLimited,omitempty"`
	RetryAfterMs int64  `json:"retryAfterMs,omitempty"`
}

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
		// Lockout expired.
		delete(rl.failures, ip)
		return true, 0
	}
	// Window expired, reset.
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

	// Prevent unbounded map growth under DDoS.
	if len(rl.failures) >= maxRateLimitEntries {
		if _, exists := rl.failures[ip]; !exists {
			return // silently drop new entries when at capacity
		}
	}

	f := rl.failures[ip]
	if f == nil {
		rl.failures[ip] = &ipFailures{count: 1, firstAt: now}
		return
	}
	// Reset if window expired.
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

// Authorize performs the authentication check against resolved auth config.
// bearerToken is extracted from the Authorization header or connect params.
// password is from connect params (WS) or basic auth (HTTP).
func Authorize(resolved *ResolvedAuth, bearerToken, password, remoteIP string, rateLimiter *AuthRateLimiter) AuthResult {
	// Mode: none — allow all.
	if resolved.Mode == AuthModeNone {
		return AuthResult{OK: true, Method: "none"}
	}

	// Local direct requests always pass.
	if isLoopback(remoteIP) {
		return AuthResult{OK: true, Method: "local"}
	}

	// Rate limit check.
	if rateLimiter != nil {
		allowed, retryMs := rateLimiter.Check(remoteIP)
		if !allowed {
			return AuthResult{OK: false, Reason: "rate limited", RateLimited: true, RetryAfterMs: retryMs}
		}
	}

	// Token auth.
	if resolved.Mode == AuthModeToken && resolved.Token != "" {
		if bearerToken != "" && constantTimeEqual(bearerToken, resolved.Token) {
			if rateLimiter != nil {
				rateLimiter.Reset(remoteIP)
			}
			return AuthResult{OK: true, Method: "token"}
		}
	}

	// Password auth.
	if resolved.Mode == AuthModePassword && resolved.Password != "" {
		if password != "" && constantTimeEqual(password, resolved.Password) {
			if rateLimiter != nil {
				rateLimiter.Reset(remoteIP)
			}
			return AuthResult{OK: true, Method: "password"}
		}
	}

	// Failure.
	if rateLimiter != nil {
		rateLimiter.RecordFailure(remoteIP)
	}
	return AuthResult{OK: false, Reason: "invalid credentials"}
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

// RemoteIP extracts the client IP from an HTTP request.
// Checks X-Forwarded-For first (first entry), then RemoteAddr.
func RemoteIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i > 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// isLoopback returns true if the IP is a loopback address.
func isLoopback(ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	return parsed.IsLoopback()
}

// constantTimeEqual compares two strings in constant time using crypto/subtle.
func constantTimeEqual(a, b string) bool {
	// subtle.ConstantTimeCompare handles length differences in constant time.
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

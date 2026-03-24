package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAuthorize_ModeNone(t *testing.T) {
	resolved := &ResolvedAuth{Mode: AuthModeNone}
	result := Authorize(resolved, "", "", "1.2.3.4", nil)
	if !result.OK {
		t.Error("mode=none should allow all")
	}
	if result.Method != "none" {
		t.Errorf("expected method=none, got %q", result.Method)
	}
}

func TestAuthorize_LocalDirect(t *testing.T) {
	resolved := &ResolvedAuth{Mode: AuthModeToken, Token: "secret"}
	result := Authorize(resolved, "", "", "127.0.0.1", nil)
	if !result.OK {
		t.Error("loopback should be allowed")
	}
	if result.Method != "local" {
		t.Errorf("expected method=local, got %q", result.Method)
	}
}

func TestAuthorize_IPv6Loopback(t *testing.T) {
	resolved := &ResolvedAuth{Mode: AuthModeToken, Token: "secret"}
	result := Authorize(resolved, "", "", "::1", nil)
	if !result.OK {
		t.Error("IPv6 loopback should be allowed")
	}
}

func TestAuthorize_ValidToken(t *testing.T) {
	resolved := &ResolvedAuth{Mode: AuthModeToken, Token: "my-secret-token"}
	result := Authorize(resolved, "my-secret-token", "", "10.0.0.1", nil)
	if !result.OK {
		t.Error("valid token should be accepted")
	}
	if result.Method != "token" {
		t.Errorf("expected method=token, got %q", result.Method)
	}
}

func TestAuthorize_InvalidToken(t *testing.T) {
	resolved := &ResolvedAuth{Mode: AuthModeToken, Token: "my-secret-token"}
	result := Authorize(resolved, "wrong-token", "", "10.0.0.1", nil)
	if result.OK {
		t.Error("invalid token should be rejected")
	}
}

func TestAuthorize_ValidPassword(t *testing.T) {
	resolved := &ResolvedAuth{Mode: AuthModePassword, Password: "my-password"}
	result := Authorize(resolved, "", "my-password", "10.0.0.1", nil)
	if !result.OK {
		t.Error("valid password should be accepted")
	}
	if result.Method != "password" {
		t.Errorf("expected method=password, got %q", result.Method)
	}
}

func TestAuthorize_InvalidPassword(t *testing.T) {
	resolved := &ResolvedAuth{Mode: AuthModePassword, Password: "my-password"}
	result := Authorize(resolved, "", "wrong", "10.0.0.1", nil)
	if result.OK {
		t.Error("invalid password should be rejected")
	}
}

func TestAuthRateLimiter_Lockout(t *testing.T) {
	rl := NewAuthRateLimiter(3, 60000, 10000)
	defer rl.Close()

	ip := "1.2.3.4"
	rl.RecordFailure(ip)
	rl.RecordFailure(ip)
	rl.RecordFailure(ip) // triggers lockout

	allowed, retryMs := rl.Check(ip)
	if allowed {
		t.Error("should be locked out after 3 failures")
	}
	if retryMs <= 0 {
		t.Error("retryMs should be positive")
	}
}

func TestAuthRateLimiter_Reset(t *testing.T) {
	rl := NewAuthRateLimiter(3, 60000, 10000)
	defer rl.Close()

	ip := "1.2.3.4"
	rl.RecordFailure(ip)
	rl.RecordFailure(ip)
	rl.RecordFailure(ip)

	rl.Reset(ip)
	allowed, _ := rl.Check(ip)
	if !allowed {
		t.Error("should be allowed after reset")
	}
}

func TestGetBearerToken(t *testing.T) {
	tests := []struct {
		header string
		want   string
	}{
		{"Bearer my-token", "my-token"},
		{"bearer MY-TOKEN", "MY-TOKEN"},
		{"Basic abc123", ""},
		{"", ""},
		{"Bearer ", ""},
	}
	for _, tt := range tests {
		r := httptest.NewRequest("GET", "/", nil)
		if tt.header != "" {
			r.Header.Set("Authorization", tt.header)
		}
		got := GetBearerToken(r)
		if got != tt.want {
			t.Errorf("GetBearerToken(%q) = %q, want %q", tt.header, got, tt.want)
		}
	}
}

func TestRemoteIP(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "192.168.1.1:12345"
	if got := RemoteIP(r); got != "192.168.1.1" {
		t.Errorf("RemoteIP = %q, want 192.168.1.1", got)
	}

	r.Header.Set("X-Forwarded-For", "10.0.0.1, 192.168.1.1")
	if got := RemoteIP(r); got != "10.0.0.1" {
		t.Errorf("RemoteIP with XFF = %q, want 10.0.0.1", got)
	}
}

func TestDefaultScopesStrings(t *testing.T) {
	scopes := DefaultScopesStrings(RoleOperator)
	if len(scopes) != len(DefaultScopes(RoleOperator)) {
		t.Errorf("operator scope count mismatch: got %d, want %d", len(scopes), len(DefaultScopes(RoleOperator)))
	}
	scopes = DefaultScopesStrings(RoleViewer)
	if len(scopes) != len(DefaultScopes(RoleViewer)) {
		t.Errorf("viewer scope count mismatch: got %d, want %d", len(scopes), len(DefaultScopes(RoleViewer)))
	}
}

func TestConstantTimeEqual(t *testing.T) {
	if !constantTimeEqual("hello", "hello") {
		t.Error("equal strings should match")
	}
	if constantTimeEqual("hello", "world") {
		t.Error("different strings should not match")
	}
	if constantTimeEqual("hello", "hell") {
		t.Error("different length strings should not match")
	}
}

func TestAuthRateLimiter_AllowedBeforeLockout(t *testing.T) {
	rl := NewAuthRateLimiter(5, 60000, 10000)
	defer rl.Close()

	ip := "1.2.3.4"
	for i := 0; i < 4; i++ {
		rl.RecordFailure(ip)
	}
	allowed, _ := rl.Check(ip)
	if !allowed {
		t.Error("should still be allowed before reaching max failures")
	}
}

func TestAuthorize_WithRateLimiter(t *testing.T) {
	resolved := &ResolvedAuth{Mode: AuthModeToken, Token: "secret"}
	rl := NewAuthRateLimiter(2, 60000, 10000)
	defer rl.Close()

	ip := "10.0.0.1"

	// Two failures should trigger lockout.
	Authorize(resolved, "wrong", "", ip, rl)
	Authorize(resolved, "wrong", "", ip, rl)

	result := Authorize(resolved, "secret", "", ip, rl)
	if result.OK {
		t.Error("should be rate limited")
	}
	if !result.RateLimited {
		t.Error("should indicate rate limited")
	}
}

// Prevent the unused import error for http and time.
var _ = http.StatusOK
var _ = time.Now

package auth

import (
	"net/http/httptest"
	"testing"
)

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

package types

import (
	"testing"
	"time"
)

func TestIsSessionExpired(t *testing.T) {
	now := time.Now().UnixMilli()
	tests := []struct {
		name      string
		createdAt int64
		policy    SessionResetPolicy
		want      bool
	}{
		{"no age limit", now - 100000, SessionResetPolicy{MaxAgeMs: 0}, false},
		{"zero createdAt", 0, SessionResetPolicy{MaxAgeMs: 1000}, false},
		{"not expired", now - 500, SessionResetPolicy{MaxAgeMs: 1000}, false},
		{"expired", now - 2000, SessionResetPolicy{MaxAgeMs: 1000}, true},
		{"negative maxAge", now - 100, SessionResetPolicy{MaxAgeMs: -1}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IsSessionExpired(tc.createdAt, tc.policy)
			if got != tc.want {
				t.Errorf("IsSessionExpired() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIsSessionIdle(t *testing.T) {
	now := time.Now().UnixMilli()
	tests := []struct {
		name      string
		updatedAt int64
		policy    SessionResetPolicy
		want      bool
	}{
		{"no idle limit", now - 100000, SessionResetPolicy{MaxIdleMs: 0}, false},
		{"zero updatedAt", 0, SessionResetPolicy{MaxIdleMs: 1000}, false},
		{"not idle", now - 500, SessionResetPolicy{MaxIdleMs: 1000}, false},
		{"idle", now - 2000, SessionResetPolicy{MaxIdleMs: 1000}, true},
		{"negative maxIdle", now - 100, SessionResetPolicy{MaxIdleMs: -1}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IsSessionIdle(tc.updatedAt, tc.policy)
			if got != tc.want {
				t.Errorf("IsSessionIdle() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDefaultSessionResetPolicy(t *testing.T) {
	p := DefaultSessionResetPolicy()
	if p.MaxAgeMs != 0 {
		t.Errorf("got %d, want MaxAgeMs=0", p.MaxAgeMs)
	}
	if p.MaxIdleMs != 0 {
		t.Errorf("got %d, want MaxIdleMs=0", p.MaxIdleMs)
	}
	if p.OnNewAgent {
		t.Error("expected OnNewAgent=false")
	}
}

func TestBuildSessionHint(t *testing.T) {
	tests := []struct {
		name  string
		flags SessionHintFlags
		want  string
	}{
		{"no flags", SessionHintFlags{}, ""},
		{"aborted", SessionHintFlags{WasAborted: true}, "Previous run was aborted by user."},
		{"failed", SessionHintFlags{PreviousRunFailed: true}, "Previous run failed."},
		{"resumed", SessionHintFlags{IsResumed: true}, "Session resumed."},
		{"forked", SessionHintFlags{IsForked: true}, "Session forked from parent."},
		{
			"multiple flags",
			SessionHintFlags{WasAborted: true, IsResumed: true},
			"Previous run was aborted by user. Session resumed.",
		},
		{
			"all flags",
			SessionHintFlags{WasAborted: true, PreviousRunFailed: true, IsResumed: true, IsForked: true},
			"Previous run was aborted by user. Previous run failed. Session resumed. Session forked from parent.",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := BuildSessionHint(tc.flags)
			if got != tc.want {
				t.Errorf("BuildSessionHint() = %q, want %q", got, tc.want)
			}
		})
	}
}

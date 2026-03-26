package rpc

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/auth"
)

func TestRequiredScope(t *testing.T) {
	tests := []struct {
		method string
		want   auth.Scope
	}{
		// Read scope methods.
		{"health", auth.ScopeRead},
		{"status", auth.ScopeRead},
		{"sessions.list", auth.ScopeRead},
		{"sessions.get", auth.ScopeRead},
		{"channels.list", auth.ScopeRead},
		{"models.list", auth.ScopeRead},
		{"usage.status", auth.ScopeRead},

		// Write scope methods.
		{"sessions.create", auth.ScopeWrite},
		{"sessions.send", auth.ScopeWrite},
		{"sessions.abort", auth.ScopeWrite},
		{"chat.send", auth.ScopeWrite},
		{"send", auth.ScopeWrite},
		{"cron.add", auth.ScopeWrite},

		// Admin scope methods.
		{"channels.start", auth.ScopeAdmin},
		{"channels.stop", auth.ScopeAdmin},
		{"config.get", auth.ScopeAdmin},
		{"config.set", auth.ScopeAdmin},
		{"secrets.reload", auth.ScopeAdmin},
		{"update.run", auth.ScopeAdmin},

		// Approvals scope.
		{"process.exec", auth.ScopeApprovals},
		{"exec.approval.request", auth.ScopeApprovals},

		// Unknown method defaults to admin.
		{"nonexistent.method", auth.ScopeAdmin},
		{"", auth.ScopeAdmin},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			got := RequiredScope(tt.method)
			if got != tt.want {
				t.Errorf("RequiredScope(%q) = %q, want %q", tt.method, got, tt.want)
			}
		})
	}
}

func TestMethodScopeMapCompleteness(t *testing.T) {
	// Verify the map has a reasonable number of entries (regression guard).
	if len(methodScopes) < 100 {
		t.Errorf("methodScopes has %d entries, expected at least 100", len(methodScopes))
	}

	// Verify no scope values are empty strings.
	for method, scope := range methodScopes {
		if scope == "" {
			t.Errorf("method %q has empty scope", method)
		}
	}
}

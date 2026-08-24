package chat

import "testing"

// TestRequestedModelRecordsAskedName pins the usage-attribution contract: a run
// that asks for a role by name must be logged under that name, not under the
// model the role currently points at. Read-time model→role mapping breaks
// exactly when a role is retargeted, which is when someone is looking.
func TestRequestedModelRecordsAskedName(t *testing.T) {
	tests := []struct {
		name     string
		asked    string
		resolved string
		want     string
	}{
		{"role name survives resolution", "fallback", "deepseek-v4-flash", "fallback"},
		{"submain role survives resolution", "submain", "glm-5.3", "submain"},
		{"explicit raw model is kept verbatim", "kimi/k3", "kimi/k3", "kimi/k3"},
		// Nothing asked → the resolved id stays, so a session-level override is
		// still attributable instead of collapsing into "main".
		{"unasked run keeps the resolved id", "", "glm-5.3", "glm-5.3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := requestedModelForLog(tt.asked, tt.resolved); got != tt.want {
				t.Fatalf("requestedModel(asked=%q, resolved=%q) = %q, want %q", tt.asked, tt.resolved, got, tt.want)
			}
		})
	}
}

package chat

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/httpretry"
)

// TestShouldStripThinking verifies the Anthropic thinking-signature recovery
// is actually wired: a 400 whose body names a thinking-block signature
// classifies (llmerr) to Action.StripThink, while other 4xx/5xx do not. This
// is the consumer that makes the StripThink recovery action live — without a
// caller reading it, the classified recovery never fires.
func TestShouldStripThinkingReturnsTrueForSignatureError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{
			"anthropic thinking-signature 400",
			&httpretry.APIError{StatusCode: 400, Message: `{"error":{"message":"messages.1.content.0.thinking.signature: invalid signature for thinking block"}}`},
			true,
		},
		{
			"plain 400 format error",
			&httpretry.APIError{StatusCode: 400, Message: "invalid request body"},
			false,
		},
		{
			"transient 502",
			&httpretry.APIError{StatusCode: 502, Message: "bad gateway"},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldStripThinking(tt.err); got != tt.want {
				t.Errorf("shouldStripThinking(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestFormatToolActivitySummary(t *testing.T) {
	tests := []struct {
		name       string
		activities []agent.ToolActivity
		want       string
	}{
		{name: "nil", activities: nil, want: ""},
		{name: "empty", activities: []agent.ToolActivity{}, want: ""},
		{
			name:       "single tool",
			activities: []agent.ToolActivity{{Name: "read_file"}},
			want:       "Tools used: read_file",
		},
		{
			name: "multiple distinct",
			activities: []agent.ToolActivity{
				{Name: "read_file"},
				{Name: "edit"},
				{Name: "exec"},
			},
			want: "Tools used: read_file, edit, exec",
		},
		{
			name: "repeated tools with counts",
			activities: []agent.ToolActivity{
				{Name: "read_file"},
				{Name: "edit"},
				{Name: "read_file"},
				{Name: "exec"},
				{Name: "read_file"},
			},
			want: "Tools used: read_file ×3, edit, exec",
		},
		{
			name: "preserves first-seen order",
			activities: []agent.ToolActivity{
				{Name: "exec"},
				{Name: "read_file"},
				{Name: "exec"},
			},
			want: "Tools used: exec ×2, read_file",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := formatToolActivitySummary(tc.activities)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEnrichStopFallbackKeepsBareMessageWithoutTools(t *testing.T) {
	base := fallbackForStopReason("timeout")
	if got := enrichStopFallback(base, nil); got != base {
		t.Fatalf("got %q, want exact timeout fallback", got)
	}
}

func TestEnrichStopFallbackAppendsToolRemnant(t *testing.T) {
	base := fallbackForStopReason("timeout")
	got := enrichStopFallback(base, []agent.ToolActivity{{Name: "watch"}, {Name: "watch"}, {Name: "web"}})
	if !strings.Contains(got, base) {
		t.Fatalf("missing base fallback: %q", got)
	}
	if !strings.Contains(got, "Tools used: watch ×2, web") {
		t.Fatalf("missing remnant: %q", got)
	}
	if !strings.Contains(got, "이미 실행한 도구는 남겼어요") {
		t.Fatalf("missing remnant notice: %q", got)
	}
}

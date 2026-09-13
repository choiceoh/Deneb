package observeops

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/core/observe"
)

func TestFormatRoutingShowsTheLocalShare(t *testing.T) {
	usage := observe.RouterUsage{Window: "2026-09", Models: []observe.RouterModelUsage{
		{Model: "glm-5.3-flash-local", Requests: 838, InputTokens: 20_372_153, OutputTokens: 512_498},
		{Model: "glm-5.3-flash", Requests: 4412, InputTokens: 156_517_452, OutputTokens: 4_694_723},
	}}
	out := formatRouting(usage, map[string]bool{"glm-5.3-flash-local": true}, true)
	for _, want := range []string{"2026-09", "5250 requests", "838", "4412", "LOCAL", "remote", "20.4M"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// The share is the reason this block exists: 838 of 5250 is 16.0%.
	if !strings.Contains(out, "16.0%") {
		t.Errorf("local share not rendered:\n%s", out)
	}
}

// The meter is a diagnostic. Its absence must read as "unknown", never as "the
// engine served nothing" — the two would look identical in a postmortem.
func TestFormatRoutingSaysUnavailableRatherThanZero(t *testing.T) {
	out := formatRouting(observe.RouterUsage{}, nil, false)
	if !strings.Contains(out, "unavailable") {
		t.Errorf("an unreachable meter must say so:\n%s", out)
	}
	if strings.Contains(out, "0.0%") {
		t.Errorf("an unreachable meter must not render a share:\n%s", out)
	}
	empty := formatRouting(observe.RouterUsage{Window: "2026-09"}, nil, true)
	if !strings.Contains(empty, "no requests metered") {
		t.Errorf("an empty window must be distinguishable:\n%s", empty)
	}
}

func TestCompactTokensStaysReadable(t *testing.T) {
	for in, want := range map[int64]string{0: "0", 512: "512", 20_372: "20K", 156_517_452: "156.5M"} {
		if got := compactTokens(in); got != want {
			t.Errorf("compactTokens(%d) = %q, want %q", in, got, want)
		}
	}
}

package chat

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
)

// TestIsStalledResult locks the stall-detection predicate that decides whether
// a turn engages the model fallback chain. A stall is a timeout that produced
// nothing at all; a timeout that produced text is a slow-but-productive turn
// (falling back would discard the partial answer), and a timeout after tool
// rounds (Turns > 1) is budget exhaustion mid-work — automation turns narrate
// nothing, and a fallback re-run would repeat the committed tools' side
// effects (live 2026-07-17 mailpoll misclassification).
func TestIsStalledResultReturnsTrueOnlyForBlankTimeout(t *testing.T) {
	cases := []struct {
		name string
		res  *agent.AgentResult
		want bool
	}{
		{"nil result", nil, false},
		{"timeout with no output is a stall", &agent.AgentResult{StopReason: "timeout"}, true},
		{"timeout on the first round with no output is a stall", &agent.AgentResult{StopReason: "timeout", Turns: 1}, true},
		{"timeout with whitespace-only output is a stall", &agent.AgentResult{StopReason: "timeout", AllText: "  \n\t"}, true},
		{"timeout after producing text is not a stall", &agent.AgentResult{StopReason: "timeout", AllText: "partial answer"}, false},
		{"timeout after tool rounds is budget exhaustion, not a stall", &agent.AgentResult{StopReason: "timeout", Turns: 6}, false},
		{"end_turn with no output is not a stall", &agent.AgentResult{StopReason: "end_turn"}, false},
		{"max_turns with no output is not a stall", &agent.AgentResult{StopReason: "max_turns"}, false},
		{"aborted with no output is not a stall", &agent.AgentResult{StopReason: "aborted"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isStalledResult(tc.res); got != tc.want {
				t.Fatalf("isStalledResult(%+v) = %v, want %v", tc.res, got, tc.want)
			}
		})
	}
}

package runtimeops

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// The adoption-rate note rides the tool result only when the hint speaks —
// silence must leave the success line untouched.
func TestToolWorkstation_UsageHintAppended(t *testing.T) {
	send := func(context.Context, string, map[string]string) error { return nil }

	spoke := ToolWorkstation(send, func(action string) string {
		return "참고(효용 원장): " + action + " 유지율 낮음"
	})
	out, err := spoke(context.Background(), json.RawMessage(`{"action":"focus","view":"mail"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "참고(효용 원장): focus 유지율 낮음") {
		t.Fatalf("hint missing from result: %q", out)
	}

	silent := ToolWorkstation(send, func(string) string { return "" })
	out, err = silent(context.Background(), json.RawMessage(`{"action":"focus","view":"mail"}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "참고") {
		t.Fatalf("silent hint leaked into result: %q", out)
	}
}

package agentlog

import (
	"encoding/json"
	"testing"
)

func TestAggregateByModel_PreservesPerSessionCorrelationAndMalformedIsolation(t *testing.T) {
	writer := NewWriter(t.TempDir())
	// The same run ID may appear in different session files. Correlation maps are
	// deliberately file-scoped so the later file cannot steal the first model's
	// completion or tool errors.
	appendEntry(t, writer, "session-beta", "shared-run", TypeRunStart,
		RunStartData{Model: "beta", Provider: "provider-b"})
	appendEntry(t, writer, "session-beta", "shared-run", TypeRunEnd,
		RunEndData{Turns: 2, TotalMs: 20})
	appendEntry(t, writer, "session-alpha", "shared-run", TypeRunStart,
		RunStartData{Model: "alpha", Provider: "provider-a", ThinkingLevel: "high"})
	appendEntry(t, writer, "session-alpha", "shared-run", TypeTurnTool,
		TurnToolData{Name: "fs", IsError: true})
	appendEntry(t, writer, "session-alpha", "shared-run", TypeRunEnd,
		RunEndData{Turns: 1, TotalMs: 10, Model: "fallback-alpha"})

	// A start without an end/error is omitted from the output, and a completion
	// in a different file with no local start is ignored.
	appendEntry(t, writer, "session-incomplete", "incomplete", TypeRunStart,
		RunStartData{Model: "incomplete", Provider: "provider-i"})
	appendEntry(t, writer, "session-orphan", "shared-run", TypeRunEnd,
		RunEndData{Turns: 99, TotalMs: 999})

	for _, eventType := range []string{TypeRunStart, TypeRunEnd, TypeTurnTool, TypeRunError} {
		if err := writer.Append(LogEntry{
			Ts: 1000, Type: eventType, Session: "session-alpha", RunID: "shared-run",
			Data: json.RawMessage(`"bad payload"`),
		}); err != nil {
			t.Fatal(err)
		}
	}
	// run.error attribution uses only the envelope/runId, so its deliberately
	// opaque payload still counts; malformed payloads for parsed event types do
	// not contaminate their counters.

	stats := writer.AggregateByModel(0)
	if len(stats) != 2 {
		t.Fatalf("stats = %+v, want only completed alpha and beta", stats)
	}
	if stats[0].Model != "alpha" || stats[1].Model != "beta" {
		t.Fatalf("tie order = %+v, want model name ascending after equal run count", stats)
	}
	alpha, beta := stats[0], stats[1]
	if alpha.Provider != "provider-a" || alpha.Runs != 1 || alpha.Turns != 1 ||
		alpha.ToolErrors != 1 || alpha.ThinkingRuns != 1 || alpha.FallbackRuns != 1 ||
		alpha.AvgMs != 10 || alpha.P95Ms != 10 || alpha.Errors != 1 {
		t.Fatalf("alpha correlation = %+v", alpha)
	}
	if beta.Provider != "provider-b" || beta.Runs != 1 || beta.Turns != 2 ||
		beta.ToolErrors != 0 || beta.ThinkingRuns != 0 || beta.FallbackRuns != 0 ||
		beta.AvgMs != 20 || beta.P95Ms != 20 || beta.Errors != 0 {
		t.Fatalf("beta correlation = %+v", beta)
	}
}

package agentlog

import (
	"encoding/json"
	"sync"
	"testing"
)

func appendAggregateEntry(t *testing.T, writer *Writer, ts int64, eventType string, data any) {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Append(LogEntry{Ts: ts, Type: eventType, Session: "s", RunID: "r", Data: raw}); err != nil {
		t.Fatal(err)
	}
}

func TestAggregate_PreservesFoldFilteringAndStableToolOrder(t *testing.T) {
	writer := NewWriter(t.TempDir())
	appendAggregateEntry(t, writer, 999, TypeRunEnd, RunEndData{InputTokens: 9999})
	appendAggregateEntry(t, writer, 1000, TypeTurnTool, TurnToolData{
		Name: "beta", DurationMs: 10, OutputLen: 50,
	})
	appendAggregateEntry(t, writer, 1000, TypeTurnTool, TurnToolData{
		Name: "alpha", DurationMs: 20, OutputLen: 70, IsError: true, UnknownTool: true, Blocked: "policy",
	})
	appendAggregateEntry(t, writer, 1000, TypeRunEnd, RunEndData{
		InputTokens: 10, OutputTokens: 20, CacheReadTokens: 5,
		Proactive: true, Compacted: true,
		RepairedToolCalls:  map[string]int{"orphan": 2},
		CacheHitToolCalls:  map[string]int{"alpha": 3},
		TruncatedToolCalls: map[string]int{"beta": 1},
	})
	appendAggregateEntry(t, writer, 1000, TypeProactiveRelay, ProactiveRelayData{
		Decision: "suppressed", Reason: "contentless",
	})
	appendAggregateEntry(t, writer, 1000, TypeBackgroundJob, BackgroundJobData{
		Name: "gmailpoll", Outcome: "error",
	})
	// Valid log envelopes with payloads of the wrong JSON shape must be ignored
	// independently by each event fold.
	for _, eventType := range []string{TypeTurnTool, TypeRunEnd, TypeProactiveRelay, TypeBackgroundJob} {
		if err := writer.Append(LogEntry{
			Ts: 1000, Type: eventType, Session: "s", RunID: "bad", Data: json.RawMessage(`"bad payload"`),
		}); err != nil {
			t.Fatal(err)
		}
	}

	result := writer.Aggregate(1000)
	if result.Runs != 1 || result.ProactiveRuns != 1 || result.CompactedRuns != 1 ||
		result.TotalInputTokens != 10 || result.TotalOutputTokens != 20 || result.CacheReadTokens != 5 {
		t.Fatalf("run totals = %+v", result)
	}
	if result.ProactiveDecisions["suppressed:contentless"] != 1 ||
		result.BackgroundJobs["gmailpoll"] != 1 || result.BackgroundErrors["gmailpoll"] != 1 {
		t.Fatalf("relay/background folds = decisions:%v jobs:%v errors:%v",
			result.ProactiveDecisions, result.BackgroundJobs, result.BackgroundErrors)
	}
	if len(result.Tools) != 3 {
		t.Fatalf("tools = %+v, want alpha, beta, orphan", result.Tools)
	}
	if result.Tools[0].Name != "alpha" || result.Tools[1].Name != "beta" || result.Tools[2].Name != "orphan" {
		t.Fatalf("stable tool order = %+v, want calls desc then name asc", result.Tools)
	}
	alpha, beta, orphan := result.Tools[0], result.Tools[1], result.Tools[2]
	if alpha.Calls != 1 || alpha.AvgMs != 20 || alpha.Errors != 1 || alpha.Unknown != 1 ||
		alpha.Blocked != 1 || alpha.CacheHits != 3 || alpha.TotalOutputChars != 70 || alpha.MaxOutputChars != 70 {
		t.Fatalf("alpha fold = %+v", alpha)
	}
	if beta.Calls != 1 || beta.AvgMs != 10 || beta.Truncated != 1 || beta.TotalOutputChars != 50 {
		t.Fatalf("beta fold = %+v", beta)
	}
	if orphan.Calls != 0 || orphan.AvgMs != 0 || orphan.Repaired != 2 {
		t.Fatalf("repair-only tool fold = %+v", orphan)
	}
}

func TestAggregate_ConcurrentWithAppend(t *testing.T) {
	writer := NewWriter(t.TempDir())
	raw, err := json.Marshal(RunEndData{InputTokens: 1})
	if err != nil {
		t.Fatal(err)
	}

	const writerCount = 4
	const entriesPerWriter = 20
	start := make(chan struct{})
	errors := make(chan error, writerCount*entriesPerWriter)
	var wait sync.WaitGroup
	for worker := 0; worker < writerCount; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			for i := 0; i < entriesPerWriter; i++ {
				if err := writer.Append(LogEntry{
					Ts: 1000, Type: TypeRunEnd, Session: "shared", RunID: "concurrent", Data: raw,
				}); err != nil {
					errors <- err
				}
			}
		}()
	}
	wait.Add(1)
	go func() {
		defer wait.Done()
		<-start
		for i := 0; i < entriesPerWriter; i++ {
			_ = writer.Aggregate(0)
		}
	}()
	close(start)
	wait.Wait()
	close(errors)
	for err := range errors {
		t.Errorf("Append: %v", err)
	}

	if got, want := writer.Aggregate(0).Runs, writerCount*entriesPerWriter; got != want {
		t.Fatalf("Runs = %d, want %d after concurrent append/read", got, want)
	}
}

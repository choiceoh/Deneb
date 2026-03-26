package usage

import (
	"testing"
)

func TestNew(t *testing.T) {
	tracker := New()
	if tracker == nil {
		t.Fatal("expected non-nil tracker")
	}
	if tracker.startedAt.IsZero() {
		t.Fatal("expected startedAt to be set")
	}
}

func TestRecordCall(t *testing.T) {
	tracker := New()

	tracker.RecordCall("anthropic")
	tracker.RecordCall("anthropic")
	tracker.RecordCall("openai")

	status := tracker.Status()
	if status.Providers["anthropic"].Calls != 2 {
		t.Fatalf("expected 2 anthropic calls, got %d", status.Providers["anthropic"].Calls)
	}
	if status.Providers["openai"].Calls != 1 {
		t.Fatalf("expected 1 openai call, got %d", status.Providers["openai"].Calls)
	}
}

func TestRecordTokens(t *testing.T) {
	tracker := New()

	tracker.RecordTokens("anthropic", 100, 200, 50, 10)
	tracker.RecordTokens("anthropic", 150, 300, 0, 0)

	status := tracker.Status()
	stats := status.Providers["anthropic"]
	if stats.Tokens.Input != 250 {
		t.Fatalf("expected 250 input tokens, got %d", stats.Tokens.Input)
	}
	if stats.Tokens.Output != 500 {
		t.Fatalf("expected 500 output tokens, got %d", stats.Tokens.Output)
	}
	if stats.Tokens.CacheRead != 50 {
		t.Fatalf("expected 50 cache read tokens, got %d", stats.Tokens.CacheRead)
	}
	if stats.Tokens.CacheWrite != 10 {
		t.Fatalf("expected 10 cache write tokens, got %d", stats.Tokens.CacheWrite)
	}
}

func TestStatus(t *testing.T) {
	tracker := New()
	tracker.RecordCall("test-provider")

	status := tracker.Status()
	if status.Uptime == "" {
		t.Fatal("expected non-empty uptime")
	}
	if status.StartedAt == "" {
		t.Fatal("expected non-empty startedAt")
	}
	if len(status.Providers) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(status.Providers))
	}
}

func TestCost(t *testing.T) {
	tracker := New()
	tracker.RecordCall("anthropic")
	tracker.RecordCall("anthropic")
	tracker.RecordCall("openai")

	cost := tracker.Cost()
	if cost.TotalCalls != 3 {
		t.Fatalf("expected 3 total calls, got %d", cost.TotalCalls)
	}
	if len(cost.Providers) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(cost.Providers))
	}
}

func TestStatusIsolation(t *testing.T) {
	// Ensure Status returns a copy, not a reference to internal state.
	tracker := New()
	tracker.RecordCall("test")

	status1 := tracker.Status()
	tracker.RecordCall("test")
	status2 := tracker.Status()

	if status1.Providers["test"].Calls == status2.Providers["test"].Calls {
		t.Fatal("expected status snapshots to be independent")
	}
}

func TestEmptyStatus(t *testing.T) {
	tracker := New()
	status := tracker.Status()

	if len(status.Providers) != 0 {
		t.Fatalf("expected 0 providers, got %d", len(status.Providers))
	}
}

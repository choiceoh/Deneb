package usage

import (
	"testing"
)


func TestRecordCall(t *testing.T) {
	tracker := New()

	tracker.RecordCall("anthropic")
	tracker.RecordCall("anthropic")
	tracker.RecordCall("openai")

	status := tracker.Status()
	if status.Providers["anthropic"].Calls != 2 {
		t.Fatalf("got %d, want 2 anthropic calls", status.Providers["anthropic"].Calls)
	}
	if status.Providers["openai"].Calls != 1 {
		t.Fatalf("got %d, want 1 openai call", status.Providers["openai"].Calls)
	}
}

func TestRecordTokens(t *testing.T) {
	tracker := New()

	tracker.RecordTokens("anthropic", 100, 200, 50, 10)
	tracker.RecordTokens("anthropic", 150, 300, 0, 0)

	status := tracker.Status()
	stats := status.Providers["anthropic"]
	if stats.Tokens.Input != 250 {
		t.Fatalf("got %d, want 250 input tokens", stats.Tokens.Input)
	}
	if stats.Tokens.Output != 500 {
		t.Fatalf("got %d, want 500 output tokens", stats.Tokens.Output)
	}
	if stats.Tokens.CacheRead != 50 {
		t.Fatalf("got %d, want 50 cache read tokens", stats.Tokens.CacheRead)
	}
	if stats.Tokens.CacheWrite != 10 {
		t.Fatalf("got %d, want 10 cache write tokens", stats.Tokens.CacheWrite)
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
		t.Fatalf("got %d, want 1 provider", len(status.Providers))
	}
}

func TestCost(t *testing.T) {
	tracker := New()
	tracker.RecordCall("anthropic")
	tracker.RecordCall("anthropic")
	tracker.RecordCall("openai")

	cost := tracker.Cost()
	if cost.TotalCalls != 3 {
		t.Fatalf("got %d, want 3 total calls", cost.TotalCalls)
	}
	if len(cost.Providers) != 2 {
		t.Fatalf("got %d, want 2 providers", len(cost.Providers))
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


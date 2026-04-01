package hooks

import (
	"context"
	"log/slog"
	"testing"
)

func TestFireProgressive(t *testing.T) {
	logger := slog.Default()

	t.Run("no hooks", func(t *testing.T) {
		r := NewRegistry(logger)
		ch := r.FireProgressive(context.Background(), EventGatewayStart, nil)
		events := CollectProgress(ch)
		if len(events) != 0 {
			t.Errorf("expected 0 events, got %d", len(events))
		}
	})

	t.Run("blocking hook emits started and completed", func(t *testing.T) {
		r := NewRegistry(logger)
		r.Register(Hook{
			ID:       "test-hook",
			Event:    EventGatewayStart,
			Command:  "echo hello",
			Blocking: true,
			Enabled:  true,
		})

		ch := r.FireProgressive(context.Background(), EventGatewayStart, nil)
		events := CollectProgress(ch)

		if len(events) != 2 {
			t.Fatalf("expected 2 events, got %d", len(events))
		}
		if events[0].Phase != "started" {
			t.Errorf("first event phase = %q, want started", events[0].Phase)
		}
		if events[1].Phase != "completed" {
			t.Errorf("second event phase = %q, want completed", events[1].Phase)
		}
		if events[1].DurationMs <= 0 {
			t.Error("expected positive duration")
		}
	})

	t.Run("failed hook", func(t *testing.T) {
		r := NewRegistry(logger)
		r.Register(Hook{
			ID:       "fail-hook",
			Event:    EventGatewayStart,
			Command:  "exit 1",
			Blocking: true,
			Enabled:  true,
		})

		ch := r.FireProgressive(context.Background(), EventGatewayStart, nil)
		events := CollectProgress(ch)

		if len(events) != 2 {
			t.Fatalf("expected 2 events, got %d", len(events))
		}
		if events[1].Phase != "failed" {
			t.Errorf("event phase = %q, want failed", events[1].Phase)
		}
		if events[1].ExitCode != 1 {
			t.Errorf("exit code = %d, want 1", events[1].ExitCode)
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		r := NewRegistry(logger)
		r.Register(Hook{
			ID:      "h1",
			Event:   EventGatewayStart,
			Command: "echo one",
			Enabled: true,
		})
		r.Register(Hook{
			ID:      "h2",
			Event:   EventGatewayStart,
			Command: "sleep 10",
			Enabled: true,
		})

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // cancel immediately

		ch := r.FireProgressive(ctx, EventGatewayStart, nil)
		events := CollectProgress(ch)

		// Should get at most 1 started event before seeing cancellation.
		if len(events) > 2 {
			t.Errorf("expected <= 2 events with cancelled context, got %d", len(events))
		}
	})
}

func TestTotalDuration(t *testing.T) {
	events := []HookProgress{
		{DurationMs: 100},
		{DurationMs: 250},
		{DurationMs: 50},
	}
	total := TotalDuration(events)
	if total.Milliseconds() != 400 {
		t.Errorf("total = %v, want 400ms", total)
	}
}

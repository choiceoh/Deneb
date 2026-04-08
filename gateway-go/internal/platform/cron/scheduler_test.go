package cron

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestScheduler_ImmediateOneShot(t *testing.T) {
	s := NewScheduler(testLogger())
	defer s.Close()

	var called atomic.Int32
	err := s.Register(context.Background(), "test-1", Schedule{
		Label:     "one-shot",
		Immediate: true,
	}, func(_ context.Context) error {
		called.Add(1)
		return nil
	})
	testutil.NoError(t, err)

	// Wait for immediate execution.
	time.Sleep(50 * time.Millisecond)

	if called.Load() != 1 {
		t.Errorf("got %d, want 1 call", called.Load())
	}

	status := s.Get("test-1")
	if status == nil {
		t.Fatal("expected task status")
	}
	if status.RunCount != 1 {
		t.Errorf("got %d, want runCount=1", status.RunCount)
	}
}

func TestScheduler_IntervalTask(t *testing.T) {
	s := NewScheduler(testLogger())
	defer s.Close()

	var called atomic.Int32
	err := s.Register(context.Background(), "tick", Schedule{
		Label:      "ticker",
		IntervalMs: 50,
	}, func(_ context.Context) error {
		called.Add(1)
		return nil
	})
	testutil.NoError(t, err)

	time.Sleep(180 * time.Millisecond)

	count := called.Load()
	if count < 2 {
		t.Errorf("got %d, want at least 2 calls", count)
	}
}

func TestScheduler_Unregister(t *testing.T) {
	s := NewScheduler(testLogger())
	defer s.Close()

	var called atomic.Int32
	s.Register(context.Background(), "removable", Schedule{
		Label:      "removable",
		IntervalMs: 50,
	}, func(_ context.Context) error {
		called.Add(1)
		return nil
	})

	time.Sleep(80 * time.Millisecond)
	if !s.Unregister("removable") {
		t.Error("expected true from unregister")
	}

	countAfterRemove := called.Load()
	time.Sleep(100 * time.Millisecond)

	if called.Load() > countAfterRemove+1 { // allow one in-flight
		t.Errorf("task continued running after unregister: before=%d, after=%d", countAfterRemove, called.Load())
	}
}

func TestScheduler_List(t *testing.T) {
	s := NewScheduler(testLogger())
	defer s.Close()

	s.Register(context.Background(), "a", Schedule{Label: "A", Immediate: true}, func(_ context.Context) error { return nil })
	s.Register(context.Background(), "b", Schedule{Label: "B", Immediate: true}, func(_ context.Context) error { return nil })

	time.Sleep(50 * time.Millisecond)

	list := s.List()
	if len(list) != 2 {
		t.Errorf("got %d, want 2 tasks", len(list))
	}
}

func TestScheduler_ErrorTracking(t *testing.T) {
	s := NewScheduler(testLogger())
	defer s.Close()

	s.Register(context.Background(), "fail", Schedule{
		Label:     "failer",
		Immediate: true,
	}, func(_ context.Context) error {
		return fmt.Errorf("something broke")
	})

	time.Sleep(50 * time.Millisecond)

	status := s.Get("fail")
	if status == nil {
		t.Fatal("expected task status")
	}
	if status.LastError != "something broke" {
		t.Errorf("got %q, want error message", status.LastError)
	}
}

func TestScheduler_InvalidParams(t *testing.T) {
	s := NewScheduler(testLogger())
	defer s.Close()

	err := s.Register(context.Background(), "", Schedule{IntervalMs: 100}, func(_ context.Context) error { return nil })
	if err == nil {
		t.Error("expected error for empty ID")
	}

	err = s.Register(context.Background(), "bad", Schedule{}, func(_ context.Context) error { return nil })
	if err == nil {
		t.Error("expected error for no interval and no immediate")
	}
}

func TestScheduler_Replace(t *testing.T) {
	s := NewScheduler(testLogger())
	defer s.Close()

	var v1 atomic.Int32
	var v2 atomic.Int32

	s.Register(context.Background(), "replace-me", Schedule{Label: "v1", IntervalMs: 50}, func(_ context.Context) error {
		v1.Add(1)
		return nil
	})

	time.Sleep(80 * time.Millisecond)

	// Replace with new task.
	s.Register(context.Background(), "replace-me", Schedule{Label: "v2", IntervalMs: 50}, func(_ context.Context) error {
		v2.Add(1)
		return nil
	})

	v1CountAtReplace := v1.Load()
	time.Sleep(150 * time.Millisecond)

	// v1 should have stopped (at most 1 extra in-flight).
	if v1.Load() > v1CountAtReplace+1 {
		t.Errorf("old task continued: before=%d, after=%d", v1CountAtReplace, v1.Load())
	}
	if v2.Load() < 1 {
		t.Error("new task should have run")
	}
}

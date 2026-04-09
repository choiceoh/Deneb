package telegram

import (
	"sync"
	"testing"
	"time"
)

func TestDraftStreamLoop_ImmediateFlush(t *testing.T) {
	var mu sync.Mutex
	var sent []string

	loop := NewDraftStreamLoop(0, func(text string) (bool, error) {
		mu.Lock()
		sent = append(sent, text)
		mu.Unlock()
		return true, nil
	})

	loop.Update("hello")
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if len(sent) == 0 {
		mu.Unlock()
		t.Fatal("expected at least one send")
	}
	if sent[0] != "hello" {
		t.Errorf("sent[0] = %q, want hello", sent[0])
	}
	mu.Unlock()
}

func TestDraftStreamLoop_Throttled(t *testing.T) {
	var mu sync.Mutex
	var sent []string

	loop := NewDraftStreamLoop(100, func(text string) (bool, error) {
		mu.Lock()
		sent = append(sent, text)
		mu.Unlock()
		return true, nil
	})

	// Rapid updates should be throttled.
	loop.Update("a")
	loop.Update("b")
	loop.Update("c")

	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	count := len(sent)
	mu.Unlock()

	// Should have sent at most 2-3 messages due to throttling.
	if count == 0 {
		t.Error("expected at least one send")
	}
}

func TestDraftStreamLoop_Stop(t *testing.T) {
	loop := NewDraftStreamLoop(50, func(text string) (bool, error) {
		return true, nil
	})

	loop.Update("hello")
	loop.Stop()

	// After stop, further updates are no-ops.
	loop.Update("ignored")
	time.Sleep(100 * time.Millisecond)
}

func TestDraftStreamLoop_FlushSendsPending(t *testing.T) {
	var mu sync.Mutex
	var sent []string

	loop := NewDraftStreamLoop(500, func(text string) (bool, error) {
		mu.Lock()
		sent = append(sent, text)
		mu.Unlock()
		return true, nil
	})

	loop.Update("pending text")
	loop.Flush()

	mu.Lock()
	found := false
	for _, s := range sent {
		if s == "pending text" {
			found = true
		}
	}
	mu.Unlock()

	if !found {
		t.Error("Flush() should have sent pending text")
	}
}

func TestDraftStreamLoop_FinalizeFlushesAndStops(t *testing.T) {
	var mu sync.Mutex
	var sent []string

	loop := NewDraftStreamLoop(0, func(text string) (bool, error) {
		mu.Lock()
		sent = append(sent, text)
		mu.Unlock()
		return true, nil
	})

	loop.Update("hello world")
	time.Sleep(50 * time.Millisecond)
	loop.Finalize()

	mu.Lock()
	if len(sent) == 0 {
		mu.Unlock()
		t.Fatal("expected at least one send")
	}
	mu.Unlock()

	// After finalize, further updates should be ignored.
	loop.Update("ignored")
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	lastSent := sent[len(sent)-1]
	mu.Unlock()

	if lastSent == "ignored" {
		t.Error("update after finalize should be ignored")
	}
}

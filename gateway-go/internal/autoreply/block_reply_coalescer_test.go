package autoreply

import (
	"sync"
	"testing"
	"time"
)

func TestBlockReplyCoalescer_BasicFlush(t *testing.T) {
	var flushed []ReplyPayload
	var mu sync.Mutex

	c := NewBlockReplyCoalescer(BlockStreamingCoalescing{
		MinChars: 10,
		MaxChars: 100,
		IdleMs:   0,
	}, func() bool { return false }, func(p ReplyPayload) {
		mu.Lock()
		flushed = append(flushed, p)
		mu.Unlock()
	})

	c.Enqueue(ReplyPayload{Text: "hello world this is a test"})
	c.Flush(true)

	mu.Lock()
	defer mu.Unlock()
	if len(flushed) != 1 {
		t.Fatalf("expected 1 flushed payload, got %d", len(flushed))
	}
	if flushed[0].Text != "hello world this is a test" {
		t.Fatalf("unexpected text: %q", flushed[0].Text)
	}
}

func TestBlockReplyCoalescer_MediaFlushesImmediately(t *testing.T) {
	var flushed []ReplyPayload
	c := NewBlockReplyCoalescer(BlockStreamingCoalescing{
		MinChars: 100,
		MaxChars: 200,
		IdleMs:   0,
	}, func() bool { return false }, func(p ReplyPayload) {
		flushed = append(flushed, p)
	})

	c.Enqueue(ReplyPayload{Text: "buffered text"})
	c.Enqueue(ReplyPayload{MediaURL: "https://example.com/image.png"})

	// The buffered text should have been flushed before media, and media sent directly.
	if len(flushed) < 2 {
		t.Fatalf("expected at least 2 flushed payloads, got %d", len(flushed))
	}
}

func TestBlockReplyCoalescer_CoalescesText(t *testing.T) {
	var flushed []ReplyPayload
	c := NewBlockReplyCoalescer(BlockStreamingCoalescing{
		MinChars: 20,
		MaxChars: 200,
		IdleMs:   0,
		Joiner:   "\n",
	}, func() bool { return false }, func(p ReplyPayload) {
		flushed = append(flushed, p)
	})

	c.Enqueue(ReplyPayload{Text: "hello"})
	c.Enqueue(ReplyPayload{Text: "world"})
	c.Flush(true)

	if len(flushed) != 1 {
		t.Fatalf("expected 1 flushed payload (coalesced), got %d", len(flushed))
	}
	if flushed[0].Text != "hello\nworld" {
		t.Fatalf("expected coalesced text, got %q", flushed[0].Text)
	}
}

func TestBlockReplyCoalescer_MaxCharsForceFlush(t *testing.T) {
	var flushed []ReplyPayload
	c := NewBlockReplyCoalescer(BlockStreamingCoalescing{
		MinChars: 1,
		MaxChars: 10,
		IdleMs:   0,
	}, func() bool { return false }, func(p ReplyPayload) {
		flushed = append(flushed, p)
	})

	c.Enqueue(ReplyPayload{Text: "12345"})
	c.Enqueue(ReplyPayload{Text: "67890"})

	// Should flush when exceeding maxChars.
	if len(flushed) < 1 {
		t.Fatalf("expected at least 1 flushed payload, got %d", len(flushed))
	}
}

func TestBlockReplyCoalescer_FlushOnEnqueue(t *testing.T) {
	var flushed []ReplyPayload
	c := NewBlockReplyCoalescer(BlockStreamingCoalescing{
		MinChars:       1,
		MaxChars:       1000,
		IdleMs:         0,
		FlushOnEnqueue: true,
	}, func() bool { return false }, func(p ReplyPayload) {
		flushed = append(flushed, p)
	})

	c.Enqueue(ReplyPayload{Text: "one"})
	c.Enqueue(ReplyPayload{Text: "two"})

	if len(flushed) != 2 {
		t.Fatalf("expected 2 flushed payloads (flush on enqueue), got %d", len(flushed))
	}
}

func TestBlockReplyCoalescer_ReplyToConflict(t *testing.T) {
	var flushed []ReplyPayload
	c := NewBlockReplyCoalescer(BlockStreamingCoalescing{
		MinChars: 1,
		MaxChars: 1000,
		IdleMs:   0,
	}, func() bool { return false }, func(p ReplyPayload) {
		flushed = append(flushed, p)
	})

	c.Enqueue(ReplyPayload{Text: "hello", ReplyToID: "msg-1"})
	c.Enqueue(ReplyPayload{Text: "world", ReplyToID: "msg-2"})
	c.Flush(true)

	// Should flush separately due to reply-to conflict.
	if len(flushed) != 2 {
		t.Fatalf("expected 2 flushed payloads (reply-to conflict), got %d", len(flushed))
	}
}

func TestBlockReplyCoalescer_Abort(t *testing.T) {
	aborted := false
	var flushed []ReplyPayload
	c := NewBlockReplyCoalescer(BlockStreamingCoalescing{
		MinChars: 1,
		MaxChars: 1000,
		IdleMs:   0,
	}, func() bool { return aborted }, func(p ReplyPayload) {
		flushed = append(flushed, p)
	})

	c.Enqueue(ReplyPayload{Text: "before abort"})
	aborted = true
	c.Enqueue(ReplyPayload{Text: "after abort"})
	c.Flush(true)

	// Only the first payload should be flushed (buffer cleared on abort).
	if len(flushed) > 1 {
		t.Fatalf("expected at most 1 flushed payload, got %d", len(flushed))
	}
}

func TestBlockReplyCoalescer_IdleTimer(t *testing.T) {
	var flushed []ReplyPayload
	var mu sync.Mutex

	c := NewBlockReplyCoalescer(BlockStreamingCoalescing{
		MinChars: 3, // Low threshold so idle flush succeeds.
		MaxChars: 1000,
		IdleMs:   50, // 50ms idle timer.
	}, func() bool { return false }, func(p ReplyPayload) {
		mu.Lock()
		flushed = append(flushed, p)
		mu.Unlock()
	})

	c.Enqueue(ReplyPayload{Text: "short"})

	// Wait for idle timer.
	time.Sleep(150 * time.Millisecond)

	mu.Lock()
	count := len(flushed)
	mu.Unlock()

	if count != 1 {
		t.Fatalf("expected 1 flushed payload from idle timer, got %d", count)
	}
	c.Stop()
}

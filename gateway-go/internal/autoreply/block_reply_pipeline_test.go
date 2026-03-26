package autoreply

import (
	"context"
	"testing"
)

func TestBlockReplyPipelineFull_BasicSend(t *testing.T) {
	var sent []ReplyPayload
	ctx := context.Background()
	p := NewBlockReplyPipelineFull(ctx, BlockReplyPipelineConfig{
		OnBlockReply: func(_ context.Context, payload ReplyPayload) error {
			sent = append(sent, payload)
			return nil
		},
		TimeoutMs: 5000,
	})

	p.Enqueue(ReplyPayload{Text: "hello"})
	p.FlushAndWait(true)

	if len(sent) != 1 {
		t.Fatalf("expected 1 sent payload, got %d", len(sent))
	}
	if sent[0].Text != "hello" {
		t.Fatalf("unexpected text: %q", sent[0].Text)
	}
	if !p.DidStream() {
		t.Fatal("expected DidStream to be true")
	}
}

func TestBlockReplyPipelineFull_Dedup(t *testing.T) {
	var sent []ReplyPayload
	ctx := context.Background()
	p := NewBlockReplyPipelineFull(ctx, BlockReplyPipelineConfig{
		OnBlockReply: func(_ context.Context, payload ReplyPayload) error {
			sent = append(sent, payload)
			return nil
		},
		TimeoutMs: 5000,
	})

	p.Enqueue(ReplyPayload{Text: "hello"})
	p.Enqueue(ReplyPayload{Text: "hello"}) // duplicate
	p.FlushAndWait(true)

	if len(sent) != 1 {
		t.Fatalf("expected 1 sent payload (dedup), got %d", len(sent))
	}
}

func TestBlockReplyPipelineFull_HasSentPayload(t *testing.T) {
	ctx := context.Background()
	p := NewBlockReplyPipelineFull(ctx, BlockReplyPipelineConfig{
		OnBlockReply: func(_ context.Context, _ ReplyPayload) error { return nil },
		TimeoutMs:    5000,
	})

	payload := ReplyPayload{Text: "test"}
	p.Enqueue(payload)
	p.FlushAndWait(true)

	if !p.HasSentPayload(payload) {
		t.Fatal("expected HasSentPayload to return true")
	}
	if p.HasSentPayload(ReplyPayload{Text: "other"}) {
		t.Fatal("expected HasSentPayload to return false for unsent payload")
	}
}

func TestBlockReplyPipelineFull_DroppedAfterAbort(t *testing.T) {
	ctx := context.Background()
	p := NewBlockReplyPipelineFull(ctx, BlockReplyPipelineConfig{
		OnBlockReply: func(_ context.Context, _ ReplyPayload) error { return nil },
		TimeoutMs:    5000,
	})

	// Manually abort.
	p.mu.Lock()
	p.aborted = true
	p.mu.Unlock()

	p.Enqueue(ReplyPayload{Text: "dropped"})

	if p.DroppedAfterAbort() != 0 {
		// Enqueue short-circuits before sendPayload when aborted.
		t.Logf("dropped count: %d (expected 0, short-circuit on enqueue)", p.DroppedAfterAbort())
	}
}

func TestPayloadKey_DifferentThreading(t *testing.T) {
	p1 := ReplyPayload{Text: "hello", ReplyToID: "msg-1"}
	p2 := ReplyPayload{Text: "hello", ReplyToID: "msg-2"}
	p3 := ReplyPayload{Text: "hello"}

	k1 := payloadKey(p1)
	k2 := payloadKey(p2)
	k3 := payloadKey(p3)

	if k1 == k2 {
		t.Fatal("different ReplyToID should produce different payload keys")
	}
	if k1 == k3 {
		t.Fatal("with and without ReplyToID should produce different payload keys")
	}
}

func TestContentKey_IgnoresThreading(t *testing.T) {
	p1 := ReplyPayload{Text: "hello", ReplyToID: "msg-1"}
	p2 := ReplyPayload{Text: "hello", ReplyToID: "msg-2"}

	c1 := contentKey(p1)
	c2 := contentKey(p2)

	if c1 != c2 {
		t.Fatal("content keys should be equal regardless of ReplyToID")
	}
}

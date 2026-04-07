package streaming

import (
	"context"
	"fmt"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/types"
	"sync"
	"testing"
	"time"
)

func TestBlockReplyPipelineFull_BasicSend(t *testing.T) {
	var mu sync.Mutex
	var sent []types.ReplyPayload
	ctx := context.Background()
	p := NewBlockReplyPipelineFull(ctx, BlockReplyPipelineConfig{
		OnBlockReply: func(_ context.Context, payload types.ReplyPayload) error {
			mu.Lock()
			sent = append(sent, payload)
			mu.Unlock()
			return nil
		},
		TimeoutMs: 5000,
	})

	p.Enqueue(types.ReplyPayload{Text: "hello"})
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

func TestBlockReplyPipelineFull_SequentialDelivery(t *testing.T) {
	// Verify payloads are delivered in order (sequential send chain).
	var mu sync.Mutex
	var order []int
	ctx := context.Background()
	p := NewBlockReplyPipelineFull(ctx, BlockReplyPipelineConfig{
		OnBlockReply: func(_ context.Context, payload types.ReplyPayload) error {
			// Small delay to make ordering visible.
			time.Sleep(5 * time.Millisecond)
			mu.Lock()
			n := 0
			fmt.Sscanf(payload.Text, "msg-%d", &n)
			order = append(order, n)
			mu.Unlock()
			return nil
		},
		TimeoutMs: 5000,
	})

	for i := 0; i < 5; i++ {
		p.Enqueue(types.ReplyPayload{Text: fmt.Sprintf("msg-%d", i)})
	}
	p.FlushAndWait(true)

	mu.Lock()
	defer mu.Unlock()
	if len(order) != 5 {
		t.Fatalf("expected 5 sent, got %d", len(order))
	}
	for i := 0; i < 5; i++ {
		if order[i] != i {
			t.Fatalf("expected order[%d]=%d, got %d (order=%v)", i, i, order[i], order)
		}
	}
}

func TestBlockReplyPipelineFull_Dedup(t *testing.T) {
	var mu sync.Mutex
	var sent []types.ReplyPayload
	ctx := context.Background()
	p := NewBlockReplyPipelineFull(ctx, BlockReplyPipelineConfig{
		OnBlockReply: func(_ context.Context, payload types.ReplyPayload) error {
			mu.Lock()
			sent = append(sent, payload)
			mu.Unlock()
			return nil
		},
		TimeoutMs: 5000,
	})

	p.Enqueue(types.ReplyPayload{Text: "hello"})
	p.Enqueue(types.ReplyPayload{Text: "hello"}) // duplicate
	p.FlushAndWait(true)

	if len(sent) != 1 {
		t.Fatalf("expected 1 sent payload (dedup), got %d", len(sent))
	}
}

func TestBlockReplyPipelineFull_DifferentThreadingNotDeduped(t *testing.T) {
	// Same text with different replyToId should be sent separately.
	var mu sync.Mutex
	var sent []types.ReplyPayload
	ctx := context.Background()
	p := NewBlockReplyPipelineFull(ctx, BlockReplyPipelineConfig{
		OnBlockReply: func(_ context.Context, payload types.ReplyPayload) error {
			mu.Lock()
			sent = append(sent, payload)
			mu.Unlock()
			return nil
		},
		TimeoutMs: 5000,
	})

	p.Enqueue(types.ReplyPayload{Text: "response text", ReplyToID: "thread-root-1"})
	p.Enqueue(types.ReplyPayload{Text: "response text"})
	p.FlushAndWait(true)

	if len(sent) != 2 {
		t.Fatalf("expected 2 sent payloads (different threading), got %d", len(sent))
	}
}

func TestBlockReplyPipelineFull_HasSentPayloadIgnoresThreading(t *testing.T) {
	ctx := context.Background()
	p := NewBlockReplyPipelineFull(ctx, BlockReplyPipelineConfig{
		OnBlockReply: func(_ context.Context, _ types.ReplyPayload) error { return nil },
		TimeoutMs:    5000,
	})

	p.Enqueue(types.ReplyPayload{Text: "response text", ReplyToID: "thread-root-1"})
	p.FlushAndWait(true)

	// Content-only key should match regardless of replyToId.
	if !p.HasSentPayload(types.ReplyPayload{Text: "response text"}) {
		t.Fatal("expected HasSentPayload to match without replyToId")
	}
	if !p.HasSentPayload(types.ReplyPayload{Text: "response text", ReplyToID: "other-id"}) {
		t.Fatal("expected HasSentPayload to match with different replyToId")
	}
	if p.HasSentPayload(types.ReplyPayload{Text: "different text"}) {
		t.Fatal("expected HasSentPayload to not match different content")
	}
}

func TestPayloadKey_WhitespaceTrimming(t *testing.T) {
	a := PayloadKey(types.ReplyPayload{Text: "  hello  "})
	b := PayloadKey(types.ReplyPayload{Text: "hello"})
	if a != b {
		t.Fatal("payload keys should be equal after whitespace trimming")
	}
}

func TestPayloadKey_DifferentThreading(t *testing.T) {
	p1 := types.ReplyPayload{Text: "hello", ReplyToID: "msg-1"}
	p2 := types.ReplyPayload{Text: "hello", ReplyToID: "msg-2"}
	p3 := types.ReplyPayload{Text: "hello"}

	k1 := PayloadKey(p1)
	k2 := PayloadKey(p2)
	k3 := PayloadKey(p3)

	if k1 == k2 {
		t.Fatal("different ReplyToID should produce different payload keys")
	}
	if k1 == k3 {
		t.Fatal("with and without ReplyToID should produce different payload keys")
	}
}

func TestPayloadKey_DifferentMedia(t *testing.T) {
	a := PayloadKey(types.ReplyPayload{Text: "hello", MediaURLs: []string{"file:///a.png"}})
	b := PayloadKey(types.ReplyPayload{Text: "hello", MediaURLs: []string{"file:///b.png"}})
	if a == b {
		t.Fatal("different media should produce different payload keys")
	}
}

func TestContentKey_IgnoresThreading(t *testing.T) {
	p1 := types.ReplyPayload{Text: "hello", ReplyToID: "msg-1"}
	p2 := types.ReplyPayload{Text: "hello", ReplyToID: "msg-2"}
	p3 := types.ReplyPayload{Text: "hello"}

	c1 := ContentKey(p1)
	c2 := ContentKey(p2)
	c3 := ContentKey(p3)

	if c1 != c2 || c1 != c3 {
		t.Fatal("content keys should be equal regardless of ReplyToID")
	}
}

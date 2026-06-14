package chat

import (
	"context"
	"testing"
)

func TestAcquireInteractiveTurn_BoundsConcurrency(t *testing.T) {
	h := &Handler{interactiveTurnSem: make(chan struct{}, 2)}

	r1, err := h.AcquireInteractiveTurn(context.Background())
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	r2, err := h.AcquireInteractiveTurn(context.Background())
	if err != nil {
		t.Fatalf("second acquire: %v", err)
	}

	// Full (cap 2): a third acquire with an already-canceled ctx must fail fast
	// rather than block forever — the select can only take the ctx.Done() case.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := h.AcquireInteractiveTurn(ctx); err == nil {
		t.Fatal("expected error when full and ctx canceled")
	}

	// Releasing a slot lets a fresh acquire through.
	r1()
	r3, err := h.AcquireInteractiveTurn(context.Background())
	if err != nil {
		t.Fatalf("expected a slot after release: %v", err)
	}

	// Releases are idempotent (double-release must not over-fill the channel).
	r1()
	r2()
	r3()
}

func TestAcquireInteractiveTurn_NilSemIsNoop(t *testing.T) {
	h := &Handler{} // interactiveTurnSem nil — feature disabled
	release, err := h.AcquireInteractiveTurn(context.Background())
	if err != nil {
		t.Fatalf("nil sem must not error: %v", err)
	}
	release() // must not panic
}

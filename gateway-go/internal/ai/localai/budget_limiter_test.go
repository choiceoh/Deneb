package localai

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTokenBudgetLimiterRejectsOversized(t *testing.T) {
	limiter := newTokenBudgetLimiter(10, 0.25)

	if err := limiter.acquire(context.Background(), 11, PriorityNormal); !errors.Is(err, ErrRequestTooLarge) {
		t.Fatalf("normal oversized error = %v, want ErrRequestTooLarge", err)
	}
	if err := limiter.acquire(context.Background(), 13, PriorityCritical); !errors.Is(err, ErrRequestTooLarge) {
		t.Fatalf("critical oversized error = %v, want ErrRequestTooLarge", err)
	}
	if err := limiter.acquire(context.Background(), 12, PriorityCritical); err != nil {
		t.Fatalf("critical request within overdraw limit rejected: %v", err)
	}
	limiter.release(12)
}

func TestTokenBudgetLimiterWaitRespectsContext(t *testing.T) {
	limiter := newTokenBudgetLimiter(10, 0.25)
	if err := limiter.acquire(context.Background(), 10, PriorityNormal); err != nil {
		t.Fatal(err)
	}
	defer limiter.release(10)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- limiter.acquire(ctx, 1, PriorityNormal) }()
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("wait error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("budget wait did not stop after caller cancellation")
	}
}

func TestTokenBudgetLimiterReleaseWakesWaiter(t *testing.T) {
	limiter := newTokenBudgetLimiter(10, 0.25)
	if err := limiter.acquire(context.Background(), 10, PriorityNormal); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- limiter.acquire(context.Background(), 5, PriorityNormal) }()
	limiter.release(10)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("waiter failed after release: %v", err)
		}
		limiter.release(5)
	case <-time.After(time.Second):
		t.Fatal("budget release did not wake waiter")
	}
}

func TestTokenBudgetLimiterCloseWakesWaiter(t *testing.T) {
	limiter := newTokenBudgetLimiter(10, 0.25)
	if err := limiter.acquire(context.Background(), 10, PriorityNormal); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- limiter.acquire(context.Background(), 1, PriorityNormal) }()
	limiter.close()

	select {
	case err := <-done:
		if !errors.Is(err, ErrHubShutdown) {
			t.Fatalf("wait error = %v, want ErrHubShutdown", err)
		}
	case <-time.After(time.Second):
		t.Fatal("budget wait did not stop after limiter close")
	}
	if err := limiter.acquire(context.Background(), 1, PriorityNormal); !errors.Is(err, ErrHubShutdown) {
		t.Fatalf("post-close acquire error = %v, want ErrHubShutdown", err)
	}
	limiter.release(10)
}

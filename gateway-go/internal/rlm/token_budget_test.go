package rlm

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestTokenBudget_Basic(t *testing.T) {
	b := NewTokenBudget(1000)

	if r := b.Remaining(); r != 1000 {
		t.Errorf("expected remaining=1000, got %d", r)
	}
	if u := b.Used(); u != 0 {
		t.Errorf("expected used=0, got %d", u)
	}

	ok := b.Consume(400)
	if !ok {
		t.Error("expected Consume(400) to return true")
	}
	if r := b.Remaining(); r != 600 {
		t.Errorf("expected remaining=600, got %d", r)
	}
}

func TestTokenBudget_Exhaustion(t *testing.T) {
	b := NewTokenBudget(100)

	b.Consume(90)
	if r := b.Remaining(); r != 10 {
		t.Errorf("expected remaining=10, got %d", r)
	}

	// Consume beyond limit.
	ok := b.Consume(50)
	if ok {
		t.Error("expected Consume to return false when exceeding budget")
	}
	if r := b.Remaining(); r != 0 {
		t.Errorf("expected remaining=0 when overdrawn, got %d", r)
	}
	if u := b.Used(); u != 140 {
		t.Errorf("expected used=140, got %d", u)
	}
}

func TestTokenBudget_TryReserve(t *testing.T) {
	b := NewTokenBudget(1000)

	if !b.TryReserve(400) {
		t.Error("expected TryReserve(400) to succeed")
	}
	if r := b.Remaining(); r != 600 {
		t.Errorf("expected remaining=600, got %d", r)
	}

	// Reserve exactly the remaining budget.
	if !b.TryReserve(600) {
		t.Error("expected TryReserve(600) to succeed (exact fit)")
	}
	if r := b.Remaining(); r != 0 {
		t.Errorf("expected remaining=0, got %d", r)
	}

	// No room left — must fail without changing budget.
	if b.TryReserve(1) {
		t.Error("expected TryReserve(1) to fail when budget is 0")
	}
	if u := b.Used(); u != 1000 {
		t.Errorf("expected used=1000, got %d", u)
	}
}

func TestTokenBudget_TryReserveConcurrent(t *testing.T) {
	// 100 goroutines each trying to reserve 100 from a 5000-token budget.
	// Exactly 50 should succeed.
	b := NewTokenBudget(5000)
	var wg sync.WaitGroup
	var successCount atomic.Int32

	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if b.TryReserve(100) {
				successCount.Add(1)
			}
		}()
	}
	wg.Wait()

	if n := successCount.Load(); n != 50 {
		t.Errorf("expected exactly 50 successful reservations, got %d", n)
	}
	if u := b.Used(); u != 5000 {
		t.Errorf("expected used=5000, got %d", u)
	}
}

func TestTokenBudget_ConcurrentConsume(t *testing.T) {
	b := NewTokenBudget(10000)
	var wg sync.WaitGroup

	// 100 goroutines each consuming 100 tokens.
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.Consume(100)
		}()
	}
	wg.Wait()

	if u := b.Used(); u != 10000 {
		t.Errorf("expected used=10000, got %d", u)
	}
	if r := b.Remaining(); r != 0 {
		t.Errorf("expected remaining=0, got %d", r)
	}
}

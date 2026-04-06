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

	if !b.TryReserve(400) {
		t.Error("expected TryReserve(400) to succeed")
	}
	if r := b.Remaining(); r != 600 {
		t.Errorf("expected remaining=600, got %d", r)
	}
}

func TestTokenBudget_Exhaustion(t *testing.T) {
	b := NewTokenBudget(100)

	if !b.TryReserve(90) {
		t.Fatal("expected TryReserve(90) to succeed")
	}
	if r := b.Remaining(); r != 10 {
		t.Errorf("expected remaining=10, got %d", r)
	}

	// Reserve beyond limit — must fail without changing budget.
	if b.TryReserve(50) {
		t.Error("expected TryReserve(50) to fail when only 10 remain")
	}
	if r := b.Remaining(); r != 10 {
		t.Errorf("expected remaining=10 (unchanged after failed reserve), got %d", r)
	}
	if u := b.Used(); u != 90 {
		t.Errorf("expected used=90, got %d", u)
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

func TestTokenBudget_Settle(t *testing.T) {
	b := NewTokenBudget(1000)

	// Reserve 500, actually use 300 → 200 returned to pool.
	if !b.TryReserve(500) {
		t.Fatal("expected TryReserve(500) to succeed")
	}
	if r := b.Remaining(); r != 500 {
		t.Errorf("after reserve: expected remaining=500, got %d", r)
	}

	b.Settle(500, 300)
	if r := b.Remaining(); r != 700 {
		t.Errorf("after settle: expected remaining=700, got %d", r)
	}
	if u := b.Used(); u != 300 {
		t.Errorf("after settle: expected used=300, got %d", u)
	}

	// Reserve 400, fail (error) → all 400 returned.
	if !b.TryReserve(400) {
		t.Fatal("expected TryReserve(400) to succeed")
	}
	b.Settle(400, 0)
	if r := b.Remaining(); r != 700 {
		t.Errorf("after error settle: expected remaining=700, got %d", r)
	}

	// Reserve 200, use more than reserved (350).
	if !b.TryReserve(200) {
		t.Fatal("expected TryReserve(200) to succeed")
	}
	b.Settle(200, 350)
	if u := b.Used(); u != 650 {
		t.Errorf("after over-use settle: expected used=650, got %d", u)
	}
}

func TestTokenBudget_SettleNegativeGuard(t *testing.T) {
	b := NewTokenBudget(1000)

	// Settle without prior TryReserve — consumed would go negative.
	// Guard should clamp to 0.
	b.Settle(500, 0)
	if u := b.Used(); u != 0 {
		t.Errorf("expected used=0 (clamped), got %d", u)
	}
	if r := b.Remaining(); r != 1000 {
		t.Errorf("expected remaining=1000 after clamp, got %d", r)
	}

	// Normal flow should still work after the guard triggered.
	if !b.TryReserve(300) {
		t.Fatal("expected TryReserve(300) to succeed after clamp")
	}
	b.Settle(300, 200)
	if u := b.Used(); u != 200 {
		t.Errorf("expected used=200, got %d", u)
	}
}

func TestTokenBudget_ConcurrentSettle(t *testing.T) {
	b := NewTokenBudget(10000)
	var wg sync.WaitGroup

	// 100 goroutines each reserving 100 and settling with 50 actual.
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if b.TryReserve(100) {
				b.Settle(100, 50)
			}
		}()
	}
	wg.Wait()

	if u := b.Used(); u != 5000 {
		t.Errorf("expected used=5000, got %d", u)
	}
	if r := b.Remaining(); r != 5000 {
		t.Errorf("expected remaining=5000, got %d", r)
	}
}

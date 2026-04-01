package chat

import "testing"

func TestBudgetTracker_Basic(t *testing.T) {
	t.Run("disabled for subagent", func(t *testing.T) {
		bt := NewBudgetTracker()
		d := bt.CheckBudget("sub-1", 10000, 5000)
		if d.Action != "stop" {
			t.Errorf("action = %q, want stop for subagent", d.Action)
		}
	})

	t.Run("disabled for zero budget", func(t *testing.T) {
		bt := NewBudgetTracker()
		d := bt.CheckBudget("", 0, 5000)
		if d.Action != "stop" {
			t.Errorf("action = %q, want stop for zero budget", d.Action)
		}
	})

	t.Run("continues under budget", func(t *testing.T) {
		bt := NewBudgetTracker()
		d := bt.CheckBudget("", 10000, 5000)
		if d.Action != "continue" {
			t.Errorf("action = %q, want continue at 50%%", d.Action)
		}
		if d.Pct != 50 {
			t.Errorf("pct = %d, want 50", d.Pct)
		}
		if d.ContinuationCount != 1 {
			t.Errorf("count = %d, want 1", d.ContinuationCount)
		}
	})

	t.Run("stops at budget threshold", func(t *testing.T) {
		bt := NewBudgetTracker()
		// First check: continue (50%).
		bt.CheckBudget("", 10000, 5000)
		// Second check: at 91% — should stop.
		d := bt.CheckBudget("", 10000, 9100)
		if d.Action != "stop" {
			t.Errorf("action = %q, want stop at 91%%", d.Action)
		}
	})
}

func TestBudgetTracker_DiminishingReturns(t *testing.T) {
	bt := NewBudgetTracker()
	budget := 100000

	// Build up 3 continuations with tiny deltas.
	bt.CheckBudget("", budget, 1000) // cont 1, delta=1000
	bt.CheckBudget("", budget, 1200) // cont 2, delta=200
	bt.CheckBudget("", budget, 1300) // cont 3, delta=100

	// 4th check: diminishing should trigger since delta < 500 for 2 consecutive.
	d := bt.CheckBudget("", budget, 1400)
	if d.Action != "stop" {
		t.Errorf("action = %q, want stop on diminishing returns", d.Action)
	}
	if !d.DiminishingReturns {
		t.Error("expected DiminishingReturns=true")
	}
}

func TestBudgetTracker_Reset(t *testing.T) {
	bt := NewBudgetTracker()
	bt.CheckBudget("", 10000, 5000)
	bt.Reset()
	if bt.continuationCount != 0 {
		t.Errorf("continuationCount = %d after reset", bt.continuationCount)
	}
	if bt.lastDeltaTokens != 0 {
		t.Errorf("lastDeltaTokens = %d after reset", bt.lastDeltaTokens)
	}
}

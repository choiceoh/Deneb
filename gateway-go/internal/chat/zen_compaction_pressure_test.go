package chat

import "testing"

func TestCompactionPressure_UpdateAndCheck(t *testing.T) {
	cp := NewCompactionPressure()

	// No data → not high pressure.
	if cp.IsHighPressure("session-1") {
		t.Fatal("expected no pressure for unknown session")
	}

	// Low utilization (50%) → not high.
	cp.Update("session-1", 50000, 100000, 20)
	if cp.IsHighPressure("session-1") {
		t.Fatal("50% should not be high pressure")
	}

	p := cp.Pressure("session-1")
	if p < 0.49 || p > 0.51 {
		t.Fatalf("expected pressure ~0.5, got %f", p)
	}

	// High utilization (85%) → high pressure.
	cp.Update("session-1", 85000, 100000, 40)
	if !cp.IsHighPressure("session-1") {
		t.Fatal("85% should be high pressure")
	}

	// Clear removes pressure.
	cp.Clear("session-1")
	if cp.IsHighPressure("session-1") {
		t.Fatal("expected no pressure after clear")
	}
}

func TestCompactionPressure_Threshold(t *testing.T) {
	cp := NewCompactionPressure()

	// Exactly at threshold (80%) → high.
	cp.Update("s1", 80000, 100000, 30)
	if !cp.IsHighPressure("s1") {
		t.Fatal("exactly at 80% threshold should be high pressure")
	}

	// Just below threshold (79%) → not high.
	cp.Update("s2", 79000, 100000, 30)
	if cp.IsHighPressure("s2") {
		t.Fatal("79% should not be high pressure")
	}
}

func TestCompactionPressure_ZeroBudget(t *testing.T) {
	cp := NewCompactionPressure()

	// Zero budget should not panic or report high pressure.
	cp.Update("s1", 50000, 0, 10)
	if cp.IsHighPressure("s1") {
		t.Fatal("zero budget should not be high pressure")
	}
	if p := cp.Pressure("s1"); p != 0 {
		t.Fatalf("zero budget pressure should be 0, got %f", p)
	}
}

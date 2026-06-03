package metrics

import "testing"

func TestCacheHitTracker(t *testing.T) {
	var c CacheHitTracker

	// Empty tracker: ratio is 0, no division by zero.
	if got := c.HitRatio(); got != 0 {
		t.Errorf("empty HitRatio = %v, want 0", got)
	}

	// Non-positive values are ignored.
	c.Record(0, 0, 0)
	c.Record(-5, -1, -2)
	if cr, cc, fi := c.Snapshot(); cr != 0 || cc != 0 || fi != 0 {
		t.Errorf("after ignored records Snapshot = (%d,%d,%d), want (0,0,0)", cr, cc, fi)
	}

	// Record across two turns. The 3rd arg is the DISJOINT uncached remainder
	// (Anthropic usage.input_tokens), not a grand-total, so the buckets simply
	// sum: read=80+10, creation=10+0, fresh=10+0.
	c.Record(80, 10, 10)
	c.Record(10, 0, 0)

	cr, cc, fi := c.Snapshot()
	if cr != 90 || cc != 10 || fi != 10 {
		t.Fatalf("Snapshot = (%d,%d,%d), want (90,10,10)", cr, cc, fi)
	}

	// Ratio = cacheRead / (read+creation+fresh) = 90 / 110.
	want := 90.0 / 110.0
	if got := c.HitRatio(); got != want {
		t.Errorf("HitRatio = %v, want %v", got, want)
	}
	// HitRatioOf on the snapshot must match the method.
	if got := HitRatioOf(cr, cc, fi); got != want {
		t.Errorf("HitRatioOf = %v, want %v", got, want)
	}

	// RecentRatio is an EWMA over the two positive records: first (80,10,10)
	// ratio 0.8 seeds it, then (10,0,0) ratio 1.0 blends in:
	// 0.1*1.0 + 0.9*0.8 = 0.82.
	recent, ok := c.RecentRatio()
	if !ok {
		t.Fatal("RecentRatio ok = false, want true after positive records")
	}
	if diff := recent - 0.82; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("RecentRatio = %v, want 0.82", recent)
	}
}

// TestCacheHitTracker_RecentRatioEmpty verifies RecentRatio reports not-seen
// until a record carries prompt tokens.
func TestCacheHitTracker_RecentRatioEmpty(t *testing.T) {
	var c CacheHitTracker
	if _, ok := c.RecentRatio(); ok {
		t.Error("RecentRatio ok = true on empty tracker, want false")
	}
	c.Record(0, 0, 0) // no prompt tokens → must not seed the EWMA
	if _, ok := c.RecentRatio(); ok {
		t.Error("RecentRatio ok = true after zero-token record, want false")
	}
}

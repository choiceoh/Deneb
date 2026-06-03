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

	// Record across two turns: read=80+10, creation=10+0, fresh=10+0.
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
}

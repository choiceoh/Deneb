package proactive

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAlertGateDeduplicatesOperationalConditions(t *testing.T) {
	t.Parallel()
	gate := NewAlertGate()
	t0 := time.Date(2026, 6, 15, 9, 0, 0, 0, time.UTC)

	if !gate.ShouldRelay("low memory headroom: srv2", "warn", t0) {
		t.Fatal("first alert should relay")
	}
	if gate.ShouldRelay("low memory headroom: srv2", "warn", t0.Add(10*time.Minute)) {
		t.Error("unchanged repeat should be suppressed")
	}
	if gate.ShouldRelay("low memory headroom: srv2", "warn", t0.Add(2*time.Hour)) {
		t.Error("repeat inside the cooldown should be suppressed")
	}
	if !gate.ShouldRelay("low memory headroom: srv2", "bad", t0.Add(2*time.Hour+time.Minute)) {
		t.Error("level change should relay immediately")
	}
	if !gate.ShouldRelay("low memory headroom: srv2", "ok", t0.Add(3*time.Hour)) {
		t.Error("recovery should relay immediately")
	}
	if !gate.ShouldRelay("low memory headroom: srv2", "ok", t0.Add(3*time.Hour+alertReNotify)) {
		t.Error("standing condition should re-notify after the cooldown")
	}
	if !gate.ShouldRelay("low memory headroom: srv3", "warn", t0.Add(10*time.Minute)) {
		t.Error("a distinct title should relay independently")
	}
}

func TestAlertGateAllowsOnlyOneConcurrentFirstSighting(t *testing.T) {
	t.Parallel()
	var gate AlertGate
	now := time.Date(2026, 6, 15, 9, 0, 0, 0, time.UTC)
	var relayed atomic.Int32
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if gate.ShouldRelay("node down: srv3", "bad", now) {
				relayed.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := relayed.Load(); got != 1 {
		t.Fatalf("concurrent first sightings relayed %d times, want 1", got)
	}
}

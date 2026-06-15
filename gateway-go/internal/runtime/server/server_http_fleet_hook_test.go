package server

import (
	"testing"
	"time"
)

func TestFleetAlertGate_Dedup(t *testing.T) {
	g := newFleetAlertGate()
	t0 := time.Date(2026, 6, 15, 9, 0, 0, 0, time.UTC)

	// First sighting of a title relays.
	if !g.shouldRelay("low memory headroom: srv2", "warn", t0) {
		t.Fatal("first alert should relay")
	}
	// Same (title, level) a few minutes later — the heartbeat repeat — is suppressed.
	if g.shouldRelay("low memory headroom: srv2", "warn", t0.Add(10*time.Minute)) {
		t.Error("repeat of the same standing condition should be suppressed")
	}
	if g.shouldRelay("low memory headroom: srv2", "warn", t0.Add(2*time.Hour)) {
		t.Error("repeat within the re-notify window should be suppressed")
	}
	// A level change (warn→bad escalation) always relays immediately.
	if !g.shouldRelay("low memory headroom: srv2", "bad", t0.Add(2*time.Hour+time.Minute)) {
		t.Error("level change should relay immediately")
	}
	// Recovery (→ok) relays.
	if !g.shouldRelay("low memory headroom: srv2", "ok", t0.Add(3*time.Hour)) {
		t.Error("recovery should relay")
	}
	// A still-unchanged condition re-surfaces once the re-notify window elapses.
	if !g.shouldRelay("low memory headroom: srv2", "ok", t0.Add(3*time.Hour+fleetAlertReNotify)) {
		t.Error("standing condition should re-notify after the cooldown")
	}

	// A different node's title is tracked independently.
	if !g.shouldRelay("low memory headroom: srv3", "warn", t0.Add(10*time.Minute)) {
		t.Error("a distinct title should relay on its first sighting")
	}
}

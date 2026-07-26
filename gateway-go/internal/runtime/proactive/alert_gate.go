// Package proactive owns proactive relay policy and the shared alert gate that
// keeps operational alerts useful without over-notifying the user.
package proactive

import (
	"sync"
	"time"
)

// alertReNotify is how long an unchanged operational condition stays
// suppressed. Level changes and recoveries always relay immediately.
const alertReNotify = 6 * time.Hour

// AlertGate deduplicates operational alerts by stable title and level.
//
// Its zero value is ready to use. A gate is safe for concurrent webhook and
// watchdog calls and records time only when an alert is actually relayed, so
// repeated sightings do not extend the cooldown indefinitely.
type AlertGate struct {
	mu   sync.Mutex
	seen map[string]alertSeen
}

type alertSeen struct {
	level    string
	lastSent time.Time
}

// NewAlertGate constructs a shared operational alert gate.
func NewAlertGate() *AlertGate {
	return &AlertGate{}
}

// ShouldRelay reports whether an alert should be published now and records the
// decision when it returns true. The first sighting, a level change, and the
// first unchanged sighting after the cooldown all relay.
func (g *AlertGate) ShouldRelay(title, level string, now time.Time) bool {
	if g == nil {
		return true
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.seen == nil {
		g.seen = make(map[string]alertSeen)
	}
	prev, ok := g.seen[title]
	relay := !ok || prev.level != level || now.Sub(prev.lastSent) >= alertReNotify
	if relay {
		g.seen[title] = alertSeen{level: level, lastSent: now}
	}
	return relay
}

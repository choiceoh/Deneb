package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

// fleetAlertReNotify is how long a standing fleet condition stays suppressed
// before it is re-pushed. A level change (warn→bad, →ok recovery) is always
// pushed immediately; only an unchanged repeat waits out this window. Keeps a
// persistent "low memory headroom" from spamming the phone every heartbeat while
// still re-surfacing a problem that never clears.
const fleetAlertReNotify = 6 * time.Hour

// fleetAlertGate dedups SparkFleet alerts by title: it pushes the first time a
// title is seen, on any level change, and at most once per fleetAlertReNotify
// while the level is unchanged. Independent mutex — holds no other lock.
type fleetAlertGate struct {
	mu   sync.Mutex
	seen map[string]fleetAlertSeen
}

type fleetAlertSeen struct {
	level    string
	lastSent time.Time
}

func newFleetAlertGate() *fleetAlertGate {
	return &fleetAlertGate{seen: make(map[string]fleetAlertSeen)}
}

// shouldRelay reports whether an alert (title, level) should be pushed now, and
// records the decision when it returns true so the cooldown is measured from the
// last *push*, not the last sighting.
func (g *fleetAlertGate) shouldRelay(title, level string, now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	prev, ok := g.seen[title]
	relay := !ok || prev.level != level || now.Sub(prev.lastSent) >= fleetAlertReNotify
	if relay {
		g.seen[title] = fleetAlertSeen{level: level, lastSent: now}
	}
	return relay
}

// handleFleetHook receives SparkFleet's webhook alerts (its `generic` notify
// dialect: {"source":"sparkfleet","level":"ok|warn|bad","title":...,
// "message":...}) and fans them straight out to connected native clients as a
// push notification — node down/up, container restart loops, job done/failed,
// low unified-memory headroom land on the operator's phone instead of only in
// a log nobody is watching (the 2026-06-12 silent SparkFleet death is the
// motivating incident).
//
// Deterministic by design: no agent judgment turn in the loop — an
// operational alert must arrive verbatim and immediately. Auth matches
// /api/event/ingest: loopback-only (SparkFleet runs on this same host and
// posts to 127.0.0.1; the gateway may bind wider). Delivery is best-effort
// SSE fan-out, same as every proactive push.
//
// Deduped by [fleetAlertGate]: SparkFleet re-emits standing conditions (a node
// stuck at low memory headroom) on every heartbeat, which without this pushed the
// identical alert to the phone every few minutes (285×/day observed). A repeat of
// the same (title, level) is suppressed until it changes or fleetAlertReNotify
// elapses — over-notification the project forbids.
func (s *Server) handleFleetHook(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRemote(r.RemoteAddr) {
		s.writeJSON(w, http.StatusForbidden, map[string]any{"error": "localhost only"})
		return
	}
	var ev struct {
		Source  string `json:"source"`
		Level   string `json:"level"` // ok | warn | bad
		Title   string `json:"title"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
		s.writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid JSON body"})
		return
	}
	if strings.TrimSpace(ev.Title) == "" && strings.TrimSpace(ev.Message) == "" {
		s.writeJSON(w, http.StatusBadRequest, map[string]any{"error": "title or message required"})
		return
	}

	// Dedup standing conditions: a repeat of the same (title, level) is
	// suppressed until it changes or the re-notify window elapses. Keyed on the
	// raw title so a level transition (warn→bad, →ok) still re-keys and pushes.
	if s.fleetAlerts != nil && !s.fleetAlerts.shouldRelay(ev.Title, ev.Level, time.Now()) {
		s.writeJSON(w, http.StatusOK, map[string]any{"ok": true, "suppressed": true})
		return
	}

	badge := "🛰"
	switch ev.Level {
	case "bad":
		badge = "🔴"
	case "warn":
		badge = "⚠️"
	}
	title := strings.TrimSpace(badge + " 플릿 · " + ev.Title)
	if s.pushHub != nil {
		s.pushHub.publish(clientPushEvent{Title: title, Body: ev.Message, Kind: pushKindFleet})
	}
	s.logger.Info("fleet alert relayed to clients", "level", ev.Level, "title", ev.Title)
	s.writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

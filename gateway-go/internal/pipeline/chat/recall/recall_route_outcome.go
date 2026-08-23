package recall

// recall_route_outcome.go — did the turn actually follow the routing hint?
//
// The hint (recall_route.go) tells the model that this query shape cannot be
// answered from page snippets: sum the ledger, enumerate the roster, read the
// mail archive, walk the graph. Whether the model obeys is currently unknown —
// the hint is emitted and never checked, so a hint that stopped working would
// look exactly like one that works.
//
// This closes that loop with the same consume-once shape as the citation
// ledger: the preflight parks the shape it emitted, and the end of the turn
// compares it against the tools the run actually called. Log-only, no scoring
// and no behavior change — the follow-rate is a number to read in the journal
// before anyone tunes the hints.

import (
	"log/slog"
	"strings"
	"sync"
)

var recallRoutingStore = struct {
	mu    sync.Mutex
	store map[string]string // session key → shape emitted by the last preflight
}{store: make(map[string]string)}

// routingExpectedTools names the tools each shape's hint points at. A turn that
// calls any of them followed the hint; anything else did not.
var routingExpectedTools = map[string][]string{
	"ledger-total":    {"deal_ledger"},
	"aggregate":       {"deal_ledger", "wiki"},
	"temporal-mail":   {"mail_archive"},
	"graph-relations": {"graphify"},
}

// storeRoutingShape parks the shape for the session, replacing any previous one
// (the earlier turn's hint is already spent). An empty shape clears the slot so
// a hint-free turn cannot inherit the last turn's expectation.
func storeRoutingShape(sessionKey, shape string) {
	if sessionKey == "" {
		return
	}
	recallRoutingStore.mu.Lock()
	defer recallRoutingStore.mu.Unlock()
	if shape == "" {
		delete(recallRoutingStore.store, sessionKey)
		return
	}
	recallRoutingStore.store[sessionKey] = shape
}

// takeRoutingShape returns and clears the session's parked shape.
func takeRoutingShape(sessionKey string) string {
	if sessionKey == "" {
		return ""
	}
	recallRoutingStore.mu.Lock()
	defer recallRoutingStore.mu.Unlock()
	shape := recallRoutingStore.store[sessionKey]
	delete(recallRoutingStore.store, sessionKey)
	return shape
}

// RecordRoutingOutcome logs whether the turn used one of the tools its routing
// hint pointed at. No-op when no hint fired this turn (the common case), so the
// log stays as low-volume as the hint itself.
func RecordRoutingOutcome(sessionKey string, toolNames []string, logger *slog.Logger) {
	shape := takeRoutingShape(sessionKey)
	if shape == "" || logger == nil {
		return
	}
	expected := routingExpectedTools[shape]
	matched := firstExpectedTool(toolNames, expected)
	logger.Info("recall routing outcome",
		"shape", shape,
		"followed", matched != "",
		"tool", matched,
		"tools", strings.Join(dedupeTools(toolNames), ","))
}

// firstExpectedTool returns the first expected tool the turn called, or "".
func firstExpectedTool(toolNames, expected []string) string {
	for _, name := range toolNames {
		for _, want := range expected {
			if name == want {
				return name
			}
		}
	}
	return ""
}

// dedupeTools keeps the call order but drops repeats — a turn that calls wiki
// eight times should not fill the log line with it.
func dedupeTools(names []string) []string {
	seen := make(map[string]bool, len(names))
	out := make([]string, 0, len(names))
	for _, n := range names {
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out
}

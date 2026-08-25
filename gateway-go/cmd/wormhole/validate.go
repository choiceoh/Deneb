package main

import (
	"log/slog"
	"strings"
)

// validateConfig returns human-readable warnings about a config that would load
// fine but route wrong — caught at load/reload time instead of as a runtime error
// on the first request. It never fails the load (a bad reload must not take down
// the hot path); it surfaces problems so the operator fixes them.
func validateConfig(cfg config) []string {
	var warns []string
	seen := make(map[string]bool, len(cfg.Models))
	for _, e := range cfg.Models {
		if e.Name == "" {
			warns = append(warns, "a model has an empty name")
			continue
		}
		if seen[e.Name] {
			warns = append(warns, "duplicate model name "+e.Name+" — the last one wins, routing is ambiguous")
		}
		seen[e.Name] = true
		if strings.TrimSpace(e.URL) == "" && !e.Fleet {
			warns = append(warns, "model "+e.Name+" has an empty url")
		}
		if e.Fleet && cfg.Sparkfleet == nil {
			warns = append(warns, "model "+e.Name+" is fleet-backed (fleet:true) but no top-level sparkfleet source is configured — it can never resolve a URL")
		}
		// Check the raw field: protocol() silently collapses anything non-"anthropic"
		// to openai, so a typo ("anthropics") would route wrong with no error.
		if e.Protocol != "" && e.Protocol != protocolOpenAI && e.Protocol != protocolAnthropic {
			warns = append(warns, "model "+e.Name+" has unknown protocol "+e.Protocol+" (want openai or anthropic) — it will be treated as openai")
		}
		// thinkingMode steers the ToggleKwarg injection, so a mode without the
		// kwarg name (or an unknown mode) silently does nothing — route wrong.
		if e.ThinkingMode != thinkingModeJudge && e.ThinkingMode != thinkingModeOff && e.ThinkingMode != thinkingModeOffUnlessHard {
			warns = append(warns, "model "+e.Name+" has unknown thinkingMode "+e.ThinkingMode+` (want "off" or "off-unless-hard") — it will be treated as the default judge mode`)
		}
		if e.ThinkingMode != thinkingModeJudge && e.ToggleKwarg == "" {
			warns = append(warns, "model "+e.Name+" sets thinkingMode but no toggleKwarg (the model's thinking switch name) — no thinking routing will happen")
		}
		proto := e.protocol()
		// The anthropic /v1 gotcha: wormhole appends only "/messages" to the entry
		// url, so an anthropic url must end in /v1 (e.g. https://api.z.ai/api/anthropic/v1).
		// A bare base (as deneb.json carries, since its client appends /v1/messages)
		// 404s. This is the single most common wormhole misconfig — catch it here.
		if proto == protocolAnthropic && e.URL != "" && !strings.HasSuffix(strings.TrimRight(e.URL, "/"), "/v1") {
			warns = append(warns, "model "+e.Name+": anthropic url should end in /v1 (got "+e.URL+") — wormhole appends /messages, so a bare base returns 404")
		}
	}
	for _, a := range cfg.Auto {
		if a != "" && !seen[a] {
			warns = append(warns, "auto candidate "+a+" is not a configured model")
		}
	}
	// Fallback targets resolve at request time (failoverChain), so a typo or a
	// protocol mismatch silently means "no failover" — exactly the moment the
	// operator counted on it. Catch it at load time.
	byName := make(map[string]modelEntry, len(cfg.Models))
	for _, e := range cfg.Models {
		byName[e.Name] = e
	}
	for _, e := range cfg.Models {
		fb := strings.TrimSpace(e.Fallback)
		if fb == "" {
			continue
		}
		target, ok := byName[fb]
		switch {
		case fb == e.Name:
			warns = append(warns, "model "+e.Name+" declares itself as fallback — no failover will happen")
		case !ok:
			warns = append(warns, "model "+e.Name+" fallback "+fb+" is not a configured model — no failover will happen")
		case target.protocol() != e.protocol():
			warns = append(warns, "model "+e.Name+" fallback "+fb+" speaks a different protocol ("+target.protocol()+" vs "+e.protocol()+") — no failover will happen")
		}
	}
	// A metered hop anywhere in an unmetered entry's chain means a dead local
	// model bills per token, silently — the 2026-08 leak (see modelEntry.Metered).
	for _, e := range cfg.Models {
		if e.Metered {
			continue
		}
		if hop, chain := meteredHop(byName, e); hop != "" {
			warns = append(warns, "model "+e.Name+" fails over to metered model "+hop+" ("+chain+") — a local outage would bill per token")
		}
	}
	return warns
}

// logConfigWarnings runs validation and logs each warning, so a misconfig shows
// up the moment the config loads or hot-reloads rather than on first use.
func logConfigWarnings(log *slog.Logger, cfg config) {
	for _, w := range validateConfig(cfg) {
		log.Warn("config", "warning", w)
	}
}

// meteredHop walks e's failover chain and returns the first metered entry it
// reaches (plus the chain for the message), or "" when the chain stays
// unmetered. Cycle-guarded like failoverChain.
func meteredHop(byName map[string]modelEntry, e modelEntry) (string, string) {
	seen := map[string]bool{e.Name: true}
	chain := []string{e.Name}
	for cur := e; strings.TrimSpace(cur.Fallback) != ""; {
		next, ok := byName[strings.TrimSpace(cur.Fallback)]
		if !ok || seen[next.Name] {
			return "", ""
		}
		seen[next.Name] = true
		chain = append(chain, next.Name)
		if next.Metered {
			return next.Name, strings.Join(chain, " -> ")
		}
		cur = next
	}
	return "", ""
}

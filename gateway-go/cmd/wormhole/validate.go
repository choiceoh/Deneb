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
		if strings.TrimSpace(e.URL) == "" {
			warns = append(warns, "model "+e.Name+" has an empty url")
		}
		// Check the raw field: protocol() silently collapses anything non-"anthropic"
		// to openai, so a typo ("anthropics") would route wrong with no error.
		if e.Protocol != "" && e.Protocol != protocolOpenAI && e.Protocol != protocolAnthropic {
			warns = append(warns, "model "+e.Name+" has unknown protocol "+e.Protocol+" (want openai or anthropic) — it will be treated as openai")
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
	return warns
}

// logConfigWarnings runs validation and logs each warning, so a misconfig shows
// up the moment the config loads or hot-reloads rather than on first use.
func logConfigWarnings(log *slog.Logger, cfg config) {
	for _, w := range validateConfig(cfg) {
		log.Warn("config", "warning", w)
	}
}

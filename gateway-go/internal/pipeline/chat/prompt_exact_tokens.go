// prompt_exact_tokens.go resolves the EXACT token size of the assembled prompt
// head by asking the serving engine, instead of estimating it.
//
// Why it matters here and not everywhere: finalizePrompt spends
// SystemPromptBudget on the head and gives what is left to tier-1 memory. The
// head is around 40K tokens against a 45K budget, so the remainder is the small
// difference of two large numbers — and the estimator's ~12% band on the head
// is larger than the remainder itself. Estimating the head does not make the
// memory budget slightly wrong, it makes it arbitrary.
//
// This is a direct-connection capability. The question "how many tokens is this
// string, to you" has no place in the OpenAI chat protocol; the engine answers
// it on POST /tokenize. Routed through a proxy the gateway can only guess.
package chat

import (
	"log/slog"
	"sync"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/enginespeed"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/enginetokenize"
)

// exactTokens holds one counter per configured engine: each caches counts in
// its own engine's tokenizer, so two engines serving different models never
// answer from each other's cache. Package-level and env-derived to match the
// engine APC sampler next door (engine_cache_sample.go) rather than threading a
// second engine handle through every run.
var exactTokens struct {
	mu       sync.Mutex
	counters map[string]*enginetokenize.Counter // endpoint → counter; nil = unusable URL
}

// exactPromptTokens returns the engine's own token count for text when it is
// already known, and false otherwise — including whenever no local engine is
// configured or reachable. Never blocks a turn: a miss schedules one background
// fill, so the first turn on a given head estimates and the rest are exact.
// The prompt-cache doctrine keeps that head byte-stable across turns, which is
// what makes the cache hit.
//
// The count comes from the engine that serves the run (engineRoute), whose
// tokenizer is the one that will actually read the head. A run no configured
// engine serves — a cloud model — is counted by the first engine listed. With
// one engine that is the engine every run has always been counted by, so
// adding a second engine changes nothing for the runs neither serves. Each URL
// passes the private-host rule in enginetokenize.New (no userinfo, query or
// fragment either) or yields a counter that always misses.
func exactPromptTokens(text string, route engineRoute, logger *slog.Logger) (int, bool) {
	endpoints := enginespeed.Endpoints()
	if len(endpoints) == 0 {
		return 0, false
	}
	endpoint := endpoints[0]
	// One engine leaves nothing to choose, so nothing is read. Among several,
	// placing the run reads the router's config — a small local file, the same
	// read the liveness watcher makes on every probe.
	if len(endpoints) > 1 {
		if served := route.servingEngine(endpoints); served != "" {
			endpoint = served
		}
	}
	return exactTokenCounter(endpoint, logger).Exact(text)
}

// exactTokenCounter returns the counter for endpoint, building it on first use.
func exactTokenCounter(endpoint string, logger *slog.Logger) *enginetokenize.Counter {
	exactTokens.mu.Lock()
	defer exactTokens.mu.Unlock()
	counter, ok := exactTokens.counters[endpoint]
	if !ok {
		if exactTokens.counters == nil {
			exactTokens.counters = make(map[string]*enginetokenize.Counter)
		}
		counter = enginetokenize.NewCounter(endpoint, logger)
		exactTokens.counters[endpoint] = counter
	}
	return counter
}

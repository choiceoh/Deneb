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
	"os"
	"strings"
	"sync"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/enginetokenize"
)

// exactTokens holds the process-wide counter, rebuilt when the operator points
// the engine URL somewhere else. Package-level and env-derived to match the
// engine APC sampler next door (engine_cache_sample.go) rather than threading a
// second engine handle through every run.
var exactTokens struct {
	mu      sync.Mutex
	builtAt string // the env value this counter was built from
	counter *enginetokenize.Counter
}

// exactPromptTokens returns the engine's own token count for text when it is
// already known, and false otherwise — including whenever no local engine is
// configured or reachable. Never blocks a turn: a miss schedules one background
// fill, so the first turn on a given head estimates and the rest are exact.
// The prompt-cache doctrine keeps that head byte-stable across turns, which is
// what makes the cache hit.
func exactPromptTokens(text string, logger *slog.Logger) (int, bool) {
	endpoint := strings.TrimSpace(os.Getenv(engineMetricsURLEnv))
	if endpoint == "" {
		return 0, false
	}
	exactTokens.mu.Lock()
	if exactTokens.builtAt != endpoint {
		exactTokens.counter = enginetokenize.NewCounter(endpoint, logger)
		exactTokens.builtAt = endpoint
	}
	counter := exactTokens.counter
	exactTokens.mu.Unlock()

	return counter.Exact(text)
}

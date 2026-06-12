// run_fallback.go — model fallback chain for one agent turn: stall and
// circuit-breaker synthesis (errModelStalled / errModelCircuitOpen) and
// runAgentWithFallback, which retries the turn across the role's fallback
// models. Called from executeAgentRun (run_exec.go).
package chat

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	compact "github.com/choiceoh/deneb/gateway-go/internal/pipeline/compaction"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/pilot"
)

// errModelStalled marks a turn that timed out without producing any output (an
// LLM stream stall). It is synthesized inside runAgentWithFallback so a stall
// engages the model fallback chain the same way a hard error does.
var errModelStalled = errors.New("model produced no output before timeout (stream stall)")

// stallFallbackBudget bounds the fallback attempt when a stall has already
// consumed the per-turn deadline. A stall surfaces as a timeout result only
// after the parent ctx is spent, so the fallback needs a fresh budget — but a
// bounded one, so a wedged turn can't run unbounded. Single-user: a slightly
// late answer from a healthy model beats silence.
const stallFallbackBudget = 90 * time.Second

// errModelCircuitOpen marks a turn whose initial model was skipped because its
// circuit breaker is open (repeated recent failures — see modelrole/health.go).
// Synthesized instead of calling RunAgent so the fallback chain engages
// immediately and the user is spared the dead model's stall timeout.
var errModelCircuitOpen = errors.New("model circuit open: skipped after repeated recent failures")

// healthyFallbackExists reports whether the fallback chain for role offers at
// least one model that is distinct from failedModel and whose breaker is
// closed. The initial-model skip only happens when this holds — when every
// candidate is unhealthy, trying the requested model is still the best move.
func healthyFallbackExists(reg *modelrole.Registry, role modelrole.Role, failedModel string) bool {
	chain := reg.FallbackChain(role)
	for i := 1; i < len(chain); i++ {
		cfg := reg.Config(chain[i])
		if cfg.Model == "" || cfg.Model == failedModel {
			continue
		}
		if !reg.ModelUnhealthy(cfg.Model) {
			return true
		}
	}
	return false
}

// isStalledResult reports whether an agent run timed out without emitting any
// text — the signature of a stalled LLM stream (the inner per-run timeout fired
// before a single token arrived). A turn that timed out after producing text is
// left alone: the user already got a partial answer, so falling back would only
// discard it.
func isStalledResult(r *agent.AgentResult) bool {
	return r != nil && r.StopReason == "timeout" && strings.TrimSpace(r.AllText) == ""
}

// Stage 3: runAgentWithFallback — agent loop with compaction retry + model fallback.
// ---------------------------------------------------------------------------

// runAgentWithFallback executes the agent loop with mid-loop compaction retries
// on context overflow, transient HTTP error retries, and model fallback chain.
func runAgentWithFallback(
	ctx context.Context,
	cfg agent.AgentConfig,
	messages []llm.Message,
	client *llm.Client,
	deps runDeps,
	providerID string,
	initialRole modelrole.Role,
	hooks agent.StreamHooks,
	logger *slog.Logger,
	runLog *agentlog.RunLogger,
) (*agent.AgentResult, string, bool, error) {
	const maxCompactionRetries = 2

	contextBudget := effectiveContextBudget(deps, providerID, cfg.Model, logger)

	// Anti-thrashing state (see compact_guard.go):
	//   - lastCompactInputHash detects idempotent compaction — if the
	//     prior attempt's input slice hashes to the same value, another
	//     compact.Compact call will produce the same output and we'll
	//     retry the same failure in a loop.
	//   - protectedZoneExceedsBudget detects physically-impossible
	//     sessions where even with a zero-byte middle, the head+tail
	//     protected zones alone exceed budget.
	// On either condition we bail with stopReasonCompressionStuck so the
	// user sees a Korean "can't compress further, try /reset" message
	// instead of another cryptic "context overflow" from the provider.
	var lastCompactInputHash string

	var agentResult *agent.AgentResult
	var runErr error
	// Track which model actually answered. Starts as the requested model and
	// only changes if the fallback chain below fires.
	actualModel := cfg.Model
	fellBack := false
	// stalledResult holds the original empty timeout result when the main model
	// stalled. If no fallback model recovers, we return this rather than the
	// fallback's error — preserving the pre-fallback "stall = empty reply"
	// behavior instead of surfacing a downstream error.
	var stalledResult *agent.AgentResult
	// Circuit breaker: when the requested model has failed repeatedly within
	// the cooldown window AND the chain offers a healthy alternative, skip the
	// initial attempt and go straight to the fallback chain. Saves the user
	// the dead model's stall timeout on every turn while it is down; the
	// cooldown re-admits the model automatically once it has aged out.
	skipInitial := deps.registry != nil &&
		deps.registry.ModelUnhealthy(cfg.Model) &&
		healthyFallbackExists(deps.registry, initialRole, cfg.Model)
	if skipInitial {
		logger.Warn("model circuit open; skipping straight to fallback chain",
			"model", cfg.Model, "role", string(initialRole))
	}
	for compactAttempt := 0; compactAttempt <= maxCompactionRetries; compactAttempt++ {
		if skipInitial {
			runErr = errModelCircuitOpen
		} else {
			agentResult, runErr = agent.RunAgent(ctx, cfg, messages, client, deps.tools, hooks, logger, runLog)
		}
		// A stall (timed out with zero output) returns no error but an empty
		// timeout result. Treat it as a failure so the fallback chain below gets
		// a shot with a different model. Only the inner per-run timeout fired,
		// not the parent ctx, so fallback attempts still have budget.
		if runErr == nil && isStalledResult(agentResult) {
			logger.Warn("model stalled (no output before timeout); engaging fallback chain",
				"model", cfg.Model, "stopReason", agentResult.StopReason)
			runErr = errModelStalled
			stalledResult = agentResult
		}
		// Effort-router escalation: a thinking-disabled run that stalled gets
		// one retry with thinking restored before the model fallback chain
		// fires. The prefix is KV-cached, so the retry re-enters cheaply.
		if errors.Is(runErr, errModelStalled) && effortRouterApplied(&cfg) {
			logger.Info("effort router: non-thinking run stalled; escalating to thinking",
				"model", cfg.Model)
			stripEffortOverride(&cfg)
			agentResult, runErr = agent.RunAgent(ctx, cfg, messages, client, deps.tools, hooks, logger, runLog)
			if runErr == nil && isStalledResult(agentResult) {
				runErr = errModelStalled
				stalledResult = agentResult
			}
		}
		if runErr == nil {
			break
		}

		// Mid-loop compaction retry: on context overflow, strip images and
		// emergency-summarize to reduce context before retrying.
		if isContextOverflow(runErr) && compactAttempt < maxCompactionRetries && ctx.Err() == nil {
			// Early-abort guard A: head + tail protected zone already
			// exceeds budget. Compaction cannot reduce below budget even
			// with a zero-byte middle, so skip straight to the user-visible
			// stuck message.
			if protectedZoneExceedsBudget(messages, contextBudget) {
				logger.Warn("compaction skipped: protected zone exceeds budget",
					"messageCount", len(messages),
					"budget", contextBudget,
					"attempt", compactAttempt+1)
				if deps.broadcast != nil {
					deps.broadcast("chat.compaction_stuck", ChatCompactionStuckEvent{
						Reason:       "protected_zone_exceeds_budget",
						MessageCount: len(messages),
						Budget:       contextBudget,
					})
				}
				return &agent.AgentResult{
					StopReason:    stopReasonCompressionStuck,
					FinalMessages: messages,
				}, cfg.Model, false, nil
			}

			// Early-abort guard B: input hash matches the prior attempt.
			// The cheap-first shrink pipeline + LLM summarizer already ran
			// and produced a slice that's byte-identical to what we fed in
			// last time. Another compact.Compact call will not do anything
			// new, so stop burning the retry budget.
			inputHash := hashMessages(messages)
			if lastCompactInputHash != "" && inputHash == lastCompactInputHash {
				logger.Warn("compaction skipped: identical input as prior attempt",
					"messageCount", len(messages),
					"inputHash", inputHash,
					"attempt", compactAttempt+1)
				if deps.broadcast != nil {
					deps.broadcast("chat.compaction_stuck", ChatCompactionStuckEvent{
						Reason:       "idempotent_compaction",
						MessageCount: len(messages),
						InputHash:    inputHash,
					})
				}
				return &agent.AgentResult{
					StopReason:    stopReasonCompressionStuck,
					FinalMessages: messages,
				}, cfg.Model, false, nil
			}
			lastCompactInputHash = inputHash

			logger.Warn("context overflow, attempting mid-loop compaction",
				"attempt", compactAttempt+1,
				"maxRetries", maxCompactionRetries,
				"messageCount", len(messages),
				"error", runErr)

			// Cheap-first shrink pipeline (no LLM calls):
			// 1) Structurally truncate long tool-call argument strings.
			//    Protects against naive byte-slice truncation producing
			//    invalid JSON that providers reject non-retryably.
			// 2) Replace image blocks with text stubs.
			messages = compact.TruncateToolCallArgs(messages, 400)
			messages = compact.StripImageBlocks(messages)

			// Emergency summarize: keep head 2 + tail 8, summarize the middle.
			if len(messages) > 10 {
				var summarizer compact.Summarizer
				if pilotHub := pilot.LocalAIHub(); pilotHub != nil {
					summarizer = &localAISummarizer{}
				}
				if summarizer != nil {
					compactCfg := compact.NewConfig(contextBudget)
					compactCtx, compactCancel := context.WithTimeout(ctx, 30*time.Second)
					messages, _ = compact.Compact(compactCtx, compactCfg, messages, summarizer, logger)
					compactCancel()
				}
			}
			continue
		}

		// Transient HTTP retry: 500/502/503/521/529/429 → wait 2.5s, retry once.
		//
		// Classification is delegated to llmerr so the decision shares the
		// same taxonomy as isContextOverflow above and the autoreply runner.
		// We deliberately whitelist the narrow set of reasons the prior
		// string-based IsTransientError matched (5xx server errors, overload,
		// rate limits) plus transport timeouts — keeping ReasonUnknown and
		// non-HTTP signals out so we don't over-retry on truly unknown
		// failures or auth/billing issues.
		if ctx.Err() == nil && isTransientLLMError(runErr) {
			logger.Warn("transient HTTP error, retrying once", "error", runErr)
			select {
			case <-ctx.Done():
				return nil, "", false, ctx.Err()
			case <-time.After(2500 * time.Millisecond):
			}
			agentResult, runErr = agent.RunAgent(ctx, cfg, messages, client, deps.tools, hooks, logger, runLog)
			if runErr != nil {
				logger.Warn("transient retry also failed", "error", runErr)
			}
		}

		// Model fallback chain: try each subsequent role in the chain.
		// e.g., Main → Lightweight → Fallback
		if runErr != nil && deps.registry != nil {
			// Choose the context for fallback attempts. A hard error leaves the
			// parent ctx alive (budget remains, so reuse it). A stall, however,
			// only surfaces once the per-turn deadline is already spent waiting
			// on the dead model — so give the fallback a fresh, bounded budget,
			// otherwise the user gets silence instead of an answer from a
			// healthy model. A user abort yields StopReason "aborted" (not
			// "timeout"), so it never reaches this stall branch.
			fbCtx, fbCancel := ctx, context.CancelFunc(nil)
			runFallback := true
			if ctx.Err() != nil {
				if errors.Is(runErr, errModelStalled) {
					fbCtx, fbCancel = context.WithTimeout(context.WithoutCancel(ctx), stallFallbackBudget)
				} else {
					runFallback = false // parent canceled for another reason — respect it
				}
			}
			if runFallback {
				// Feed the circuit breaker: a hard error or stall counts against
				// the model's health. Context overflow does not (input-size
				// problem, not a model fault) and neither does the synthetic
				// circuit-open sentinel (the model was never tried).
				if !isContextOverflow(runErr) && !errors.Is(runErr, errModelCircuitOpen) {
					deps.registry.RecordModelFailure(cfg.Model)
				}
				chain := deps.registry.FallbackChain(initialRole)
				// Skip models already attempted — the chain can list the same model
				// for multiple roles (e.g. main == lightweight), and re-running a
				// model that just stalled only burns the budget.
				triedModels := map[string]bool{cfg.Model: true}
				for i := 1; i < len(chain); i++ {
					if fbCtx.Err() != nil {
						break
					}
					fbRole := chain[i]
					// Re-discover what the local vLLM is serving before targeting
					// the role (rate-limited; no-op for non-vllm roles). Without
					// this, a model swapped on the server after gateway startup
					// 404s every fallback until a restart.
					fbCfg := deps.registry.RefreshVllmRole(fbRole)
					fbClient := deps.registry.Client(fbRole)
					if fbClient == nil || triedModels[fbCfg.Model] {
						continue
					}
					triedModels[fbCfg.Model] = true
					logger.Warn("model failed, trying fallback",
						"failedRole", string(chain[i-1]),
						"nextRole", string(fbRole),
						"nextModel", fbCfg.Model,
						"error", runErr)
					agentCfg := cfg
					agentCfg.Model = fbCfg.Model
					// The effort-router override is model-specific (vLLM
					// chat_template_kwargs): never carry it to a fallback
					// model whose server/template may reject or misread it.
					if !supportsThinkingToggle(agentCfg.Model) {
						stripEffortOverride(&agentCfg)
					}
					agentResult, runErr = agent.RunAgent(fbCtx, agentCfg, messages, fbClient, deps.tools, hooks, logger, runLog)
					// A stalled fallback (empty timeout) is also a failure — advance
					// to the next role instead of returning its empty result.
					if runErr == nil && isStalledResult(agentResult) {
						runErr = errModelStalled
					}
					if runErr == nil {
						actualModel = fbCfg.Model
						fellBack = true
						break
					}
					if fbCtx.Err() == nil {
						// Only count failures the model itself produced — a spent
						// fallback budget says nothing about the model's health.
						deps.registry.RecordModelFailure(fbCfg.Model)
					}
					logger.Error("fallback also failed",
						"role", string(fbRole), "model", fbCfg.Model, "error", runErr)
				}
			}
			if fbCancel != nil {
				fbCancel()
			}
		}

		if runErr != nil {
			// The main model stalled and no fallback model produced an answer
			// (it stalled too, or errored — e.g. a provider rejecting the
			// history). Degrade to the original empty timeout result rather than
			// surfacing the fallback's error, matching the prior behavior from
			// before stalls engaged the fallback chain.
			if stalledResult != nil && !fellBack {
				return stalledResult, actualModel, false, nil
			}
			// Surface unrecoverable context overflow so operators/UI see it.
			// Without this the only signal was a Warn log in the retry loop
			// and the final error return — easy to miss when diagnosing
			// "why did the bot suddenly stop on long sessions".
			if isContextOverflow(runErr) && deps.broadcast != nil {
				deps.broadcast("chat.context_overflow_unrecoverable", ChatContextOverflowEvent{
					Model:        cfg.Model,
					MessageCount: len(messages),
					Attempts:     maxCompactionRetries + 1,
					Error:        runErr.Error(),
				})
				logger.Error("context overflow: all compaction retries exhausted",
					"model", cfg.Model,
					"messageCount", len(messages),
					"attempts", maxCompactionRetries+1,
					"error", runErr)
			}
			return nil, "", false, runErr
		}
		break // success via transient retry or fallback
	}

	// Close the answering model's breaker. Failures were already recorded at
	// the fallback-engagement points above; the stalled-degrade and
	// compression-stuck paths return earlier and never reach here.
	if deps.registry != nil {
		deps.registry.RecordModelSuccess(actualModel)
	}
	return agentResult, actualModel, fellBack, nil
}

// resolveThinkingConfig maps a session ThinkingLevel string to an llm.ThinkingConfig.
// Returns nil for "off", empty, or unrecognized levels (disables extended thinking).

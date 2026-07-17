// run_fallback.go — model fallback chain for one agent turn: stall and
// circuit-breaker synthesis (errModelStalled / errModelCircuitOpen) and
// runAgentWithFallback, which retries the turn across the role's fallback
// models. Called from executeAgentRun (run_exec.go).
//
// Structure: runAgentWithFallback owns only the retry ladder's ORDER; each
// rung lives in its own fallbackTurn method (initial attempt, effort
// escalation, compaction recovery, transient retry, thinking-strip retry,
// fallback chain, failure finalization). The fallbackTurn struct carries the
// state the original 385-line function threaded through local variables —
// behavior is unchanged, the rungs are just individually readable/testable.
package chat

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/core/agentlog"
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

// maxCompactionRetries bounds the mid-loop compaction retry ladder: on context
// overflow the turn is compacted and re-run at most this many times.
const maxCompactionRetries = 2

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

// isStalledResult reports whether an agent run timed out without emitting
// ANYTHING — the signature of a stalled LLM stream (timeout before a single
// token, no completed rounds). Two shapes are deliberately excluded:
//
//   - timed out after producing text: the user already got a partial answer;
//     falling back would only discard it.
//   - timed out mid-work after tool rounds (Turns > 1 — a tool round always
//     leaves Turns >= 2, the same pin isEmptyFinalResult uses): the model was
//     alive and productive, its budget just ran out. Automation turns narrate
//     nothing (all output goes to tool inputs), so "no visible text" alone
//     cannot mean "stalled". Re-running such a run on a fallback model repeats
//     the committed tools' side effects and feeds the circuit breaker a fault
//     the model didn't commit (live 2026-07-17: a mailpoll run filed wiki
//     pages on k3[1m] for 6 rounds, hit the 360s stage-2 budget with zero
//     prose, was misread as a stall, and glm re-ran the whole analysis).
func isStalledResult(r *agent.AgentResult) bool {
	return r != nil && r.StopReason == "timeout" && r.Turns <= 1 &&
		strings.TrimSpace(r.AllText) == ""
}

// isEmptyFinalResult reports whether a "successful" run is an accidental
// empty completion: end_turn after tool activity with zero text anywhere —
// a model emitting stop right after tool results. Distinct from intentional
// silence, which uses the NO_REPLY token and therefore has non-empty raw
// text. Delivered as-is it reaches the user as a blank bubble with ok=true
// (measured from the puppet seat); both delivery paths substitute the
// fallbackForEmptyFinalReply notice instead. No auto-rerun: the turn already
// executed tools, and re-running them — unlike the turn-0 stall retry, which
// fires before any tool ran — can repeat side effects. Turns > 1 pins the
// tool-activity requirement: the counter increments per LLM round, so a tool
// round always leaves Turns >= 2, while a plain single-shot answer stays out.
func isEmptyFinalResult(r *agent.AgentResult) bool {
	return r != nil && r.StopReason == "end_turn" && r.Turns > 1 &&
		strings.TrimSpace(r.AllText) == ""
}

// Stage 3: runAgentWithFallback — agent loop with compaction retry + model fallback.
// ---------------------------------------------------------------------------

// fallbackTurn carries one turn's retry-ladder state across the rungs. The
// immutable inputs are set once by runAgentWithFallback; cfg/messages/route
// evolve as recovery steps rewrite them (compaction shrinks messages, the
// effort escalation restores thinking on cfg, the thinking-strip retry drops
// signed blocks).
type fallbackTurn struct {
	// Immutable for the turn.
	deps          runDeps
	client        *llm.Client
	providerID    string
	initialRole   modelrole.Role
	hooks         agent.StreamHooks
	logger        *slog.Logger
	runLog        *agentlog.RunLogger
	contextBudget int
	// skipInitial short-circuits the initial attempt with errModelCircuitOpen
	// when the requested model's breaker is open AND the chain offers a healthy
	// alternative. Saves the user the dead model's stall timeout on every turn
	// while it is down; the cooldown re-admits the model automatically.
	skipInitial bool

	// Evolving state.
	cfg      agent.AgentConfig
	messages []llm.Message
	route    *effortRoute

	agentResult *agent.AgentResult
	runErr      error
	// actualModel tracks which model actually answered. Starts as the
	// requested model and only changes if the fallback chain fires.
	actualModel string
	fellBack    bool
	// stalledResult holds the original empty timeout result when the main model
	// stalled. If no fallback model recovers, we return this rather than the
	// fallback's error — preserving the pre-fallback "stall = empty reply"
	// behavior instead of surfacing a downstream error.
	stalledResult *agent.AgentResult
	// lastCompactInputHash detects idempotent compaction — if the prior
	// attempt's input slice hashes to the same value, another compact.Compact
	// call will produce the same output and we'd retry the same failure in a
	// loop (see compact_guard.go).
	lastCompactInputHash string
}

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
	route *effortRoute,
	hooks agent.StreamHooks,
	logger *slog.Logger,
	runLog *agentlog.RunLogger,
) (*agent.AgentResult, string, bool, error) {
	// Briefcase binds maxTurns and timeout as hard signed budgets. Production's
	// retry/fallback ladder can execute a fresh Agent loop (and repeat tool side
	// effects), so deterministic runs make exactly one attempt and fail closed.
	if deps.briefcaseMode {
		result, err := agent.RunAgent(ctx, cfg, messages, client, deps.tools, hooks, logger, runLog)
		return result, cfg.Model, false, err
	}
	t := &fallbackTurn{
		deps:          deps,
		client:        client,
		providerID:    providerID,
		initialRole:   initialRole,
		hooks:         hooks,
		logger:        logger,
		runLog:        runLog,
		contextBudget: effectiveContextBudget(deps, providerID, cfg.Model, logger),
		cfg:           cfg,
		messages:      messages,
		route:         route,
		actualModel:   cfg.Model,
	}
	t.skipInitial = deps.registry != nil &&
		deps.registry.ModelUnhealthy(cfg.Model) &&
		healthyFallbackExists(deps.registry, initialRole, cfg.Model)
	if t.skipInitial {
		logger.Warn("model circuit open; skipping straight to fallback chain",
			"model", cfg.Model, "role", string(initialRole))
	}

	for compactAttempt := 0; compactAttempt <= maxCompactionRetries; compactAttempt++ {
		t.runInitialAttempt(ctx)
		t.maybeEscalateEffort(ctx)
		if t.runErr == nil {
			break
		}

		retry, stuck := t.compactionRecovery(ctx, compactAttempt)
		if stuck != nil {
			return stuck, t.cfg.Model, false, nil
		}
		if retry {
			continue
		}

		if aborted, err := t.retryTransient(ctx); aborted {
			return nil, "", false, err
		}
		t.retryThinkingStrip(ctx)
		t.walkFallbackChain(ctx)

		if t.runErr != nil {
			return t.finalizeFailure()
		}
		break // success via transient retry or fallback
	}

	// Close the answering model's breaker. Failures were already recorded at
	// the fallback-engagement points above; the stalled-degrade and
	// compression-stuck paths return earlier and never reach here.
	if deps.registry != nil {
		deps.registry.RecordModelSuccess(t.actualModel)
	}
	return t.agentResult, t.actualModel, t.fellBack, nil
}

// synthStall converts a "no error but empty timeout result" outcome into
// errModelStalled so it engages the recovery ladder the same way a hard error
// does. recordStalled preserves the original stalled result for the degrade
// path (the fallback chain's own attempts don't overwrite it).
func (t *fallbackTurn) synthStall(recordStalled bool) {
	if t.runErr == nil && isStalledResult(t.agentResult) {
		t.runErr = errModelStalled
		if recordStalled {
			t.stalledResult = t.agentResult
		}
	}
}

// runInitialAttempt runs the turn on the requested model (or synthesizes the
// circuit-open sentinel when skipInitial is set). A stall (timed out with zero
// output) returns no error but an empty timeout result — treat it as a failure
// so the fallback chain gets a shot with a different model. Only the inner
// per-run timeout fired, not the parent ctx, so fallback attempts still have
// budget.
func (t *fallbackTurn) runInitialAttempt(ctx context.Context) {
	if t.skipInitial {
		t.runErr = errModelCircuitOpen
		return
	}
	t.agentResult, t.runErr = agent.RunAgent(ctx, t.cfg, t.messages, t.client, t.deps.tools, t.hooks, t.logger, t.runLog)
	switch {
	case t.runErr == nil && isStalledResult(t.agentResult):
		t.logger.Warn("model stalled (no output before timeout); engaging fallback chain",
			"model", t.cfg.Model, "stopReason", t.agentResult.StopReason)
	case t.runErr == nil && t.agentResult != nil && t.agentResult.StopReason == "timeout" &&
		t.agentResult.Turns > 1 && strings.TrimSpace(t.agentResult.AllText) == "":
		// Not a stall: the model worked through tool rounds and ran out of
		// budget. No fallback — a re-run would repeat committed side effects.
		t.logger.Warn("run budget exhausted mid-work; not engaging fallback",
			"model", t.cfg.Model, "turns", t.agentResult.Turns)
	}
	t.synthStall(true)
}

// maybeEscalateEffort gives a routed (thinking-disabled) run that failed in a
// router-attributable shape — stall, silently idle stream, or degenerate empty
// success — ONE thinking-restored retry on the same model before the fallback
// chain. The prefix is KV-cached so re-entry is cheap. Runs that already
// executed tools are excluded: re-running them would double tool side effects
// (message.send, exec); those flow to the fallback chain under its pre-existing
// re-run semantics. The retry budget mirrors the stall fallback: a stall
// surfaces with the parent ctx spent (fresh bounded budget via WithoutCancel),
// while a live parent stays cancelable but bounded so a wedged server cannot
// double the turn's wait.
func (t *fallbackTurn) maybeEscalateEffort(ctx context.Context) {
	if t.route == nil || !effortRouted(&t.cfg) ||
		!escalatableEffortFailure(t.runErr, t.agentResult) ||
		resultRanTools(t.agentResult) ||
		(ctx.Err() != nil && !errors.Is(t.runErr, errModelStalled)) {
		return
	}
	t.logger.Info("effort router: non-thinking run failed; retrying with thinking restored",
		"model", t.cfg.Model, "error", t.runErr)
	restoreEffort(&t.cfg, t.route)
	t.route.escalated = true // visible to the caller's run-complete record
	t.route = nil            // one shot; cfg keeps the restored thinking from here on
	escCtx, escCancel := context.WithTimeout(ctx, stallFallbackBudget)
	if ctx.Err() != nil {
		escCancel()
		escCtx, escCancel = context.WithTimeout(context.WithoutCancel(ctx), stallFallbackBudget)
	}
	t.agentResult, t.runErr = agent.RunAgent(escCtx, t.cfg, t.messages, t.client, t.deps.tools, t.hooks, t.logger, t.runLog)
	escCancel()
	t.synthStall(true)
}

// compactionRecovery handles a context-overflow failure: shrink the message
// slice (cheap passes + emergency summarize) and signal the caller to retry.
// Returns retry=true when the turn should re-run on the shrunk slice, or a
// non-nil stuck result when the anti-thrashing guards (compact_guard.go) prove
// compaction cannot succeed — the caller surfaces the Korean "can't compress
// further, try /reset" message instead of another cryptic provider error.
func (t *fallbackTurn) compactionRecovery(ctx context.Context, compactAttempt int) (retry bool, stuck *agent.AgentResult) {
	if !isContextOverflow(t.runErr) || compactAttempt >= maxCompactionRetries || ctx.Err() != nil {
		return false, nil
	}

	// Early-abort guard A: head + tail protected zone already exceeds budget.
	// Compaction cannot reduce below budget even with a zero-byte middle, so
	// skip straight to the user-visible stuck message.
	if protectedZoneExceedsBudget(t.messages, t.contextBudget) {
		t.logger.Warn("compaction skipped: protected zone exceeds budget",
			"messageCount", len(t.messages),
			"budget", t.contextBudget,
			"attempt", compactAttempt+1)
		if t.deps.broadcast != nil {
			broadcastPayload(t.deps.broadcast, "chat.compaction_stuck", ChatCompactionStuckEvent{
				Reason:       "protected_zone_exceeds_budget",
				MessageCount: len(t.messages),
				Budget:       t.contextBudget,
			})
		}
		return false, &agent.AgentResult{
			StopReason:    stopReasonCompressionStuck,
			FinalMessages: t.messages,
		}
	}

	// Early-abort guard B: input hash matches the prior attempt. The
	// cheap-first shrink pipeline + LLM summarizer already ran and produced a
	// slice that's byte-identical to what we fed in last time. Another
	// compact.Compact call will not do anything new, so stop burning the
	// retry budget.
	inputHash := hashMessages(t.messages)
	if t.lastCompactInputHash != "" && inputHash == t.lastCompactInputHash {
		t.logger.Warn("compaction skipped: identical input as prior attempt",
			"messageCount", len(t.messages),
			"inputHash", inputHash,
			"attempt", compactAttempt+1)
		if t.deps.broadcast != nil {
			broadcastPayload(t.deps.broadcast, "chat.compaction_stuck", ChatCompactionStuckEvent{
				Reason:       "idempotent_compaction",
				MessageCount: len(t.messages),
				InputHash:    inputHash,
			})
		}
		return false, &agent.AgentResult{
			StopReason:    stopReasonCompressionStuck,
			FinalMessages: t.messages,
		}
	}
	t.lastCompactInputHash = inputHash

	t.logger.Warn("context overflow, attempting mid-loop compaction",
		"attempt", compactAttempt+1,
		"maxRetries", maxCompactionRetries,
		"messageCount", len(t.messages),
		"error", t.runErr)

	// Cheap-first shrink pipeline (no LLM calls):
	// 1) Structurally truncate long tool-call argument strings. Protects
	//    against naive byte-slice truncation producing invalid JSON that
	//    providers reject non-retryably.
	// 2) Replace image blocks with text stubs.
	t.messages = compact.TruncateToolCallArgs(t.messages, 400)
	t.messages = compact.StripImageBlocks(t.messages)

	// Emergency summarize: keep head 2 + tail 8, summarize the middle.
	if len(t.messages) > 10 {
		var summarizer compact.Summarizer
		if pilotHub := pilot.LocalAIHub(); pilotHub != nil && !t.deps.briefcaseMode {
			summarizer = &localAISummarizer{}
		}
		if summarizer != nil {
			compactCfg := compact.NewConfig(t.contextBudget)
			compactCtx, compactCancel := context.WithTimeout(ctx, 30*time.Second)
			t.messages, _ = compact.Compact(compactCtx, compactCfg, t.messages, summarizer, t.logger)
			compactCancel()
		}
	}
	return true, nil
}

// transientRetryBackoff is the pause before a single transient-HTTP retry.
// Tests shrink this via TestMain — production keeps the live 2.5s settle time.
var transientRetryBackoff = 2500 * time.Millisecond

// retryTransient retries once, after a short backoff, on transient HTTP
// failures: 500/502/503/521/529/429 plus transport timeouts.
//
// Classification is delegated to llmerr so the decision shares the same
// taxonomy as isContextOverflow and the autoreply runner. We deliberately
// whitelist the narrow set of reasons the prior string-based IsTransientError
// matched — keeping ReasonUnknown and non-HTTP signals out so we don't
// over-retry on truly unknown failures or auth/billing issues.
//
// aborted=true means the parent ctx died during the backoff wait; the caller
// must return err immediately (no stalled-degrade, no overflow broadcast —
// the turn was cancelled, not failed).
func (t *fallbackTurn) retryTransient(ctx context.Context) (aborted bool, err error) {
	if ctx.Err() != nil || !isTransientLLMError(t.runErr) {
		// No-op passthrough, not an abort: t.runErr stays set and flows to the
		// caller's finalizeFailure (mirrors the pre-split skip semantics).
		return false, nil //nolint:nilerr // deliberate — pending t.runErr is surfaced downstream
	}
	t.logger.Warn("transient HTTP error, retrying once", "error", t.runErr)
	select {
	case <-ctx.Done():
		return true, ctx.Err()
	case <-time.After(transientRetryBackoff):
	}
	t.agentResult, t.runErr = agent.RunAgent(ctx, t.cfg, t.messages, t.client, t.deps.tools, t.hooks, t.logger, t.runLog)
	if t.runErr != nil {
		t.logger.Warn("transient retry also failed", "error", t.runErr)
	}
	return false, nil
}

// retryThinkingStrip recovers from an Anthropic thinking-signature rejection:
// a stale/invalid thinking-block signature (e.g. a mid-loop compaction shifted
// the prefix the block was signed against, or signed reasoning was replayed to
// a model that did not mint it) makes the provider reject the echoed thinking
// blocks with a 400. The recovery classified in llmerr (ReasonThinkingSignature
// -> Action{StripThink, RetryOnce}) is to drop thinking blocks from the history
// and retry once on the same model — without it the identical broken history is
// re-sent and the turn wedges until /reset. Stripping rewrites the in-memory
// call slice only (never the transcript), and the stripped history also helps
// the fallback chain if this retry fails. Only retried when blocks were
// actually removed (n > 0); otherwise the same request would 400 again.
func (t *fallbackTurn) retryThinkingStrip(ctx context.Context) {
	if t.runErr == nil || ctx.Err() != nil || !shouldStripThinking(t.runErr) {
		return
	}
	strippedMsgs, n := compact.StripThinkingBlocks(t.messages)
	if n == 0 {
		return
	}
	t.logger.Warn("thinking-block signature rejected; stripping thinking blocks and retrying once",
		"model", t.cfg.Model, "stripped", n, "error", t.runErr)
	t.messages = strippedMsgs
	t.agentResult, t.runErr = agent.RunAgent(ctx, t.cfg, t.messages, t.client, t.deps.tools, t.hooks, t.logger, t.runLog)
	t.synthStall(true)
	if t.runErr != nil {
		t.logger.Warn("thinking-strip retry also failed", "error", t.runErr)
	}
}

// walkFallbackChain tries each subsequent role in the fallback chain
// (e.g., Main → Lightweight → Fallback) until one produces a successful turn.
func (t *fallbackTurn) walkFallbackChain(ctx context.Context) {
	if t.runErr == nil || t.deps.registry == nil {
		return
	}

	// Choose the context for fallback attempts. A hard error leaves the parent
	// ctx alive (budget remains, so reuse it). A stall, however, only surfaces
	// once the per-turn deadline is already spent waiting on the dead model —
	// so give the fallback a fresh, bounded budget, otherwise the user gets
	// silence instead of an answer from a healthy model. A user abort yields
	// StopReason "aborted" (not "timeout"), so it never reaches this stall branch.
	fbCtx, fbCancel := ctx, context.CancelFunc(nil)
	if ctx.Err() != nil {
		if !errors.Is(t.runErr, errModelStalled) {
			return // parent canceled for another reason — respect it
		}
		fbCtx, fbCancel = context.WithTimeout(context.WithoutCancel(ctx), stallFallbackBudget)
	}
	if fbCancel != nil {
		defer fbCancel()
	}

	// Feed the circuit breaker: a hard error or stall counts against the
	// model's health. Context overflow does not (input-size problem, not a
	// model fault) and neither does the synthetic circuit-open sentinel (the
	// model was never tried).
	if !isContextOverflow(t.runErr) && !errors.Is(t.runErr, errModelCircuitOpen) {
		t.deps.registry.RecordModelFailure(t.cfg.Model)
	}
	chain := t.deps.registry.FallbackChain(t.initialRole)
	// Skip models already attempted — the chain can list the same model for
	// multiple roles (e.g. main == lightweight), and re-running a model that
	// just stalled only burns the budget.
	triedModels := map[string]bool{t.cfg.Model: true}
	// The role whose failure this iteration recovers from. NOT chain[i-1]:
	// that can be a rung skipped without an attempt (unassigned model / no
	// client), which blamed dormant main2 for main's failures in the journal
	// (observed 2026-07-17 while diagnosing kimi fallbacks).
	failedRole := t.initialRole
	for i := 1; i < len(chain); i++ {
		if fbCtx.Err() != nil {
			break
		}
		fbRole := chain[i]
		// Re-discover what the local vLLM is serving before targeting the role
		// (rate-limited; no-op for non-vllm roles). Without this, a model
		// swapped on the server after gateway startup 404s every fallback
		// until a restart.
		fbCfg := t.deps.registry.RefreshVllmRole(fbRole)
		fbClient := t.deps.registry.Client(fbRole)
		if fbClient == nil || triedModels[fbCfg.Model] {
			continue
		}
		triedModels[fbCfg.Model] = true
		t.logger.Warn("model failed, trying fallback",
			"failedRole", string(failedRole),
			"nextRole", string(fbRole),
			"nextModel", fbCfg.Model,
			"error", t.runErr)
		agentCfg := t.cfg
		agentCfg.Model = fbCfg.Model
		// cfg's cache_control policy (system markers + trailing-marker hook)
		// was applied for the ORIGINAL provider in run_exec.go; a fallback can
		// cross to a provider with the opposite policy — marker-rejecting
		// (Kimi: every attempt 400s) or Anthropic-mode behind an OpenAI-mode
		// main (runs uncached). Reconcile per attempt.
		reconcileFallbackCacheMarkers(&agentCfg, t.deps, t.providerID, t.cfg.Model,
			fbCfg.ProviderID, fbCfg.Model, t.client, fbClient, t.logger)
		// Routed runs: carry "disabled" only to fallback models whose template
		// supports the toggle (provider-aware capability lookup); every other
		// fallback gets the session's ORIGINAL thinking back — never a bare
		// nil that would erase the session's reasoning config (GLM leaks
		// chain-of-thought without it; step3p7 truncates, see
		// applySamplingParams).
		if t.route != nil && effortRouted(&t.cfg) {
			if fbProfile := routingProfileForRun(t.deps, fbCfg.ProviderID, fbCfg.Model); fbProfile.Enabled {
				// Rebuild (not drop) the per-step policy on the FALLBACK
				// model's own profile — the original closure carries the old
				// kwarg/thresholds, and nil-ing it would pin every fallback
				// turn non-thinking with no per-step revert.
				fbDisabled := &llm.ThinkingConfig{Type: "disabled", TemplateKwarg: fbProfile.ToggleKwarg}
				agentCfg.Thinking = fbDisabled
				agentCfg.ThinkingModulator = effortStepModulator(fbProfile, fbDisabled, t.route.origThinking)
			} else {
				restoreEffort(&agentCfg, t.route)
			}
		}
		t.agentResult, t.runErr = agent.RunAgent(fbCtx, agentCfg, t.messages, fbClient, t.deps.tools, t.hooks, t.logger, t.runLog)
		// A stalled fallback (empty timeout) is also a failure — advance to
		// the next role instead of returning its empty result.
		t.synthStall(false)
		if t.runErr == nil {
			t.actualModel = fbCfg.Model
			t.fellBack = true
			return
		}
		failedRole = fbRole
		if fbCtx.Err() == nil {
			// Only count failures the model itself produced — a spent fallback
			// budget says nothing about the model's health.
			t.deps.registry.RecordModelFailure(fbCfg.Model)
		}
		t.logger.Error("fallback also failed",
			"role", string(fbRole), "model", fbCfg.Model, "error", t.runErr)
	}
}

// finalizeFailure terminates an iteration whose recovery ladder ended with
// runErr still set.
func (t *fallbackTurn) finalizeFailure() (*agent.AgentResult, string, bool, error) {
	// The main model stalled and no fallback model produced an answer (it
	// stalled too, or errored — e.g. a provider rejecting the history).
	// Degrade to the original empty timeout result rather than surfacing the
	// fallback's error, matching the prior behavior from before stalls engaged
	// the fallback chain.
	if t.stalledResult != nil && !t.fellBack {
		return t.stalledResult, t.actualModel, false, nil
	}
	// Surface unrecoverable context overflow so operators/UI see it. Without
	// this the only signal was a Warn log in the retry loop and the final
	// error return — easy to miss when diagnosing "why did the bot suddenly
	// stop on long sessions".
	if isContextOverflow(t.runErr) && t.deps.broadcast != nil {
		broadcastPayload(t.deps.broadcast, "chat.context_overflow_unrecoverable", ChatContextOverflowEvent{
			Model:        t.cfg.Model,
			MessageCount: len(t.messages),
			Attempts:     maxCompactionRetries + 1,
			Error:        t.runErr.Error(),
		})
		t.logger.Error("context overflow: all compaction retries exhausted",
			"model", t.cfg.Model,
			"messageCount", len(t.messages),
			"attempts", maxCompactionRetries+1,
			"error", t.runErr)
	}
	return nil, "", false, t.runErr
}

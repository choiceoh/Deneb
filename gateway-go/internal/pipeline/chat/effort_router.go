// effort_router.go — chat-side adapter for the model-agnostic effort router.
//
// The routing POLICY (is this run simple enough to skip thinking?) lives in the
// reusable internal/ai/router package, parameterized by a per-model
// router.Profile that modelrole resolves (builtin defaults + deneb.json
// overrides). This file owns the chat-specific LIFECYCLE the policy can't:
// translating a RunParams turn into a router.Request, swapping the agent's
// thinking config, the per-step modulator, and the escalation/fallback restore.
//
// Dual-mode models (DeepSeek V4 family today) ship with an always-thinking
// serving default that wastes tokens and latency on conversational turns:
// disabling thinking per-request cuts completion tokens ~58% and wall-clock
// ~42-62% on simple queries while the KV prefix cache survives the toggle (the
// template flag only changes the generation tail). The router disables thinking
// ONLY for obviously-simple short conversational turns; the error asymmetry
// (a false-easy costs answer quality, a false-hard merely wastes tokens) keeps
// thinking for automation, attachments, and any analysis/code/planning signal.
// A routed run that fails in a router-attributable way gets one thinking-
// restored retry before the model fallback chain (run_fallback.go); the prefix
// is KV-cached, so the escalation re-enters cheaply.
//
// Mechanically the router only swaps cfg.Thinking to {Type: "disabled",
// TemplateKwarg: profile.ToggleKwarg}: the provider layer translates that to
// vLLM chat_template_kwargs, Anthropic-wire providers ignore "disabled", and
// models with no toggle resolve to an inert (Enabled=false) profile.
//
// Routing is opt-in via DENEB_ADAPTIVE_EFFORT=1.
package chat

import (
	"errors"
	"log/slog"
	"os"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/router"
)

// envFlagEnabled reports whether an opt-in env flag is truthy. Shared by the
// chat package's experiment flags (reasoning sandwich, effort router).
func envFlagEnabled(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "on", "yes":
		return true
	default:
		return false
	}
}

// Effort-router operating modes (DENEB_ADAPTIVE_EFFORT).
const (
	effortModeOff      = ""         // default: router inert
	effortModeAdaptive = "adaptive" // any truthy value: heuristic routing
	effortModeForce    = "force"    // eval-only: route every eligible run (always-non baseline)
)

// effortMode resolves the router's operating mode from the env opt-in.
// "force" exists for the acceptance harness (scripts/dev/effort-eval.sh): a
// RouterBench-style comparison needs the always-non fixed policy as one of
// the interpolation endpoints. Never use force in production.
func effortMode() string {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("DENEB_ADAPTIVE_EFFORT")))
	switch v {
	case "force":
		return effortModeForce
	case "1", "true", "on", "yes", "adaptive":
		return effortModeAdaptive
	default:
		return effortModeOff
	}
}

// effortRoute records what the router replaced so escalation and the model
// fallback chain can restore the session's original thinking configuration
// with plain field assignment (no shared state to strip). Reason/escalated
// feed the structured run-complete record (label-pipeline raw data).
type effortRoute struct {
	origThinking  *llm.ThinkingConfig
	origModulator func(turn int, acts []agent.ToolActivity) *llm.ThinkingConfig
	reason        string
	escalated     bool
}

// isAutomationRun reports whether a turn is non-conversational automation whose
// message is a job instruction, not chatter — these always keep thinking so
// NO_REPLY/delivery judgments never run degraded. Two markers cover the surface:
// EphemeralUser (heartbeat, boot, skill review, event ingest, mail QA) and the
// "cron:"/"acp:" session prefixes (cron persists its transcript, so it is NOT
// ephemeral). AutoDeliveredOutput is deliberately NOT checked: it is delivery
// semantics and is set on the interactive native client path too.
func isAutomationRun(params RunParams) bool {
	return params.EphemeralUser ||
		strings.HasPrefix(params.SessionKey, "cron:") ||
		strings.HasPrefix(params.SessionKey, "acp:")
}

// applyEffortRouter swaps the run's thinking config to "disabled" when the
// resolved profile is enabled (the model supports a template toggle) and the
// router judges the user input obviously simple. Must run AFTER model
// resolution (buildAgentConfig leaves cfg.Model empty). Returns the route to
// restore on escalation/fallback (nil when not routed) plus the decision string
// ("routed:…"/"kept:…", "" when the router gate is closed) for the structured
// run-complete record.
func applyEffortRouter(cfg *agent.AgentConfig, params RunParams, messages []llm.Message, profile router.Profile, logger *slog.Logger) (*effortRoute, string) {
	// Always arm the thinking-runaway recovery for models with a chat_template
	// off-toggle (dsv4), independent of routing/mode: a KEPT-thinking run (e.g.
	// kept:automation cron analysis) can still loop in the thinking channel until
	// max_tokens, and dsv4 can't lower effort — only this off-toggle escapes it.
	if profile.ToggleKwarg != "" {
		cfg.ThinkingOffRetry = &llm.ThinkingConfig{Type: "disabled", TemplateKwarg: profile.ToggleKwarg}
	}
	mode := effortMode()
	if mode == effortModeOff || !profile.Enabled {
		return nil, ""
	}
	dec := router.Decide(profile, router.Request{
		Message:        params.Message,
		HasAttachments: len(params.Attachments) > 0,
		IsAutomation:   isAutomationRun(params),
		History:        messages,
	})
	off, reason := dec.ThinkingOff, dec.Reason
	// Force mode (eval baseline) overrides only the HEURISTIC tail: the
	// automation/attachments/empty guards stand even under force — the dev
	// gateway shares production cron jobs, and forcing those non-thinking
	// would degrade real NO_REPLY/delivery judgments mid-eval.
	if mode == effortModeForce && !off && !protectedEffortReason(reason) {
		off, reason = true, "forced"
	}
	if !off {
		return nil, "kept:" + reason
	}
	route := &effortRoute{
		origThinking:  cfg.Thinking,
		origModulator: cfg.ThinkingModulator,
		reason:        reason,
	}
	disabled := &llm.ThinkingConfig{Type: "disabled", TemplateKwarg: profile.ToggleKwarg}
	cfg.Thinking = disabled
	cfg.ThinkingModulator = effortStepModulator(profile, disabled, route.origThinking)
	if logger != nil {
		logger.Info("effort router: thinking off for this run",
			"reason", reason, "model", cfg.Model, "ceilingTurn", profile.StepCeilingTurn)
	}
	return route, "routed:" + reason
}

// effortStepModulator builds the per-turn policy for a routed run on the given
// model's disabled config and profile thresholds. When the session had no
// explicit thinking config, an empty "enabled" sentinel reverts to the PROVIDER
// DEFAULT (applySamplingParams emits nothing for enabled with BudgetTokens 0 —
// the dual-mode template then thinks again). Shared by the initial route and the
// fallback chain (which rebuilds it for the fallback model's own profile).
func effortStepModulator(profile router.Profile, disabled, origThinking *llm.ThinkingConfig) func(turn int, acts []agent.ToolActivity) *llm.ThinkingConfig {
	revert := origThinking
	if revert == nil {
		revert = &llm.ThinkingConfig{Type: "enabled"}
	}
	return func(turn int, acts []agent.ToolActivity) *llm.ThinkingConfig {
		return effortStepThinking(profile, turn, acts, disabled, revert)
	}
}

// effortStepThinking is the per-step effort policy for a routed run: stay
// non-thinking only while the step is early AND this run's observations are
// light; revert to the session's thinking otherwise (Ares: later steps and
// heavy observations need effort). o_t comes from the executor's ToolActivity
// records — run-scoped by construction (session history is h_t, not o_t), no
// message re-parsing. Batch-aware: calls sharing a Turn form one batch, and the
// LATEST batch counts its LARGEST result while ANY error across the run reverts.
// Thresholds come from the profile so an operator can retune per model.
func effortStepThinking(profile router.Profile, turn int, acts []agent.ToolActivity, disabled, revert *llm.ThinkingConfig) *llm.ThinkingConfig {
	if turn == 0 {
		return disabled
	}
	if turn >= profile.StepCeilingTurn {
		return revert
	}
	var lastTurn, lastBatchMax, totalSize int
	var anyErr bool
	for _, a := range acts {
		totalSize += a.OutputRunes
		if a.IsError {
			anyErr = true
		}
		if a.Turn != lastTurn {
			lastTurn, lastBatchMax = a.Turn, 0
		}
		if a.OutputRunes > lastBatchMax {
			lastBatchMax = a.OutputRunes
		}
	}
	if anyErr || lastBatchMax > profile.ObservationRunes || totalSize > profile.CumulativeRunes {
		return revert
	}
	return disabled
}

// effortRouted reports whether the config currently carries the router's
// template-toggle "disabled" thinking (vs a session-configured disabled,
// which has no TemplateKwarg).
func effortRouted(cfg *agent.AgentConfig) bool {
	return cfg.Thinking != nil && cfg.Thinking.Type == "disabled" && cfg.Thinking.TemplateKwarg != ""
}

// restoreEffort puts the session's original thinking configuration back.
func restoreEffort(cfg *agent.AgentConfig, route *effortRoute) {
	cfg.Thinking = route.origThinking
	cfg.ThinkingModulator = route.origModulator
}

// escalatableEffortFailure reports whether a routed run failed in a shape the
// thinking-restored retry can plausibly fix: the classic stall (empty timeout
// result, errModelStalled), a silently idle stream (the realistic wedge shape
// for a NON-thinking run — with no reasoning deltas flowing, a dead stream
// hits the idle watchdog instead of the full-run timeout), or a degenerate
// empty success (end_turn with no text and no tool calls).
func escalatableEffortFailure(runErr error, res *agent.AgentResult) bool {
	if errors.Is(runErr, errModelStalled) || errors.Is(runErr, agent.ErrStreamIdle) {
		return true
	}
	return runErr == nil && res != nil && res.StopReason == "end_turn" &&
		strings.TrimSpace(res.AllText) == "" && res.TotalToolCalls == 0
}

// resultRanTools reports whether the run executed any tool calls — re-running
// such a turn would double tool side effects (message.send, exec).
func resultRanTools(res *agent.AgentResult) bool {
	return res != nil && res.TotalToolCalls > 0
}

// protectedEffortReason reports keep-reasons that even force mode must not
// override (see applyEffortRouter).
func protectedEffortReason(reason string) bool {
	switch reason {
	case "automation", "attachments", "empty":
		return true
	}
	return false
}

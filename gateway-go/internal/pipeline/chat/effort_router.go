// effort_router.go — Adaptive reasoning-effort router (v1, static heuristic).
//
// Dual-mode models (DeepSeek V4 family) ship with an always-thinking serving
// default, which wastes tokens and latency on conversational turns: measured
// on DSV4-Flash (2x DGX Spark, vLLM), disabling thinking per-request cuts
// completion tokens ~58% and wall-clock ~42-62% on simple queries, while the
// KV prefix cache survives per-request toggles (the template flag only
// changes the generation tail; verified live, TTFT 0.31s across switches).
//
// The router disables thinking ONLY for obviously-simple short conversational
// user messages. The error asymmetry dictates the bias: a false-easy (hard
// task routed to non-thinking) costs answer quality, a false-hard merely
// wastes tokens — so automation runs, attachment-bearing turns, and anything
// with an analysis/code/planning signal keep thinking. A routed run that
// fails in a router-attributable way gets one thinking-restored retry before
// the model fallback chain (run_fallback.go); the prefix is KV-cached, so the
// escalation re-enters cheaply.
//
// Mechanically the router only swaps cfg.Thinking to {Type: "disabled",
// TemplateKwarg: <from modelcaps>}: the provider layer translates that to
// vLLM chat_template_kwargs (openai.go applySamplingParams), Anthropic-wire
// providers ignore "disabled" as before, and models without a template
// toggle keep the existing reasoning_effort floor. Capability knowledge
// lives in modelcaps.ThinkingToggleKwarg (provider-aware), not here.
//
// v1 is opt-in via DENEB_ADAPTIVE_EFFORT=1.
package chat

import (
	"errors"
	"log/slog"
	"os"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
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

// adaptiveEffortEnabled reports whether the effort router is opted in.
func adaptiveEffortEnabled() bool {
	return envFlagEnabled("DENEB_ADAPTIVE_EFFORT")
}

// effortRoute records what the router replaced so escalation and the model
// fallback chain can restore the session's original thinking configuration
// with plain field assignment (no shared state to strip).
type effortRoute struct {
	origThinking  *llm.ThinkingConfig
	origModulator func(turn int) *llm.ThinkingConfig
}

// effortHardSignals are substrings that mark a message as needing the
// thinking mode regardless of length. Korean-first, with common English
// equivalents. Deliberately broad — false-hard only wastes tokens.
var effortHardSignals = []string{
	// analysis / planning / building
	"분석", "계획", "설계", "구현", "정리", "요약", "작성", "검토", "비교",
	"전략", "보고서", "리뷰", "평가", "조사", "검증", "최적화",
	// code / debugging
	"코드", "디버그", "오류", "버그", "에러", "빌드", "테스트", "리팩",
	// math / logic
	"계산", "증명", "수식",
	// open-ended interrogatives
	"왜", "어떻게", "어떡",
	// english equivalents
	"analyz", "plan", "design", "implement", "debug", "review", "compare",
	"why", "how", "code", "fix", "refactor", "summar", "report",
}

// decideThinkingOff returns true when the run's user input is obviously
// simple enough to skip the thinking phase, plus a short reason for logs.
func decideThinkingOff(params RunParams) (bool, string) {
	// Automation runs always keep thinking: their Message is a job
	// instruction, not conversational chatter, and NO_REPLY/delivery
	// judgments must not run in degraded mode. Two markers cover the
	// automation surface: EphemeralUser (heartbeat, boot, skill review,
	// event ingest, mail QA) and the "cron:" session prefix (cron agentTurn
	// persists its transcript, so it is NOT ephemeral — cron_agent_adapter
	// sets only AutoDeliveredOutput). AutoDeliveredOutput itself is
	// deliberately NOT checked: it is delivery semantics and is set on the
	// interactive native client path (miniapp.chat.send) too.
	if params.EphemeralUser || strings.HasPrefix(params.SessionKey, "cron:") {
		return false, "automation"
	}
	// Attachment-bearing turns keep thinking: a short caption routinely
	// accompanies a complex image/document task ("이거 봐줘" + contract photo).
	if len(params.Attachments) > 0 {
		return false, "attachments"
	}
	msg := strings.TrimSpace(params.Message)
	if msg == "" {
		return false, "empty"
	}
	if len([]rune(msg)) > 60 {
		return false, "long"
	}
	if strings.Contains(msg, "```") || strings.Count(msg, "\n") >= 2 {
		return false, "structured"
	}
	lower := strings.ToLower(msg)
	for _, sig := range effortHardSignals {
		if strings.Contains(lower, sig) {
			return false, "hard-signal:" + sig
		}
	}
	return true, "short-conversational"
}

// applyEffortRouter swaps the run's thinking config to "disabled" when the
// resolved model supports a template thinking toggle and the user input is
// obviously simple. Must run AFTER model resolution (buildAgentConfig leaves
// cfg.Model empty). Returns the route to restore on escalation/fallback, or
// nil when the run was not routed.
func applyEffortRouter(cfg *agent.AgentConfig, params RunParams, toggleKwarg string, logger *slog.Logger) *effortRoute {
	if !adaptiveEffortEnabled() || toggleKwarg == "" {
		return nil
	}
	off, reason := decideThinkingOff(params)
	if !off {
		return nil
	}
	route := &effortRoute{origThinking: cfg.Thinking, origModulator: cfg.ThinkingModulator}
	cfg.Thinking = &llm.ThinkingConfig{Type: "disabled", TemplateKwarg: toggleKwarg}
	cfg.ThinkingModulator = nil
	if logger != nil {
		logger.Info("effort router: thinking off for this run",
			"reason", reason, "model", cfg.Model)
	}
	return route
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

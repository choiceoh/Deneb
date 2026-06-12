// effort_router.go — Adaptive reasoning-effort router (v1, static heuristic).
//
// Dual-mode models (DeepSeek V4 family: non-thinking <-> thinking) ship with
// an always-thinking serving default, which wastes tokens and latency on
// conversational turns: measured on DSV4-Flash (2x DGX Spark), disabling
// thinking per-request cuts completion tokens ~58% and wall-clock ~42% on
// simple queries, while the KV prefix cache is preserved across toggles (the
// template flag only changes the generation tail, verified live).
//
// The router disables thinking ONLY for obviously-simple short conversational
// messages. The error asymmetry dictates the bias: a false-easy (hard task
// routed to non-thinking) costs answer quality, a false-hard merely wastes
// tokens — so anything with an analysis/code/planning signal keeps thinking.
// A non-thinking run that stalls escalates back to thinking once before the
// model fallback chain fires (run_fallback.go); the prefix is KV-cached, so
// the escalation re-enters at full speed.
//
// v1 is opt-in via DENEB_ADAPTIVE_EFFORT=1 and gated to models whose chat
// template accepts a per-request thinking toggle (vLLM chat_template_kwargs).
package chat

import (
	"log/slog"
	"os"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
)

// applyEffortRouter disables the thinking phase for obviously-simple
// conversational messages on dual-mode models. Must be called AFTER model
// resolution (buildAgentConfig leaves cfg.Model empty). Dropping the
// session's reasoning_effort alongside is intentional — with the template's
// thinking disabled it would only conflict.
func applyEffortRouter(cfg *agent.AgentConfig, userMessage string, logger *slog.Logger) {
	if cfg == nil || !adaptiveEffortEnabled() || !supportsThinkingToggle(cfg.Model) {
		return
	}
	off, reason := decideThinkingOff(userMessage)
	if !off {
		if logger != nil {
			logger.Debug("effort router: thinking kept", "reason", reason, "model", cfg.Model)
		}
		return
	}
	cfg.ExtraBody = effortOverrideBody()
	cfg.Thinking = nil
	cfg.ThinkingModulator = nil
	if logger != nil {
		logger.Info("effort router: thinking off for this run",
			"reason", reason, "model", cfg.Model)
	}
}

// adaptiveEffortEnabled reports whether the effort router is opted in
// (same env-flag pattern as reasoningSandwichEnabled).
func adaptiveEffortEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("DENEB_ADAPTIVE_EFFORT"))) {
	case "1", "true", "on", "yes":
		return true
	default:
		return false
	}
}

// supportsThinkingToggle reports whether the model's chat template accepts a
// per-request {"thinking": bool} toggle via vLLM chat_template_kwargs.
// Currently the DeepSeek V4 family (deepseek-v4-flash etc.).
func supportsThinkingToggle(model string) bool {
	m := strings.ToLower(model)
	return strings.Contains(m, "deepseek-v4") || strings.Contains(m, "deepseek_v4")
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

// decideThinkingOff returns true when the user message is obviously simple
// enough to skip the thinking phase, plus a short reason for logs. Empty
// messages (cron/automation runs with injected context) always keep thinking.
func decideThinkingOff(message string) (bool, string) {
	msg := strings.TrimSpace(message)
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

// effortOverrideBody builds the per-request body override that disables the
// thinking phase on vLLM-served dual-mode models.
func effortOverrideBody() map[string]any {
	return map[string]any{"chat_template_kwargs": map[string]any{"thinking": false}}
}

// effortRouterApplied reports whether the run's config carries the router's
// thinking-off override (the chat pipeline is the sole ExtraBody writer).
func effortRouterApplied(cfg *agent.AgentConfig) bool {
	if cfg == nil || cfg.ExtraBody == nil {
		return false
	}
	_, ok := cfg.ExtraBody["chat_template_kwargs"]
	return ok
}

// stripEffortOverride removes the router's override so an escalation retry
// runs with the model's thinking default restored.
func stripEffortOverride(cfg *agent.AgentConfig) {
	if cfg == nil || cfg.ExtraBody == nil {
		return
	}
	delete(cfg.ExtraBody, "chat_template_kwargs")
	if len(cfg.ExtraBody) == 0 {
		cfg.ExtraBody = nil
	}
}

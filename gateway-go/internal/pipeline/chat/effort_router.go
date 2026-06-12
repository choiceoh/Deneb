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

// Per-step policy thresholds (Ares, arXiv:2603.07915: effort needs GROW with
// step index and with the perceptual load of accumulated observations). A
// routed run decides EVERY turn from (turn, h_t, o_t) — request-level, so
// flipping per turn is KV-prefix-safe:
//   - turn 0: non-thinking (the routed simple reply, typically terminal)
//   - turns 1..ceiling-1: non-thinking only while the latest observation is
//     small and clean; a big or error-bearing tool result reverts to thinking
//   - turn >= effortStepCeilingTurn: always the session's thinking
const (
	effortStepCeilingTurn   = 3    // hard ceiling: later steps always think
	effortObservationRunes  = 2000 // a tool_result bigger than this needs thinking to digest
	effortCumulativeRunes   = 8000 // total tool_result volume that marks the run as heavy
	effortHeavyHistoryRunes = 1500 // an assistant message this long marks recent context heavy
)

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

// effortPureAcks are messages that stay routable even when the recent
// context is heavy: pure pleasantries/acks do not steer the ongoing work.
// Matched by EXACT equality against the punctuation-stripped message —
// substring matching misreads Korean sentence endings as acks ("안되네"
// contains "네", "안 좋아" contains "좋아": both are steering/negative
// reports, the costliest false-easy shape).
var effortPureAcks = map[string]struct{}{
	"고마워": {}, "고마워요": {}, "감사": {}, "감사합니다": {}, "감사해요": {},
	"잘자": {}, "잘자요": {}, "좋아": {}, "좋아요": {}, "좋네": {}, "좋네요": {},
	"굿": {}, "오케이": {}, "ㅇㅋ": {}, "ㅋㅋ": {}, "응": {}, "응응": {}, "웅": {},
	"넵": {}, "네": {}, "네네": {}, "넹": {}, "예": {}, "알겠어": {}, "알겠습니다": {},
	"ok": {}, "okay": {}, "thanks": {}, "thx": {}, "good": {},
}

func isPureAck(msg string) bool {
	if len([]rune(msg)) > 12 {
		return false
	}
	// Strip everything but letters/digits so "고마워!!" or "네~" match.
	var b strings.Builder
	for _, r := range strings.ToLower(msg) {
		if ('가' <= r && r <= '힣') || ('a' <= r && r <= 'z') || ('0' <= r && r <= '9') ||
			('ㄱ' <= r && r <= 'ㅣ') {
			b.WriteRune(r)
		}
	}
	_, ok := effortPureAcks[b.String()]
	return ok
}

// recentContextHeavy reports whether the tail of the assembled history (h_t,
// excluding the current user message) shows deep work in progress: tool
// activity or a long assistant output. A short follow-up steering such a
// thread ("그래서 어떻게 됐어", "계속해줘") needs the thinking the thread
// already required — query-only routing would misjudge it (Ares decision
// point #3: the router must see the agent's context, not the query alone).
func recentContextHeavy(messages []llm.Message) bool {
	if len(messages) < 2 {
		return false
	}
	tail := messages[:len(messages)-1] // exclude the current user message
	start := len(tail) - 6
	if start < 0 {
		start = 0
	}
	for _, m := range tail[start:] {
		for _, b := range llm.ContentToBlocks(m.Content) {
			switch b.Type {
			case "tool_use", "tool_result":
				return true
			case "text":
				if m.Role == "assistant" && len([]rune(b.Text)) > effortHeavyHistoryRunes {
					return true
				}
			}
		}
	}
	return false
}

// decideThinkingOff returns true when the run's user input is obviously
// simple enough to skip the thinking phase, plus a short reason for logs.
// messages is the assembled history INCLUDING the current user message —
// the same context the agent itself sees (h_t).
func decideThinkingOff(params RunParams, messages []llm.Message) (bool, string) {
	// Automation runs always keep thinking: their Message is a job
	// instruction, not conversational chatter, and NO_REPLY/delivery
	// judgments must not run in degraded mode. Two markers cover the
	// automation surface: EphemeralUser (heartbeat, boot, skill review,
	// event ingest, mail QA) and the "cron:" session prefix (cron agentTurn
	// persists its transcript, so it is NOT ephemeral — cron_agent_adapter
	// sets only AutoDeliveredOutput). AutoDeliveredOutput itself is
	// deliberately NOT checked: it is delivery semantics and is set on the
	// interactive native client path (miniapp.chat.send) too.
	if params.EphemeralUser || strings.HasPrefix(params.SessionKey, "cron:") ||
		strings.HasPrefix(params.SessionKey, "acp:") {
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
	// h_t check (Ares #3): a short message steering a heavy thread (recent
	// tool activity / long assistant output) inherits the thread's effort —
	// unless it is a pure pleasantry/ack that steers nothing.
	if recentContextHeavy(messages) && !isPureAck(msg) {
		return false, "context-heavy"
	}
	return true, "short-conversational"
}

// applyEffortRouter swaps the run's thinking config to "disabled" when the
// resolved model supports a template thinking toggle and the user input is
// obviously simple. Must run AFTER model resolution (buildAgentConfig leaves
// cfg.Model empty). Returns the route to restore on escalation/fallback (nil
// when not routed) plus the decision string ("routed:…"/"kept:…", "" when the
// router gate is closed) for the structured run-complete record.
func applyEffortRouter(cfg *agent.AgentConfig, params RunParams, messages []llm.Message, toggleKwarg string, logger *slog.Logger) (*effortRoute, string) {
	mode := effortMode()
	if mode == effortModeOff || toggleKwarg == "" {
		return nil, ""
	}
	off, reason := decideThinkingOff(params, messages)
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
	disabled := &llm.ThinkingConfig{Type: "disabled", TemplateKwarg: toggleKwarg}
	cfg.Thinking = disabled
	cfg.ThinkingModulator = effortStepModulator(disabled, route.origThinking)
	if logger != nil {
		logger.Info("effort router: thinking off for this run",
			"reason", reason, "model", cfg.Model, "ceilingTurn", effortStepCeilingTurn)
	}
	return route, "routed:" + reason
}

// effortStepModulator builds the per-turn policy for a routed run on the
// given model's disabled config. When the session had no explicit thinking
// config, an empty "enabled" sentinel reverts to the PROVIDER DEFAULT
// (applySamplingParams emits nothing for enabled with BudgetTokens 0 — the
// dual-mode template then thinks again). Shared by the initial route and the
// fallback chain (which rebuilds it for the fallback model's own kwarg).
func effortStepModulator(disabled, origThinking *llm.ThinkingConfig) func(turn int, acts []agent.ToolActivity) *llm.ThinkingConfig {
	revert := origThinking
	if revert == nil {
		revert = &llm.ThinkingConfig{Type: "enabled"}
	}
	return func(turn int, acts []agent.ToolActivity) *llm.ThinkingConfig {
		return effortStepThinking(turn, acts, disabled, revert)
	}
}

// effortStepThinking is the per-step effort policy for a routed run: stay
// non-thinking only while the step is early AND this run's observations are
// light; revert to the session's thinking otherwise (Ares: later steps and
// heavy observations need effort). o_t comes from the executor's ToolActivity
// records — run-scoped by construction (session history is h_t, not o_t), no
// message re-parsing. Batch-aware: calls sharing a Turn form one batch, and
// the LATEST batch counts its LARGEST result while ANY error across the run
// reverts — a [6K-rune error, tiny ok] batch cannot hide behind call order.
func effortStepThinking(turn int, acts []agent.ToolActivity, disabled, revert *llm.ThinkingConfig) *llm.ThinkingConfig {
	if turn == 0 {
		return disabled
	}
	if turn >= effortStepCeilingTurn {
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
	if anyErr || lastBatchMax > effortObservationRunes || totalSize > effortCumulativeRunes {
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

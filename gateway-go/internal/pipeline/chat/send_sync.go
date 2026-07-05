package chat

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/pkg/dentime"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/hanja"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/shortid"
)

// SyncResult holds the outcome of a synchronous agent run.
type SyncResult struct {
	Text    string
	AllText string // accumulated text from all turns; used by cron delivery as a fallback when the final turn is NO_REPLY
	// DeliverableText is AllText minus the brief progress narration the model
	// emits alongside tool calls. Preferred by proactive/cron delivery so a
	// multi-turn report ships its answer turns without the "이제 위키 검색부터
	// 할게요" working narration. See agent.AgentResult.DeliverableText.
	DeliverableText string
	Model           string
	FellBack        bool // true when the model fallback chain fired (Model is the model that actually answered)
	InputTokens     int
	OutputTokens    int
	StopReason      string // "end_turn", "max_tokens", "tool_use", etc.
}

// BestText returns the answer to surface to the user. It prefers DeliverableText
// — the accumulation of every substantial answer turn with the interim
// "이제 ~할게요" tool-call narration removed — which fixes two failure modes at
// once: a short wrap-up final turn (the agent writes the body mid-run, then
// closes with "위키에 기록했습니다") no longer makes the answer vanish, and the
// working narration the model emits before tool calls never reaches the surface.
//
// Mirrors cronChatAdapter so every proactive surface agrees:
//   - DeliverableText present → use it (the common multi-turn case).
//   - else Text (the final turn) → use it.
//   - else AllText (last resort: a run that produced only narration before
//     aborting).
//
// NO_REPLY is stripped so the marker never leaks to the client.
func (r *SyncResult) BestText() string {
	if d := strings.TrimSpace(StripSilentToken(r.DeliverableText)); d != "" {
		return d
	}
	// Text needs the silent strip too: a final turn of exactly NO_REPLY with an
	// empty DeliverableText would otherwise return the literal marker here,
	// contradicting the "never leaks to the client" contract above.
	if t := strings.TrimSpace(StripSilentToken(r.Text)); t != "" {
		return t
	}
	return strings.TrimSpace(StripSilentToken(r.AllText))
}

func (r *SyncResult) fillEmptyStopFallback() bool {
	if r == nil || r.BestText() != "" {
		return false
	}
	msg := fallbackForStopReason(r.StopReason)
	if msg == "" {
		return false
	}
	r.Text = msg
	r.AllText = msg
	r.DeliverableText = msg
	return true
}

// SyncOptions holds optional parameters for synchronous agent runs.
// Used by the OpenAI-compatible HTTP endpoints to pass through sampling
// parameters and conversation context.
type SyncOptions struct {
	Temperature      *float64
	TopP             *float64
	MaxTokens        *int
	FrequencyPenalty *float64
	PresencePenalty  *float64
	Stop             []string
	ResponseFormat   *llm.ResponseFormat
	ToolChoice       any // "auto", "none", "required", or structured object
	// Thinking overrides the session's thinking level for this run — a
	// resolveThinkingConfig level or "off"/"none" to disable the thinking
	// phase (cron jobs use this). Empty = session/provider default.
	Thinking string

	// Messages provides a full conversation context (system, user, assistant,
	// tool messages). When set, this replaces the normal transcript-based
	// context assembly, and the `message` parameter is ignored.
	Messages []llm.Message

	// SystemPrompt provides a system prompt extracted from the messages array.
	// Used when Messages is set and system messages were present.
	SystemPrompt string

	// ToolPreset restricts available tools for this run (e.g. "boot", "conversation").
	// Empty means no restriction.
	ToolPreset string

	// MaxHistoryTokens overrides the transcript history token budget.
	// When set, assembleContext trims older messages to fit within this budget.
	MaxHistoryTokens int

	// Delivery carries channel routing for proactive tool sends (e.g. message.send).
	// Required in cron / scheduled contexts: without it the message tool fails
	// with "no active delivery target" and the agent falls back to text-only
	// replies that the cron delivery layer may not route correctly.
	Delivery *DeliveryContext

	// EphemeralUser suppresses persistence of the inbound user-role message —
	// see RunParams.EphemeralUser. Set by autonomous triggers (heartbeat) so
	// recurring self-triggers do not crowd out the recent-history window.
	EphemeralUser bool

	// SkipRecall skips the long-term-memory recall preflight for this turn —
	// see RunParams.SkipRecall. Set from the native client's "memory off /
	// focused chat" toggle so general questions skip work-context injection.
	SkipRecall bool

	// FeedContext is the 업무 day's-feed digest to inject as wire-only context —
	// see RunParams.FeedContext. Set by the native bridge for 업무 turns only.
	FeedContext string

	// EphemeralAssistant suppresses persistence of assistant/tool_result
	// messages produced during the run — see RunParams.EphemeralAssistant.
	// Heartbeat sets this true so autonomous ticks do not crowd out the
	// user's short-term conversation context; heartbeat state belongs in
	// HEARTBEAT.md instead.
	EphemeralAssistant bool

	// AutoDeliveredOutput marks a run whose final reply text is delivered by
	// the caller's run-completion path (e.g. the cron delivery layer) rather
	// than by the agent's in-loop `message` tool. Propagated to RunParams;
	// see RunParams.AutoDeliveredOutput.
	AutoDeliveredOutput bool

	// BeforeToolCall, when set, gates each tool execution (block + reason).
	// Propagated to RunParams.BeforeToolCall; the goal loop uses it for its
	// idempotency guard. nil = no gate.
	BeforeToolCall func(name, toolCallID string, input []byte) (block bool, blockReason string)

	// OnToolResult, when set, observes each tool result. Propagated to
	// RunParams.OnToolResult; the goal loop uses it to record committed
	// destructive actions into the ledger. nil = no observer.
	OnToolResult func(name, toolUseID, result string, isErr bool)

	// OnToolEvent, when set on a streaming run (SendSyncStream only), receives
	// tool lifecycle transitions (started/completed, with detail hint and
	// error flag) so the transport can surface live tool progress — the native
	// client renders these as the waiting indicator's tool label. Nil-safe.
	OnToolEvent func(ev ToolStreamEvent)

	// OnThinking, when set on a streaming run (SendSyncStream only), fires
	// while the model emits reasoning deltas (throttled by the broadcaster) so
	// the transport can show a "thinking" hint before the first visible token.
	// preview carries a chip-sized tail of the recent reasoning text ("" when
	// nothing readable accumulated yet).
	OnThinking func(preview string)

	// GateUntrustedTools enables the untrusted-origin tool gate (blocking
	// irreversible tools when promptware enters the turn). Set by the
	// interactive native-client transports. Propagated to
	// RunParams.GateUntrustedTools.
	GateUntrustedTools bool
}

// prepareSyncRun builds RunParams and runDeps from the common sync arguments.
// Both SendSync and SendSyncStream share this setup.
func (h *Handler) prepareSyncRun(sessionKey, message, model, runIDPrefix string, opts *SyncOptions) (RunParams, runDeps, error) {
	if h.sessions == nil {
		return RunParams{}, runDeps{}, fmt.Errorf("chat handler not initialized")
	}

	sess := h.sessions.Get(sessionKey)
	if sess == nil {
		sess = h.sessions.Create(sessionKey, "direct")
	}

	params := RunParams{
		SessionKey:  sessionKey,
		Message:     sanitizeInput(message),
		Model:       model,
		ClientRunID: shortid.New(runIDPrefix),
	}

	if opts != nil {
		params.Temperature = opts.Temperature
		params.TopP = opts.TopP
		params.MaxTokens = opts.MaxTokens
		params.FrequencyPenalty = opts.FrequencyPenalty
		params.PresencePenalty = opts.PresencePenalty
		params.Stop = opts.Stop
		params.ResponseFormat = opts.ResponseFormat
		params.ToolChoice = opts.ToolChoice
		params.Thinking = opts.Thinking
		if len(opts.Messages) > 0 {
			params.PrebuiltMessages = opts.Messages
		}
		if opts.SystemPrompt != "" {
			params.System = opts.SystemPrompt
		}
		if opts.ToolPreset != "" {
			sess.ToolPreset = opts.ToolPreset
		}
		if opts.Delivery != nil {
			params.Delivery = opts.Delivery
		}
		params.EphemeralUser = opts.EphemeralUser
		params.SkipRecall = opts.SkipRecall
		params.FeedContext = opts.FeedContext
		params.EphemeralAssistant = opts.EphemeralAssistant
		params.AutoDeliveredOutput = opts.AutoDeliveredOutput
		params.BeforeToolCall = opts.BeforeToolCall
		params.OnToolResult = opts.OnToolResult
		params.GateUntrustedTools = opts.GateUntrustedTools
	}

	deps := h.buildRunDeps()
	if opts != nil && opts.MaxHistoryTokens > 0 {
		// MaxHistoryTokens is the HISTORY budget, but MemoryTokenBudget is the
		// TOTAL (system + history) budget — run_exec derives
		// contextBudget = MemoryTokenBudget - SystemPromptBudget. Setting total =
		// history collapsed contextBudget to (history - system): boot's 30K-30K=0,
		// and skill-review's 1-30K underflowed (uint64) to a giant budget. Both
		// drove compaction to process the full uncapped transcript and stall. Add
		// the system budget back so the requested history budget survives intact.
		deps.contextCfg.MemoryTokenBudget = uint64(opts.MaxHistoryTokens) + deps.contextCfg.SystemPromptBudget
	}
	if h.recordActivity != nil && !params.EphemeralUser {
		h.recordActivity(sessionKey)
	}

	return params, deps, nil
}

// buildSyncResult converts a chatRunResult into a SyncResult, resolving the
// model name through the fallback chain (explicit → default → registry).
func (h *Handler) buildSyncResult(model string, result *chatRunResult) (*SyncResult, error) {
	resolvedModel := model
	if resolvedModel == "" {
		resolvedModel = h.DefaultModel()
	}
	if resolvedModel == "" && h.registry != nil {
		resolvedModel = h.registry.FullModelID(modelrole.RoleMain)
	}

	if result == nil {
		return nil, fmt.Errorf("agent run returned nil result")
	}

	// Prefer the model that actually answered (set when the fallback chain fired).
	if result.ActualModel != "" {
		resolvedModel = result.ActualModel
	}

	// Strip any chain-of-thought delimiters that leaked into the answer (see
	// reasoning_leak.go). The block regex matches here because the full assembled
	// text is available. TrimSpace cleans the gap a removed leading block leaves.
	// Then read Sino-Korean Hanja as Hangul (報告書 → 보고서) so a Chinese-lineage
	// model's output surfaces in Korean — this is the chokepoint for every
	// BestText() consumer (the stream done frame, proactive bridge, auto-title).
	res := &SyncResult{
		Text:            hanja.Transliterate(strings.TrimSpace(stripReasoningLeak(result.Text))),
		AllText:         hanja.Transliterate(strings.TrimSpace(stripReasoningLeak(result.AllText))),
		DeliverableText: hanja.Transliterate(strings.TrimSpace(stripReasoningLeak(result.DeliverableText))),
		Model:           resolvedModel,
		FellBack:        result.FellBack,
		InputTokens:     result.Usage.InputTokens,
		OutputTokens:    result.Usage.OutputTokens,
		StopReason:      result.StopReason,
	}
	res.fillEmptyStopFallback()
	return res, nil
}

// SendSync runs the agent loop synchronously, blocking until the response is
// complete or the context is canceled. Used by the OpenAI-compatible HTTP
// endpoints and the native client's miniapp.chat.send.
func (h *Handler) SendSync(ctx context.Context, sessionKey, message, model string, opts *SyncOptions) (*SyncResult, error) {
	if res, handled := h.trySlashSync(sessionKey, message, opts); handled {
		return res, nil
	}
	if res, handled := h.trySteerIntoActiveRun(sessionKey, message, opts); handled {
		return res, nil
	}
	// Link fetches start now and join inside executeAgentRun, AFTER the
	// parallel prep phase — a pasted slow link no longer blocks the turn
	// start for up to 30s (see link_enrichment.go).
	enrich := h.startLinkEnrichment(ctx, message, opts)
	params, deps, err := h.prepareSyncRun(sessionKey, message, model, "sync", opts)
	if err != nil {
		return nil, err
	}
	params.PendingEnrichment = enrich

	// Agent detail logging: without a RunLogger every SendSync surface
	// (miniapp.chat.send, cron single-run, heartbeat, boot, mail-qa, BTW) is
	// invisible in ~/.deneb/agent-logs and to the modeltuner's AggregateByModel.
	runLog := agentlog.NewRunLogger(deps.agentLog, params.SessionKey, params.ClientRunID)
	result, err := executeAgentRun(ctx, params, deps, nil, nil, h.logger, runLog)
	if err != nil {
		return nil, err
	}
	finishTurnSideEffects(deps, params, result.AgentResult, h.logger)
	res, err := h.buildSyncResult(model, result)
	if err == nil {
		h.autoTitleSessionAsync(sessionKey, message, res)
	}
	return res, err
}

// steerMaxRunes bounds which mid-run follow-ups fold into the active turn as a
// steer note. Longer messages read as a genuinely new request and keep the
// current behavior (their own run).
const steerMaxRunes = 400

// trySteerIntoActiveRun folds a short follow-up that arrives while this
// session's run is still executing into that run's steer queue — the same
// mechanism as the explicit /steer command, made automatic (OpenClaw's
// queue-steering default: a mid-run correction like "아 내일 말고 모레" joins
// the active turn at the next tool-result boundary instead of racing it with
// a second concurrent run on the same session/transcript).
//
// The message is ALSO persisted to the transcript (same store the run uses),
// so history keeps the correction in order: …, user(correction),
// assistant(reply that honored it). The wire injection itself stays a
// per-request copy (steer_inject.go) — prompt-cache Rule A intact.
//
// Conservative gates: interactive chat only (no API Messages, no autonomous
// EphemeralUser/AutoDelivered surfaces), short plain text, and an active run.
// If the run finishes between the check and the hook's drain, the note is
// consumed by this session's next run (the steer hook drains before every LLM
// call) — deferred, never lost.
func (h *Handler) trySteerIntoActiveRun(sessionKey, message string, opts *SyncOptions) (*SyncResult, bool) {
	if opts == nil || len(opts.Messages) > 0 || opts.EphemeralUser || opts.AutoDeliveredOutput {
		return nil, false
	}
	trimmed := strings.TrimSpace(message)
	if trimmed == "" || utf8.RuneCountInString(trimmed) > steerMaxRunes {
		return nil, false
	}
	if !h.abort.HasActiveRun(sessionKey) {
		return nil, false
	}
	if !h.steer.Enqueue(sessionKey, trimmed) {
		return nil, false
	}
	if h.transcript != nil {
		now := dentime.Now()
		userMsg := NewTextChatMessage("user", formatTurnUserMessage(trimmed, now), now.UnixMilli())
		if err := h.transcript.Append(sessionKey, userMsg); err != nil {
			h.logger.Error("auto-steer: persist steered user message failed", "sessionKey", sessionKey, "error", err)
		}
	}
	h.logger.Info("auto-steer: folded mid-run message into the active run", "sessionKey", sessionKey, "runes", utf8.RuneCountInString(trimmed))
	const ack = "지금 진행 중인 답변에 반영할게요."
	return &SyncResult{
		Text:            ack,
		AllText:         ack,
		DeliverableText: ack,
		Model:           "steer",
		StopReason:      "steered",
	}, true
}

// trySlashSync short-circuits slash commands on the synchronous send paths.
// The native client talks to the gateway via miniapp.chat.send (SendSync), so
// without this, slash input fell through to the LLM as plain text and the
// dispatch layer's reply (delivered via ReplyFn, unwired on the native-only
// deployment) was lost. The collector captures every immediate respond() call
// so the slash reply returns in the RPC response the client renders.
// Long-running commands (/update, /rollback, …) reply from their own
// goroutines later; their sync response is an acknowledgement only.
func (h *Handler) trySlashSync(sessionKey, message string, opts *SyncOptions) (*SyncResult, bool) {
	// PrebuiltMessages flows (OpenAI-compatible HTTP with full history) are
	// API traffic, not interactive chat — leave them untouched.
	if opts != nil && len(opts.Messages) > 0 {
		return nil, false
	}
	cmd := ParseSlashCommand(message)
	if cmd == nil || !cmd.Handled {
		return nil, false
	}
	var delivery *DeliveryContext
	if opts != nil {
		delivery = opts.Delivery
	}
	var reply strings.Builder
	h.handleSlashCommand(shortid.New("slash"), sessionKey, delivery, cmd, func(text string) {
		if text == "" {
			return
		}
		if reply.Len() > 0 {
			reply.WriteString("\n\n")
		}
		reply.WriteString(text)
	})
	text := reply.String()
	if text == "" {
		// Async commands ack immediately; their real output arrives later.
		text = fmt.Sprintf("`/%s` 명령을 실행했습니다.", cmd.Command)
	}
	return &SyncResult{
		Text:            text,
		AllText:         text,
		DeliverableText: text,
		Model:           "slash:" + cmd.Command,
		StopReason:      "slash_command",
	}, true
}

// SendSyncStream runs the agent loop, calling onDelta for each text chunk,
// then returning the final result. Used by streaming OpenAI-compatible
// endpoints and the native client's miniapp.chat.stream.
func (h *Handler) SendSyncStream(ctx context.Context, sessionKey, message, model string, opts *SyncOptions, onDelta func(string)) (*SyncResult, error) {
	if res, handled := h.trySlashSync(sessionKey, message, opts); handled {
		if onDelta != nil && res.Text != "" {
			onDelta(res.Text)
		}
		return res, nil
	}
	if res, handled := h.trySteerIntoActiveRun(sessionKey, message, opts); handled {
		if onDelta != nil && res.Text != "" {
			onDelta(res.Text)
		}
		return res, nil
	}
	// Same deferred link enrichment as SendSync — see the comment there.
	enrich := h.startLinkEnrichment(ctx, message, opts)
	params, deps, err := h.prepareSyncRun(sessionKey, message, model, "stream", opts)
	if err != nil {
		return nil, err
	}
	params.PendingEnrichment = enrich

	// Wrap onDelta to scrub leaked reasoning delimiters per chunk so a literal
	// "[thinking]" never reaches the stream. The block regex can't match across
	// delta boundaries, but the standalone-marker strip catches the tokens; the
	// final answer is fully cleaned in buildSyncResult. See reasoning_leak.go.
	streamDelta := onDelta
	if onDelta != nil {
		streamDelta = func(delta string) {
			if cleaned := stripReasoningLeak(delta); cleaned != "" {
				onDelta(cleaned)
			}
		}
	}

	sinks := streamEventSinks{OnDelta: streamDelta}
	if opts != nil {
		sinks.OnTool = opts.OnToolEvent
		sinks.OnThinking = opts.OnThinking
	}
	result, err := executeAgentRunWithDelta(ctx, params, deps, sinks, h.logger)
	if err != nil {
		return nil, err
	}
	finishTurnSideEffects(deps, params, result.AgentResult, h.logger)
	res, err := h.buildSyncResult(model, result)
	if err == nil {
		h.autoTitleSessionAsync(sessionKey, message, res)
	}
	return res, err
}

package chat

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/core/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/leafbind"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/streaming"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire"
	"github.com/choiceoh/deneb/gateway-go/pkg/dentime"
	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
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
	ProviderModel   string
	FellBack        bool // true when the model fallback chain fired (Model is the model that actually answered)
	InputTokens     int
	OutputTokens    int
	Turns           int
	StopReason      string // "end_turn", "max_tokens", "tool_use", etc.
	// Thinking is the accumulated chain-of-thought across all turns (empty when
	// the model produced no reasoning). Surfaced to clients as the expandable
	// reasoning block; never fed back into the model context.
	Thinking string
	// synthesizedFallback marks text created after the agent loop (timeout,
	// abort, or accidental empty completion). The sync caller uses it to persist
	// the user-visible terminal message that per-turn model persistence could not
	// have recorded.
	synthesizedFallback bool
}

// BestTextRaw returns the answer selected for delivery before surface-specific
// substitutions. Deterministic evaluators use this form so process-global,
// time-sensitive presentation caches cannot change score-visible model text.
//
// It prefers DeliverableText
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
// NO_REPLY is stripped so the marker never leaks to consumers.
func (r *SyncResult) BestTextRaw() string {
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

// BestText returns the answer to surface to an interactive user. Market letter
// tokens ("{{market:usd_krw}}") are substituted with their fetched values —
// the proactive relay covers the cron path, this covers the interactive one
// (a letter composed in chat would otherwise surface raw tokens). No-op for
// ordinary replies (fast path on the token prefix).
func (r *SyncResult) BestText() string {
	return toolwire.SubstituteMarketLetterTokens(r.BestTextRaw())
}

func (r *SyncResult) fillEmptyStopFallback(activities []agent.ToolActivity) bool {
	if r == nil || r.BestText() != "" {
		return false
	}
	msg := enrichStopFallback(fallbackForStopReason(r.StopReason), activities)
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
	Temperature *float64
	TopP        *float64
	MaxTokens   *int
	MaxTurns    *int
	// MaxToolCallAttempts is a hard run-local cap on model-emitted tool calls.
	// Nil is unlimited; non-nil zero allows only a tool-free response.
	MaxToolCallAttempts *int
	FrequencyPenalty    *float64
	PresencePenalty     *float64
	Stop                []string
	ResponseFormat      *llm.ResponseFormat
	ToolChoice          rawJSON // "auto", "none", "required", or structured object
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

	// InitialDeferredTools activates selected deferred tools on turn 1 for
	// runtime-owned jobs that know a named tool is mandatory. The effective
	// tool preset still gates both schema exposure and execution.
	InitialDeferredTools []string

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

	// AllowRecall lets this ephemeral turn still run the recall preflight —
	// see RunParams.AllowRecall. Set by autonomous callers that have a real
	// subject to recall against.
	AllowRecall bool

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

	// OnProgress receives deterministic lifecycle phases for a streaming run.
	// Unlike reasoning previews these values are server-authored UI state and do
	// not reveal chain-of-thought.
	OnProgress func(phase string)

	// OnThinking, when set on a streaming run (SendSyncStream only), fires
	// while the model emits reasoning deltas (throttled by the broadcaster) so
	// the transport can show a "thinking" hint before the first visible token.
	// preview carries a chip-sized tail of the recent reasoning text ("" when
	// nothing readable accumulated yet).
	OnThinking func(preview string)

	// OnReasoning fires alongside OnThinking with the full reasoning-so-far so the
	// transport can grow a LIVE expandable reasoning block during streaming. Nil-safe.
	OnReasoning func(full string)

	// SoftDeadline asks the agent to wrap up without new tools once this much
	// end-to-end turn time has elapsed. Zero disables the preference.
	SoftDeadline time.Duration

	// GateUntrustedTools enables the untrusted-origin tool gate (blocking
	// irreversible tools when promptware enters the turn). Set by the
	// interactive native-client transports. Propagated to
	// RunParams.GateUntrustedTools.
	GateUntrustedTools bool

	// TrustedDirectUserInput is a server-owned provenance marker set only by
	// authenticated native interactive chat ingress. It is deliberately
	// separate from GateUntrustedTools: capture and other untrusted-content
	// runs enable that defensive gate but are not direct-user commands.
	TrustedDirectUserInput bool
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
		SessionKey:   sessionKey,
		Message:      sanitizeInput(message),
		Model:        model,
		ClientRunID:  leafbind.NewShortID(runIDPrefix),
		WorkspaceDir: h.workspaceDir,
	}

	if opts != nil {
		params.Temperature = opts.Temperature
		params.TopP = opts.TopP
		params.MaxTokens = opts.MaxTokens
		params.MaxTurns = opts.MaxTurns
		params.MaxToolCallAttempts = opts.MaxToolCallAttempts
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
		params.InitialDeferredTools = append([]string(nil), opts.InitialDeferredTools...)
		if opts.Delivery != nil {
			params.Delivery = opts.Delivery
		}
		params.EphemeralUser = opts.EphemeralUser
		params.AllowRecall = opts.AllowRecall
		params.SkipRecall = opts.SkipRecall
		params.FeedContext = opts.FeedContext
		params.EphemeralAssistant = opts.EphemeralAssistant
		params.AutoDeliveredOutput = opts.AutoDeliveredOutput
		params.BeforeToolCall = opts.BeforeToolCall
		params.OnToolResult = opts.OnToolResult
		params.OnProgress = opts.OnProgress
		params.SoftDeadline = opts.SoftDeadline
		params.GateUntrustedTools = opts.GateUntrustedTools
		params.TrustedDirectUserInput = opts.TrustedDirectUserInput
	}

	deps := h.buildRunDeps()
	// Wire the sub-agent notification channel so a child completion that lands
	// mid-run is consumed by THIS run (DeferredSystemText) instead of stranded.
	// startAsyncRun does the same for the async path; the sync entries did not,
	// so once a sync run registers as active (withSyncRunLifecycle) its parked
	// child notifications would otherwise never be drained. nil-safe: hand-built
	// test handlers leave h.subagent nil.
	if h.subagent != nil {
		deps.subagentNotifyCh = h.subagent.NotifyCh(sessionKey)
	}
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

	reasoning := ""
	if result.AgentResult != nil {
		reasoning = strings.TrimSpace(result.AgentResult.Thinking)
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
		Text:            streaming.Transliterate(strings.TrimSpace(stripReasoningLeak(result.Text))),
		AllText:         streaming.Transliterate(strings.TrimSpace(stripReasoningLeak(result.AllText))),
		DeliverableText: streaming.Transliterate(strings.TrimSpace(stripReasoningLeak(result.DeliverableText))),
		Model:           resolvedModel,
		ProviderModel:   result.ProviderModel,
		FellBack:        result.FellBack,
		InputTokens:     result.Usage.InputTokens,
		OutputTokens:    result.Usage.OutputTokens,
		Turns:           result.Turns,
		StopReason:      result.StopReason,
		Thinking:        reasoning,
	}
	var activities []agent.ToolActivity
	if result.AgentResult != nil {
		activities = result.AgentResult.ToolActivities
	}
	res.synthesizedFallback = res.fillEmptyStopFallback(activities)
	// Accidental empty completion — end_turn after tool activity with zero
	// text and no silent token. fillEmptyStopFallback deliberately leaves
	// end_turn alone (NO_REPLY silence must survive), and that narrow rule
	// let this case reach the client as a blank reply with ok=true
	// (measured from the puppet seat). The silent-token case cannot land
	// here: its raw AllText is non-empty, so isEmptyFinalResult is false.
	if res.BestText() == "" && isEmptyFinalResult(result.AgentResult) {
		msg := fallbackForEmptyFinalReply(emptyFinalResultRanTools(result.AgentResult))
		res.Text, res.AllText, res.DeliverableText = msg, msg, msg
		res.synthesizedFallback = true
	}
	return res, nil
}

func (h *Handler) buildSyncTimeoutFallback(model string) (*SyncResult, error) {
	return h.buildSyncResult(model, &chatRunResult{
		AgentResult: &agent.AgentResult{StopReason: "timeout"},
	})
}

// persistSynthesizedSyncFallback closes the persistence gap between the agent
// loop and buildSyncResult. Per-turn persistence can only store model-emitted
// messages; a timeout/empty fallback is created afterward, so without this
// append a reconnect sees the user row but no assistant terminal row even
// though the live SSE done frame showed one.
func persistSynthesizedSyncFallback(params RunParams, deps runDeps, res *SyncResult, logger *slog.Logger) {
	if res == nil || !res.synthesizedFallback || params.EphemeralAssistant || deps.transcript == nil {
		return
	}
	text := strings.TrimSpace(res.BestText())
	if text == "" {
		return
	}
	assistantMsg := NewTextChatMessage("assistant", text, dentime.Now().UnixMilli())
	if err := deps.transcript.Append(params.SessionKey, assistantMsg); err != nil {
		logger.Error("failed to persist synthesized sync fallback", "session", params.SessionKey, "error", err)
		return
	}
	if deps.callbacks.emitTranscriptFn != nil {
		deps.callbacks.emitTranscriptFn(params.SessionKey, mustRawJSON(assistantMsg), "")
	}
}

// SendSync runs the agent loop synchronously, blocking until the response is
// complete or the context is canceled. Used by the OpenAI-compatible HTTP
// endpoints and the native client's miniapp.chat.send.
func (h *Handler) SendSync(ctx context.Context, sessionKey, message, model string, opts *SyncOptions) (*SyncResult, error) {
	if h.abort == nil || !h.abort.AcquireAdmission() {
		return nil, ErrRuntimeDraining
	}
	defer h.abort.ReleaseAdmission()
	if res, handled := h.trySlashSync(sessionKey, message, opts); handled {
		return res, nil
	}
	ticket := h.reserveSyncAdmission(sessionKey)
	defer ticket.release()
	if err := ticket.wait(ctx); err != nil {
		return nil, err
	}
	// Serialize the steer-vs-own-turn decision as part of the same ingress FIFO
	// as full synchronous runs. If an earlier message must wait for its own turn,
	// a later short message must not jump ahead by steering into the active run.
	if res, handled := h.trySteerIntoActiveRun(sessionKey, message, opts); handled {
		return res, nil
	}
	if err := h.admitSyncWhenSessionIdle(ctx, sessionKey); err != nil {
		return nil, err
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
	return h.withAdmittedSyncRunLifecycleReleasingTicket(ctx, sessionKey, params.ClientRunID, ticket,
		isAutomationRun(params),
		func(runCtx context.Context) (*SyncResult, error) {
			result, err := executeAgentRun(runCtx, params, deps, nil, nil, h.logger, runLog)
			if err != nil {
				if errors.Is(context.Cause(runCtx), context.DeadlineExceeded) {
					res, buildErr := h.buildSyncTimeoutFallback(model)
					if buildErr == nil {
						persistSynthesizedSyncFallback(params, deps, res, h.logger)
					}
					return res, buildErr
				}
				return nil, err
			}
			normalizeRunCardReplies(result.AgentResult, params, deps, h.logger)
			finishTurnSideEffects(deps, params, result.AgentResult, h.logger)
			res, err := h.buildSyncResult(model, result)
			if err == nil {
				persistSynthesizedSyncFallback(params, deps, res, h.logger)
				h.autoTitleSessionAsync(sessionKey, message, res)
			}
			return res, err
		})
}

type syncAdmissionTicket struct {
	handler     *Handler
	session     string
	previous    <-chan struct{}
	complete    chan struct{}
	releaseOnce sync.Once
}

func (h *Handler) reserveSyncAdmission(sessionKey string) *syncAdmissionTicket {
	h.syncAdmissionMu.Lock()
	defer h.syncAdmissionMu.Unlock()
	if h.syncAdmissionTails == nil {
		h.syncAdmissionTails = make(map[string]chan struct{})
	}
	previous := h.syncAdmissionTails[sessionKey]
	if previous == nil {
		ready := make(chan struct{})
		close(ready)
		previous = ready
	}
	complete := make(chan struct{})
	h.syncAdmissionTails[sessionKey] = complete
	return &syncAdmissionTicket{handler: h, session: sessionKey, previous: previous, complete: complete}
}

func (t *syncAdmissionTicket) wait(ctx context.Context) error {
	if t == nil {
		return nil
	}
	select {
	case <-t.previous:
		return nil
	case <-ctx.Done():
		if cause := context.Cause(ctx); cause != nil {
			return cause
		}
		return ctx.Err()
	}
}

func (t *syncAdmissionTicket) release() {
	if t == nil || t.handler == nil {
		return
	}
	t.releaseOnce.Do(func() {
		finish := func() {
			close(t.complete)
			t.handler.syncAdmissionMu.Lock()
			if t.handler.syncAdmissionTails[t.session] == t.complete {
				delete(t.handler.syncAdmissionTails, t.session)
			}
			t.handler.syncAdmissionMu.Unlock()
		}
		select {
		case <-t.previous:
			finish()
		default:
			// A canceled middle request must not open a shortcut around its
			// predecessor. Bridge the chain after returning cancellation to the
			// caller, then release the next ticket only when the predecessor ends.
			logger := t.handler.logger
			if logger == nil {
				logger = slog.Default()
			}
			safego.GoWithSlog(logger, "sync-admission-release", func() {
				<-t.previous
				finish()
			})
		}
	})
}

// withSyncRunLifecycle makes a synchronous run a first-class member of the
// session lifecycle, exactly as startAsyncRun does for the async path. Without
// it, a native run (miniapp.chat.send/stream is the native client's ONLY entry,
// all synchronous) was invisible to the abort tracker, which broke three things:
//   - auto-steer never fired (trySteerIntoActiveRun's HasActiveRun check never
//     saw a sync run, so a mid-turn follow-up raced a second concurrent run on
//     the same session/transcript instead of folding);
//   - the merge window and /kill could not act on a native turn;
//   - a sub-agent that finished mid-run saw the parent as idle and spawned a
//     messy concurrent triggerRun to deliver its result.
//
// Registration alone would be strictly worse — a registered-but-undrained run
// strands child notifications — so this pairs it with the notify-channel wiring
// (prepareSyncRun) and, on completion, ReclaimOnIdle + a pending drain, mirroring
// startAsyncRun's goroutine defers. Cleanup is a SYNCHRONOUS defer that finishes
// before the call returns, so a sequential next SendSync on the same session
// never observes a stale active run. Order mirrors run_start.go: Cleanup first
// (HasActiveRun authoritative), then ReclaimOnIdle, then drain.
func (h *Handler) withSyncRunLifecycle(
	ctx context.Context, sessionKey, clientRunID string, automation bool,
	fn func(context.Context) (*SyncResult, error),
) (*SyncResult, error) {
	return h.withSyncRunLifecycleAdmission(ctx, sessionKey, clientRunID, false, nil, automation, fn)
}

func (h *Handler) withAdmittedSyncRunLifecycle(
	ctx context.Context, sessionKey, clientRunID string, automation bool,
	fn func(context.Context) (*SyncResult, error),
) (*SyncResult, error) {
	return h.withSyncRunLifecycleAdmission(ctx, sessionKey, clientRunID, true, nil, automation, fn)
}

func (h *Handler) withAdmittedSyncRunLifecycleReleasingTicket(
	ctx context.Context,
	sessionKey, clientRunID string,
	ticket *syncAdmissionTicket,
	automation bool,
	fn func(context.Context) (*SyncResult, error),
) (*SyncResult, error) {
	var onRegistered func()
	if ticket != nil {
		onRegistered = ticket.release
	}
	return h.withSyncRunLifecycleAdmission(ctx, sessionKey, clientRunID, true, onRegistered, automation, fn)
}

func (h *Handler) withSyncRunLifecycleAdmission(
	ctx context.Context, sessionKey, clientRunID string, admitted bool, onRegistered func(), automation bool,
	fn func(context.Context) (*SyncResult, error),
) (*SyncResult, error) {
	runCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	entry := &AbortEntry{
		SessionKey: sessionKey,
		ClientRun:  clientRunID,
		CancelFn:   cancel,
		ExpiresAt:  time.Now().Add(4 * time.Hour),
		Automation: automation,
	}
	var registered bool
	if admitted {
		var registerErr error
		registered, registerErr = h.registerAdmittedSyncWhenSessionIdle(ctx, clientRunID, entry)
		if registerErr != nil {
			return nil, registerErr
		}
	} else {
		registered = h.abort.TryRegister(clientRunID, entry)
	}
	if !registered {
		return nil, ErrRuntimeDraining
	}
	// The ingress ticket protects the steer-vs-own-turn decision and the final
	// registration, not the whole model run. Once this run is visible in the
	// abort tracker, later short inputs may safely steer into it; later own-turn
	// inputs will reserve their own ticket and wait for this run to become idle.
	if onRegistered != nil {
		onRegistered()
	}
	// The abort tracker answers cancellation/steer questions, while the session
	// lifecycle backs transcript.turnRunning and reconnect recovery. Sync runs
	// need both views to transition together.
	if h.sessions != nil {
		h.sessions.ApplyLifecycleEvent(sessionKey, session.LifecycleEvent{
			Phase: session.PhaseStart,
			Ts:    time.Now().UnixMilli(),
		})
	}
	if h.broadcast != nil {
		broadcastPayload(h.broadcast, "sessions.changed", SessionsChangedEvent{
			SessionKey: sessionKey,
			Reason:     "message_sent",
			Status:     "running",
		})
	}
	defer func() {
		// Register an already-accepted continuation before removing this entry,
		// under the same decision lock used by producers. This keeps both the
		// normal sync handoff and shutdown drain free of an idle-gap race.
		h.finishRunWithPendingHandoff(sessionKey, clientRunID)
		if h.subagent != nil {
			h.subagent.ReclaimOnIdle(sessionKey)
		}
	}()

	res, err := fn(runCtx)
	h.finishSyncSessionLifecycle(runCtx, sessionKey, res, err)
	return res, err
}

// registerAdmittedSyncWhenSessionIdle closes the final idle-check/register
// gap for synchronous requests. The earlier admitSyncWhenSessionIdle call is a
// cheap wait before prompt preparation, but two idle callers can pass it at
// once. Rechecking and RegisterAdmitted under the same per-session decision
// lock ensures only one becomes active; the other waits for the registered run
// (and any queued continuation) to finish.
func (h *Handler) registerAdmittedSyncWhenSessionIdle(
	ctx context.Context,
	clientRunID string,
	entry *AbortEntry,
) (bool, error) {
	if h == nil || h.abort == nil || entry == nil {
		return false, ErrRuntimeDraining
	}
	if h.mergeWindow == nil {
		return h.abort.RegisterAdmitted(clientRunID, entry), nil
	}
	for {
		// Take the wakeup BEFORE testing the registry: an already-idle session
		// hands back a closed channel, so the predecessor can finish anywhere in
		// this loop without the wait missing its transition.
		idle := h.abort.SessionIdleWait(entry.SessionKey)
		sessLock := h.mergeWindow.SessionLock(entry.SessionKey)
		sessLock.Lock()
		if !h.abort.HasActiveRun(entry.SessionKey) {
			registered := h.abort.RegisterAdmitted(clientRunID, entry)
			sessLock.Unlock()
			if !registered {
				return false, ErrRuntimeDraining
			}
			return true, nil
		}
		sessLock.Unlock()

		if err := waitSessionIdle(ctx, idle); err != nil {
			return false, err
		}
	}
}

// waitSessionIdle blocks until the predecessor's run leaves the abort registry
// or the caller gives up. A run that dies without cleanup is reclaimed by the
// tracker's GC sweep, which also releases this wait, so a leaked entry cannot
// wedge a session's ingress queue permanently.
func waitSessionIdle(ctx context.Context, idle <-chan struct{}) error {
	select {
	case <-idle:
		return nil
	case <-ctx.Done():
		if cause := context.Cause(ctx); cause != nil {
			return cause
		}
		return ctx.Err()
	}
}

func (h *Handler) finishSyncSessionLifecycle(runCtx context.Context, sessionKey string, res *SyncResult, runErr error) {
	phase := session.PhaseEnd
	reason := "completed"
	status := "done"
	failureReason := ""
	if runErr != nil {
		if context.Cause(runCtx) != nil {
			reason = "aborted"
			status = "killed"
		} else {
			phase = session.PhaseError
			reason = "error"
			status = "failed"
			failureReason = classifyRunFailureReason(runErr)
		}
	} else if res == nil {
		phase = session.PhaseError
		reason = "error"
		status = "failed"
		failureReason = "응답 결과가 비어 있습니다."
	}
	if h.sessions != nil {
		h.sessions.ApplyLifecycleEvent(sessionKey, session.LifecycleEvent{
			Phase:         phase,
			Ts:            time.Now().UnixMilli(),
			FailureReason: failureReason,
		})
	}
	if h.broadcast != nil {
		broadcastPayload(h.broadcast, "sessions.changed", SessionsChangedEvent{
			SessionKey: sessionKey,
			Reason:     reason,
			Status:     status,
		})
	}
}

// steerMaxRunes bounds which mid-run follow-ups fold into the active turn as a
// steer note. Longer messages read as a genuinely new request and keep the
// current behavior (their own run).
const steerMaxRunes = 400

// admitSyncWhenSessionIdle blocks until no run is registered for sessionKey.
// Detached capture turns (miniapp.capture.*) and in-flight sync/stream runs
// both register in the abort tracker; without this gate a second SendSync would
// race the same transcript instead of queueing like chat.send does.
func (h *Handler) admitSyncWhenSessionIdle(ctx context.Context, sessionKey string) error {
	if h == nil || h.abort == nil {
		return nil
	}
	for {
		idle := h.abort.SessionIdleWait(sessionKey)
		if h.abort.HasActiveRun(sessionKey) {
			if err := waitSessionIdle(ctx, idle); err != nil {
				return err
			}
			continue
		}
		if h.mergeWindow == nil {
			return nil
		}
		sessLock := h.mergeWindow.SessionLock(sessionKey)
		sessLock.Lock()
		busy := h.abort.HasActiveRun(sessionKey)
		sessLock.Unlock()
		if !busy {
			return nil
		}
	}
}

// isNativeClientDelivery reports whether a delivery targets the native app's
// "client" channel — an interactive surface that returns its reply as the RPC
// result, distinct from the autonomous AutoDeliveredOutput relays (cron,
// mailpoll, …) that push their reply on other channels. Kept as a literal to
// avoid a chat→handler import cycle; the value matches handler/chat's
// NativeClientChannel and the "client" channel deliveryFromSessionKey stamps.
func isNativeClientDelivery(d *DeliveryContext) bool {
	return d != nil && d.Channel == "client"
}

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
// EphemeralUser surfaces), short plain text, and an active run. If the run
// finishes between the check and the hook's drain, the note is consumed by this
// session's next run (the steer hook drains before every LLM call) — deferred,
// never lost.
func (h *Handler) trySteerIntoActiveRun(sessionKey, message string, opts *SyncOptions) (*SyncResult, bool) {
	if opts == nil || !opts.TrustedDirectUserInput || len(opts.Messages) > 0 || opts.EphemeralUser {
		return nil, false
	}
	// AutoDeliveredOutput marks runs whose reply the completion layer delivers
	// instead of the message tool. It covers BOTH truly autonomous relays (cron,
	// mailpoll, goal, event-ingest — not interactive) AND the native client,
	// which IS interactive but returns its reply as the RPC result rather than
	// pushing it. Only the autonomous relays should block steer; the native
	// "client" surface is exactly the mid-turn-correction case ("아 남도에코만
	// 봐줘") steer exists for. Without this carve-out steer was dead on the sole
	// native entry — every follow-up raced a second concurrent run instead.
	if opts.AutoDeliveredOutput && !isNativeClientDelivery(opts.Delivery) {
		return nil, false
	}
	trimmed := strings.TrimSpace(message)
	if trimmed == "" || utf8.RuneCountInString(trimmed) > steerMaxRunes {
		return nil, false
	}
	// Interactive runs only: if the sole active run on this session is an
	// autonomous relay (heartbeat/cron/mailpoll riding client:main), fold
	// nothing — the user isn't watching that turn. Their message becomes a
	// normal new turn instead.
	if !h.abort.HasActiveInteractiveRun(sessionKey) {
		return nil, false
	}
	// A profile assertion/correction/forget needs its own ordered turn so its
	// deterministic induction runs on the exact user message. Folding it into
	// the active model turn would persist the transcript correction but induce
	// only the older RunParams.Message when that turn finishes.
	if !h.EnqueueSteer(sessionKey, trimmed) {
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
	if h != nil && h.briefcaseMode {
		return nil, false
	}
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
	h.handleSlashCommand(leafbind.NewShortID("slash"), sessionKey, delivery, cmd, func(text string) {
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
	if h.abort == nil || !h.abort.AcquireAdmission() {
		return nil, ErrRuntimeDraining
	}
	defer h.abort.ReleaseAdmission()
	if res, handled := h.trySlashSync(sessionKey, message, opts); handled {
		if onDelta != nil && res.Text != "" {
			onDelta(res.Text)
		}
		return res, nil
	}
	ticket := h.reserveSyncAdmission(sessionKey)
	defer ticket.release()
	if err := ticket.wait(ctx); err != nil {
		return nil, err
	}
	// Keep streaming ingress on the same ordered steer decision as SendSync.
	if res, handled := h.trySteerIntoActiveRun(sessionKey, message, opts); handled {
		if onDelta != nil && res.Text != "" {
			onDelta(res.Text)
		}
		return res, nil
	}
	if err := h.admitSyncWhenSessionIdle(ctx, sessionKey); err != nil {
		return nil, err
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
		sinks.OnReasoning = opts.OnReasoning
	}
	return h.withAdmittedSyncRunLifecycleReleasingTicket(ctx, sessionKey, params.ClientRunID, ticket,
		isAutomationRun(params),
		func(runCtx context.Context) (*SyncResult, error) {
			result, err := executeAgentRunWithDelta(runCtx, params, deps, sinks, h.logger)
			if err != nil {
				if errors.Is(context.Cause(runCtx), context.DeadlineExceeded) {
					res, buildErr := h.buildSyncTimeoutFallback(model)
					if buildErr == nil {
						persistSynthesizedSyncFallback(params, deps, res, h.logger)
					}
					return res, buildErr
				}
				return nil, err
			}
			normalizeRunCardReplies(result.AgentResult, params, deps, h.logger)
			finishTurnSideEffects(deps, params, result.AgentResult, h.logger)
			res, err := h.buildSyncResult(model, result)
			if err == nil {
				persistSynthesizedSyncFallback(params, deps, res, h.logger)
				h.autoTitleSessionAsync(sessionKey, message, res)
			}
			return res, err
		})
}

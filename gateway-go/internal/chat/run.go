package chat

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/aurora"
	"github.com/choiceoh/deneb/gateway-go/internal/chat/streaming"
	"github.com/choiceoh/deneb/gateway-go/internal/chatport"
	"github.com/choiceoh/deneb/gateway-go/internal/hooks"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/memory"
	"github.com/choiceoh/deneb/gateway-go/internal/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/provider"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
	"github.com/choiceoh/deneb/gateway-go/internal/shortid"
	"github.com/choiceoh/deneb/gateway-go/internal/telegram"
	"github.com/choiceoh/deneb/gateway-go/internal/unified"
	"github.com/choiceoh/deneb/gateway-go/internal/vega"
)

// cachedWorkspaceDir caches the resolved workspace directory at startup
// to avoid disk I/O (config.LoadConfigFromDefaultPath) on every chat message.
// Single-user deployment: config doesn't change at runtime.
var (
	cachedWorkspaceDir     string
	cachedWorkspaceDirOnce sync.Once
)

// RunParams holds all parameters for an async agent run.
type RunParams struct {
	SessionKey   string
	Message      string
	Attachments  []ChatAttachment
	Model        string // role name ("main", "lightweight", "fallback"); raw model ID only via /model override
	System       string // system prompt override
	ClientRunID  string
	Delivery     *DeliveryContext
	WorkspaceDir string // per-channel workspace override (empty = use global default)

	// Sampling parameters (from OpenAI-compatible API pass-through).
	Temperature      *float64
	TopP             *float64
	MaxTokens        *int // overrides default max output tokens
	FrequencyPenalty *float64
	PresencePenalty  *float64
	Stop             []string
	ResponseFormat   *llm.ResponseFormat
	ToolChoice       any // "auto", "none", "required", or structured object

	// PrebuiltMessages, when set, replaces the normal transcript-based context
	// assembly. Used by the OpenAI-compatible HTTP API to pass through the full
	// conversation history from the client.
	PrebuiltMessages []llm.Message

	// ContinuationIndex tracks the current autonomous continuation number.
	// 0 = original run, 1+ = continuation runs. Not set by external callers.
	ContinuationIndex int

	// DeepWork enables extended autonomous mode (2-3 hours): maxTurns=50,
	// timeout=30min/run, up to 30 continuations. Activated via /deepwork directive.
	DeepWork bool
}

// Agent run defaults.
const (
	defaultMaxTokens     = 8192
	defaultMaxTurns      = 25
	defaultAgentTimeout  = 60 * time.Minute
	maxCompactionRetries = 2
)

// runDeps holds the dependencies the async run needs from the Handler.
// Optional fields (may be nil): transcript, tools, authManager,
// broadcast, broadcastRaw, jobTracker. Required: sessions, logger.
type runDeps struct {
	sessions        *session.Manager                  // required
	llmClient       *llm.Client                       // optional; resolved from authManager if nil
	transcript      TranscriptStore                   // optional; history unavailable without it
	tools           *ToolRegistry                     // optional; no tool use if nil
	authManager     *provider.AuthManager             // optional; uses pre-configured client if nil
	providerRuntime *provider.ProviderRuntimeResolver // optional; runtime auth, missing-auth messages
	broadcast       BroadcastFunc                     // optional
	broadcastRaw    streaming.BroadcastRawFunc        // optional
	jobTracker      *agent.JobTracker                 // optional
	replyFunc       ReplyFunc                         // optional; delivers response to originating channel
	mediaSendFn     MediaSendFunc                     // optional; delivers files to originating channel
	typingFn        TypingFunc                        // optional; sends typing indicator during run
	reactionFn      ReactionFunc                      // optional; sets emoji reaction for status phases
	// channelUploadLimitFn returns the max file upload size for a channel ID.
	// Returns 0 if no limit is registered (tool applies its own default).
	channelUploadLimitFn func(channelID string) int64 // optional
	toolProgressFn       ToolProgressFunc             // optional; reports tool events to channel integrations
	draftEditFn          DraftEditFunc                // optional; sends/edits streaming draft messages
	draftDeleteFn        DraftDeleteFunc              // optional; deletes streaming draft messages
	providerConfigs      map[string]ProviderConfig    // optional; config-based provider credentials
	logger               *slog.Logger                 // required (defaults to slog.Default)

	auroraStore    *aurora.Store             // optional; enables Aurora compaction
	vegaBackend    vega.Backend              // optional; enables knowledge prefetch
	memoryStore    *memory.Store             // optional; structured memory (Honcho-style)
	memoryEmbedder *memory.Embedder          // optional; fact embedding
	unifiedStore   *unified.Store            // optional; unified memory (search + tier-1)
	dreamTurnFn    func(ctx context.Context) // optional; increments dream turn via autonomous
	agentLog       *agentlog.Writer          // optional; enables agent detail logging
	registry       *modelrole.Registry       // centralized model role registry
	// emitAgentFn sends agent lifecycle events (run.start, tool.start, tool.end)
	// to the gateway event subscription pipeline. Optional; nil if not wired.
	emitAgentFn func(kind, sessionKey, runID string, payload map[string]any)
	// emitTranscriptFn sends transcript updates (user/assistant message appends)
	// to the gateway event subscription pipeline. Optional; nil if not wired.
	emitTranscriptFn     func(sessionKey string, message any, messageID string)
	sessionMemory        *SessionMemoryStore // optional; structured session state
	contextCfg           ContextConfig
	compactionCfg        aurora.SweepConfig
	defaultModel         string
	subagentDefaultModel string
	defaultSystem        string
	maxTokens            int
	// shutdownCtx is the server lifecycle context; used to bound background
	// goroutines (e.g., auto-memory extraction) so they stop on server shutdown.
	shutdownCtx context.Context
	// internalHookRegistry fires programmatic internal hooks.
	internalHookRegistry *hooks.InternalRegistry
	// drainPendingFn drains the next queued message for a session after the
	// current run completes. Set by the Handler; nil disables pending queue.
	drainPendingFn func(sessionKey string) *RunParams
	// startRunFn starts a new async run (for processing queued messages).
	// Set by the Handler; nil disables pending queue processing.
	startRunFn func(params RunParams)
	// maxContinuations is the maximum number of autonomous continuation runs
	// triggered by the continue_run tool. 0 means use default (5).
	maxContinuations int
	// continuationEnabled controls whether the continue_run tool is functional.
	// When false (sync paths), the ContinuationSignal is not injected into tool
	// context, so the tool returns "not available" instead of silently no-oping.
	continuationEnabled bool

	// subagentNotifyCh receives completion notifications for child sessions
	// spawned by the current session. Consumed by DeferredSystemText to inject
	// notifications mid-run without polling. nil if not applicable.
	subagentNotifyCh <-chan string

	// chatport boundary: injected implementations that decouple chat from autoreply.
	// When nil, the corresponding functionality is simply skipped.
	newTypingSignaler    func(onStart func()) chatport.TypingSignaler // optional; creates phase-aware typing signaler
	sanitizeDraft        chatport.DraftSanitizerFunc                  // optional; cleans streaming draft text
	parseReplyDirectives chatport.ParseReplyDirectivesFunc            // optional; parses reply directives
	isTransientError     chatport.IsTransientErrorFunc                // optional; checks transient HTTP errors
}

// abbreviateSession shortens channel prefixes in session keys for compact log output.
// e.g. "telegram:7074071666" → "te:7074071666"
func abbreviateSession(key string) string {
	prefixes := [][2]string{
		{"telegram:", "te:"},
	}
	for _, p := range prefixes {
		if len(key) > len(p[0]) && key[:len(p[0])] == p[0] {
			return p[1] + key[len(p[0]):]
		}
	}
	return key
}

// isSystemSession reports whether key is a system-internal session (e.g. "system:diary-heartbeat").
// System sessions must not write to the shared Aurora store because their messages
// (diary prompts, heartbeat responses) would contaminate the user's conversation context.
func isSystemSession(key string) bool {
	return strings.HasPrefix(key, "system:")
}

// isMainSession reports whether key is a top-level direct session (e.g. "telegram:123").
// Sub-sessions ("telegram:123:task:ts"), system ("system:*"), cron, hook, and
// bare keys (no colon, e.g. "dev-chat-xxx") return false.
func isMainSession(key string) bool {
	if isSystemSession(key) {
		return false
	}
	idx := strings.Index(key, ":")
	if idx < 0 {
		return false
	}
	return !strings.Contains(key[idx+1:], ":")
}

// runAgentAsync is the background goroutine that executes an agent run.
// It persists the user message, assembles context, calls the LLM agent loop,
// persists the result, and broadcasts completion events.
func runAgentAsync(ctx context.Context, params RunParams, deps runDeps) {
	logger := deps.logger
	if logger == nil {
		logger = slog.Default()
	}
	var logArgs []any
	if !isMainSession(params.SessionKey) {
		logArgs = append(logArgs, "session", abbreviateSession(params.SessionKey))
	}
	if params.ClientRunID != "" {
		logArgs = append(logArgs, "runId", params.ClientRunID)
	}
	if len(logArgs) > 0 {
		logger = logger.With(logArgs...)
	}

	// Emit lifecycle start event for agent job tracker.
	if deps.jobTracker != nil {
		deps.jobTracker.OnLifecycleEvent(agent.LifecycleEvent{
			RunID: params.ClientRunID,
			Phase: "start",
			Ts:    time.Now().UnixMilli(),
		})
	}

	// Create streaming broadcaster for this run.
	var broadcaster *streaming.Broadcaster
	if deps.broadcastRaw != nil {
		broadcaster = streaming.NewBroadcaster(deps.broadcastRaw, params.SessionKey, params.ClientRunID)
		broadcaster.EmitStarted()
	}

	// Inject delivery context and reply function into ctx so tools
	// (especially the message tool) can send proactive messages.
	if params.Delivery != nil {
		ctx = WithDeliveryContext(ctx, params.Delivery)
	}
	if deps.replyFunc != nil {
		ctx = WithReplyFunc(ctx, deps.replyFunc)
	}
	if deps.mediaSendFn != nil {
		ctx = WithMediaSendFunc(ctx, deps.mediaSendFn)
	}
	// Inject the channel-specific upload limit so send_file can enforce
	// the correct per-channel maximum without hard-coding channel names.
	if deps.channelUploadLimitFn != nil && params.Delivery != nil {
		if limit := deps.channelUploadLimitFn(params.Delivery.Channel); limit > 0 {
			ctx = WithMaxUploadBytes(ctx, limit)
		}
	}
	ctx = WithSessionKey(ctx, params.SessionKey)

	// Set up phase-aware typing indicator for channel delivery (e.g., Telegram).
	// The factory (injected via chatport boundary) creates a TypingSignaler with
	// a 5s keepalive matching Telegram's sendChatAction TTL.
	var typingSignaler chatport.TypingSignaler
	if deps.newTypingSignaler != nil && deps.typingFn != nil && params.Delivery != nil {
		delivery := params.Delivery
		typingSignaler = deps.newTypingSignaler(func() { _ = deps.typingFn(ctx, delivery) })
		typingSignaler.SignalRunStart()
	}

	// Set up status reaction controller for phase-aware emoji on the user's message.
	// Shows: 👀 queued → 🤔 thinking → 🔥 tool → ⚡ web → 👍 done.
	var statusCtrl *telegram.StatusReactionController
	if deps.reactionFn != nil && params.Delivery != nil && params.Delivery.MessageID != "" {
		delivery := params.Delivery
		phaseEmojis := telegram.StatusReactionEmojis{
			Queued:     "👀",
			Thinking:   "🤔",
			Tool:       "🔥",
			Coding:     "🔥",
			Web:        "⚡",
			Done:       "👍",
			Error:      "😱",
			StallSoft:  "🥱",
			StallHard:  "😨",
			Compacting: "🤔",
		}
		statusCtrl = telegram.NewStatusReactionController(telegram.StatusReactionControllerParams{
			Enabled: true,
			SetReaction: func(emoji string) error {
				rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
				defer cancel()
				return deps.reactionFn(rctx, delivery, emoji)
			},
			Emojis: &phaseEmojis,
			OnError: func(err error) {
				logger.Warn("status reaction failed", "error", err)
			},
		})
		statusCtrl.SetQueued()
	}

	// Create agent detail logger for this run.
	runLog := agentlog.NewRunLogger(deps.agentLog, params.SessionKey, params.ClientRunID)

	// Run the agent and capture result.
	chatResult, err := executeAgentRun(ctx, params, deps, broadcaster, typingSignaler, statusCtrl, logger, runLog)

	// Stop typing indicator before delivering the reply.
	if typingSignaler != nil {
		typingSignaler.Stop()
	}

	// Persist interrupted context: when the run was aborted while tools were
	// executing, save a context note to the transcript so the next run knows
	// what the assistant was doing. Without this, the next run has no memory
	// of the interrupted work and starts from scratch.
	if chatResult != nil && len(chatResult.InterruptedToolNames) > 0 && deps.transcript != nil {
		persistInterruptedContext(deps, params.SessionKey, chatResult.AgentResult, logger)
	}

	// Handle completion.
	now := time.Now().UnixMilli()
	if err != nil {
		if statusCtrl != nil {
			statusCtrl.SetError()
			statusCtrl.CloseAfterDrain()
		}
		handleRunError(ctx, params, deps, broadcaster, logger, err, now, runLog)
		return
	}

	if statusCtrl != nil {
		statusCtrl.SetDone()
		statusCtrl.CloseAfterDrain()
	}
	handleRunSuccess(ctx, params, deps, broadcaster, logger, chatResult.AgentResult, now, runLog)

	// Process pending message: if the user sent a message while this run was
	// active, it was queued. Now that the run is complete, drain and process it.
	// User messages take priority over autonomous continuation.
	if deps.drainPendingFn != nil && deps.startRunFn != nil {
		if pending := deps.drainPendingFn(params.SessionKey); pending != nil {
			logger.Info("processing queued message after run completion",
				"sessionKey", params.SessionKey)
			deps.startRunFn(*pending)
			return
		}
	}

	// Autonomous continuation is active in Normal and Work modes (or DeepWork).
	// Chat mode (conversation-only) completes after a single run.
	if !params.DeepWork {
		sess := deps.sessions.Get(params.SessionKey)
		if sess == nil || sess.Mode == session.ModeChat {
			return
		}
	}

	// Autonomous continuation: trigger a new run when:
	// (a) the LLM explicitly called continue_run, OR
	// (b) the agent was cut off by hitting max_turns, OR
	// (c) the run had meaningful work (code changes) and this is the first run
	//     → auto-start a verification continuation to check the work.
	maxConts := deps.maxContinuations
	if maxConts <= 0 {
		maxConts = 5
	}
	if params.DeepWork {
		maxConts = 30
	}

	var contReason string
	var contMessage string
	if chatResult.ContSignal != nil && chatResult.ContSignal.Requested() {
		contReason = chatResult.ContSignal.Reason()
		contMessage = "[System: Autonomous continuation %d/%d. Reason: %s.\n" +
			"이전 실행의 컨텍스트:\n" +
			"- 사용한 도구: " + summarizeToolActivity(chatResult.ToolActivities) + "\n" +
			"- 에러 발생 도구: " + summarizeErrorTools(chatResult.ToolActivities) + "\n" +
			"Continue your work. 동일한 실패를 반복하지 마세요.]"
	} else if chatResult.StopReason == "max_turns" || chatResult.StopReason == "timeout" {
		contReason = fmt.Sprintf("에이전트가 %s에 도달했지만 작업이 진행 중이었습니다", chatResult.StopReason)
		contMessage = "[System: Autonomous continuation %d/%d. Reason: %s.\n" +
			"이전 실행 요약:\n" +
			"- 실행 턴: " + fmt.Sprintf("%d", chatResult.Turns) + "\n" +
			"- 사용한 도구: " + summarizeToolActivity(chatResult.ToolActivities) + "\n" +
			"- 에러 발생 도구: " + summarizeErrorTools(chatResult.ToolActivities) + "\n" +
			"이전 실행이 중단된 지점부터 이어서 작업하세요. 동일한 접근법으로 같은 에러가 반복되면 다른 전략을 시도하세요.]"
	} else if params.ContinuationIndex == 0 && hadMutatingToolActivity(chatResult.ToolActivities) {
		contReason = "코드 변경 후 검증"
		contMessage = "[System: Autonomous continuation %d/%d. 이전 실행에서 코드를 변경했습니다.\n" +
			"변경한 도구: " + summarizeToolActivity(chatResult.ToolActivities) + "\n" +
			"검증 사항: 빌드, 테스트 통과, 추가 작업 필요 여부. 모든 것이 정상이면 최종 요약으로 마무리하세요.]"
	}

	if contReason != "" && params.ContinuationIndex < maxConts && deps.startRunFn != nil {
		nextIndex := params.ContinuationIndex + 1
		logger.Info("autonomous continuation triggered",
			"reason", contReason,
			"continuation", nextIndex,
			"maxContinuations", maxConts,
			"stopReason", chatResult.StopReason,
			"deepWork", params.DeepWork)

		// Build continuation message; inject session memory in deep work mode.
		contMsg := fmt.Sprintf(contMessage, nextIndex, maxConts, contReason)
		if params.DeepWork && deps.sessionMemory != nil {
			if sm := deps.sessionMemory.Get(params.SessionKey); sm != "" {
				contMsg += "\n\n[Session Memory — your task state:]\n" + sm
			}
		}

		contParams := RunParams{
			SessionKey:        params.SessionKey,
			ClientRunID:       shortid.New("cont"),
			Message:           contMsg,
			Delivery:          params.Delivery,
			Model:             params.Model,
			WorkspaceDir:      params.WorkspaceDir,
			ContinuationIndex: nextIndex,
			DeepWork:          params.DeepWork,
		}
		deps.startRunFn(contParams)
	}
}

// hadMutatingToolActivity reports whether any tool call in the run was a
// code-changing operation (edit, write, exec, etc.). Used to decide if an
// automatic verification continuation is warranted.
func hadMutatingToolActivity(activities []agent.ToolActivity) bool {
	for _, a := range activities {
		if mutatingTools[a.Name] {
			return true
		}
	}
	return false
}

// summarizeToolActivity returns a compact summary of tools used in a run.
func summarizeToolActivity(activities []agent.ToolActivity) string {
	if len(activities) == 0 {
		return "없음"
	}
	counts := make(map[string]int)
	for _, a := range activities {
		counts[a.Name]++
	}
	var parts []string
	for name, count := range counts {
		if count > 1 {
			parts = append(parts, fmt.Sprintf("%s×%d", name, count))
		} else {
			parts = append(parts, name)
		}
	}
	if len(parts) > 8 {
		parts = parts[:8]
		parts = append(parts, "...")
	}
	return strings.Join(parts, ", ")
}

// summarizeErrorTools returns names of tools that had errors.
func summarizeErrorTools(activities []agent.ToolActivity) string {
	var errs []string
	seen := make(map[string]bool)
	for _, a := range activities {
		if a.IsError && !seen[a.Name] {
			errs = append(errs, a.Name)
			seen[a.Name] = true
		}
	}
	if len(errs) == 0 {
		return "없음"
	}
	return strings.Join(errs, ", ")
}

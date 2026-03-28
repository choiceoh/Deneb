// Package chat implements the chat/LLM message routing RPC methods.
//
// This mirrors src/gateway/server-methods/chat/chat.ts from the TypeScript
// codebase. The Go gateway handles chat message orchestration, history
// retrieval, abort signaling, and message injection.
//
// Session execution (sessions.send/steer/abort) runs natively in Go:
// the LLM agent loop, tool execution, context assembly, and compaction
// are handled without bridging to Node.js.
package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/aurora"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/memory"
	"github.com/choiceoh/deneb/gateway-go/internal/provider"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
	"github.com/choiceoh/deneb/gateway-go/internal/shortid"
	"github.com/choiceoh/deneb/gateway-go/internal/vega"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// BroadcastFunc sends an event to all matching subscribers.
type BroadcastFunc func(event string, payload any) (int, []error)

// BroadcastRawFunc sends pre-serialized event data to all matching subscribers.
type BroadcastRawFunc func(event string, data []byte) int

// ReplyFunc delivers the assistant response back to the originating channel.
// Called with the delivery context (channel + recipient) and the response text.
type ReplyFunc func(ctx context.Context, delivery *DeliveryContext, text string) error

// TypingFunc signals a typing indicator to the originating channel.
// Called periodically during an agent run to show "typing..." status.
type TypingFunc func(ctx context.Context, delivery *DeliveryContext) error

// ReactionFunc sets/removes an emoji reaction on the triggering message.
// Pass an empty emoji to remove reactions.
type ReactionFunc func(ctx context.Context, delivery *DeliveryContext, emoji string) error

// ProviderConfig holds credentials and endpoint for an LLM provider.
type ProviderConfig struct {
	APIKey  string `json:"apiKey"`
	BaseURL string `json:"baseUrl"`
	API     string `json:"api"` // "openai" (default) or "anthropic" (inferred from provider ID)
}

// DeliveryContext carries channel routing information for a chat message.
type DeliveryContext struct {
	Channel     string `json:"channel,omitempty"`
	To          string `json:"to,omitempty"`
	AccountID   string `json:"accountId,omitempty"`
	ThreadID    string `json:"threadId,omitempty"`
	MessageID   string `json:"messageId,omitempty"`   // triggering message ID for reply threading
	ToolProfile string `json:"toolProfile,omitempty"` // optional: "coding" restricts to code-related tools
}

// ChatMessage represents a message in a session transcript.
type ChatMessage struct {
	Role        string           `json:"role"`
	Content     string           `json:"content,omitempty"`
	Attachments []ChatAttachment `json:"attachments,omitempty"`
	Timestamp   int64            `json:"timestamp,omitempty"`
	ParentID    string           `json:"parentId,omitempty"`
	ID          string           `json:"id,omitempty"`
}

// ChatAttachment represents an attachment on a chat message.
type ChatAttachment struct {
	Type     string `json:"type"` // "image", "file", "audio", "video"
	MimeType string `json:"mimeType,omitempty"`
	URL      string `json:"url,omitempty"`
	Data     string `json:"data,omitempty"` // base64-encoded content (used for inline media)
	Name     string `json:"name,omitempty"`
	Size     int64  `json:"size,omitempty"`
}

// AbortEntry tracks an active abort controller for a running chat session.
type AbortEntry struct {
	SessionKey string
	ClientRun  string
	CancelFn   context.CancelFunc
	ExpiresAt  time.Time
}

// Handler manages chat RPC methods.
type Handler struct {
	sessions     *session.Manager
	broadcast    BroadcastFunc
	broadcastRaw BroadcastRawFunc
	logger       *slog.Logger

	// Native agent execution deps.
	llmClient       *llm.Client
	transcript      TranscriptStore
	tools           *ToolRegistry
	authManager     *provider.AuthManager
	jobTracker      *agent.JobTracker
	providerConfigs map[string]ProviderConfig
	auroraStore     *aurora.Store             // Aurora hierarchical compaction store
	vegaBackend     vega.Backend              // optional; knowledge prefetch
	memoryStore     *memory.Store             // optional; structured memory (Honcho-style)
	memoryEmbedder  *memory.Embedder          // optional; fact embedding
	dreamTurnFn     func(ctx context.Context) // optional; increments dream turn via autonomous
	agentLog        *agentlog.Writer          // optional; agent detail logging

	// Agent run configuration.
	contextCfg    ContextConfig
	compactionCfg CompactionConfig
	defaultModel  string
	defaultSystem string
	maxTokens     int

	replyFunc   ReplyFunc     // optional: delivers response to originating channel
	mediaSendFn MediaSendFunc // optional: delivers files to originating channel
	typingFn    TypingFunc    // optional: sends typing indicator during agent run
	reactionFn  ReactionFunc  // optional: sets emoji reaction on triggering message

	abortMu  sync.Mutex
	abortMap map[string]*AbortEntry // clientRunId -> entry
	done     chan struct{}          // signals abortGCLoop to stop

	// Dual-core: tracks background task progress per session for concurrent responses.
	taskProgressMu sync.RWMutex
	taskProgress   map[string]*TaskProgress // sessionKey -> progress

	// Dual-core: tracks active concurrent response cancel functions per session.
	// Only one concurrent response per session at a time; new ones cancel the previous.
	concRespMu     sync.Mutex
	concRespCancel map[string]context.CancelFunc // sessionKey -> cancel

	// maxHistoryBytes caps the total JSON bytes returned by chat.history.
	maxHistoryBytes int
	// maxHistoryCount caps the number of messages returned.
	maxHistoryCount int
	// maxMessageBytes caps a single message body before truncation.
	maxMessageBytes int
}

// HandlerConfig configures the chat handler.
type HandlerConfig struct {
	MaxHistoryBytes int
	MaxHistoryCount int
	MaxMessageBytes int

	// Native agent execution config.
	LLMClient       *llm.Client
	Transcript      TranscriptStore
	Tools           *ToolRegistry
	AuthManager     *provider.AuthManager
	JobTracker      *agent.JobTracker
	ProviderConfigs map[string]ProviderConfig // provider ID → config
	AuroraStore     *aurora.Store             // Aurora hierarchical compaction store
	VegaBackend     vega.Backend              // optional; enables knowledge prefetch in chat
	MemoryStore     *memory.Store             // optional; structured memory (Honcho-style)
	MemoryEmbedder  *memory.Embedder          // optional; fact embedding via SGLang
	DreamTurnFn     func(ctx context.Context) // optional; increments dream turn via autonomous
	AgentLog        *agentlog.Writer          // optional; agent detail logging
	ContextCfg      ContextConfig
	CompactionCfg   CompactionConfig
	DefaultModel    string
	DefaultSystem   string
	MaxTokens       int
}

// DefaultHandlerConfig returns sensible defaults.
func DefaultHandlerConfig() HandlerConfig {
	return HandlerConfig{
		MaxHistoryBytes: 2 * 1024 * 1024, // 2 MB
		MaxHistoryCount: 200,
		MaxMessageBytes: 128 * 1024, // 128 KB
		ContextCfg:      DefaultContextConfig(),
		CompactionCfg:   DefaultCompactionConfig(),
		MaxTokens:       defaultMaxTokens,
	}
}

// NewHandler creates a new chat handler.
func NewHandler(sessions *session.Manager, broadcast BroadcastFunc, logger *slog.Logger, cfg HandlerConfig) *Handler {
	if cfg.MaxHistoryBytes == 0 {
		defaults := DefaultHandlerConfig()
		cfg.MaxHistoryBytes = defaults.MaxHistoryBytes
		cfg.MaxHistoryCount = defaults.MaxHistoryCount
		cfg.MaxMessageBytes = defaults.MaxMessageBytes
	}
	h := &Handler{
		sessions:        sessions,
		broadcast:       broadcast,
		logger:          logger,
		llmClient:       cfg.LLMClient,
		transcript:      cfg.Transcript,
		tools:           cfg.Tools,
		authManager:     cfg.AuthManager,
		jobTracker:      cfg.JobTracker,
		providerConfigs: cfg.ProviderConfigs,
		auroraStore:     cfg.AuroraStore,
		vegaBackend:     cfg.VegaBackend,
		memoryStore:     cfg.MemoryStore,
		memoryEmbedder:  cfg.MemoryEmbedder,
		dreamTurnFn:     cfg.DreamTurnFn,
		agentLog:        cfg.AgentLog,
		contextCfg:      cfg.ContextCfg,
		compactionCfg:   cfg.CompactionCfg,
		defaultModel:    cfg.DefaultModel,
		defaultSystem:   cfg.DefaultSystem,
		maxTokens:       cfg.MaxTokens,
		abortMap:        make(map[string]*AbortEntry),
		taskProgress:    make(map[string]*TaskProgress),
		concRespCancel:  make(map[string]context.CancelFunc),
		done:            make(chan struct{}),
		maxHistoryBytes: cfg.MaxHistoryBytes,
		maxHistoryCount: cfg.MaxHistoryCount,
		maxMessageBytes: cfg.MaxMessageBytes,
	}
	go h.abortGCLoop()
	return h
}

// GetBroadcastRaw returns the current raw broadcast function (may be nil).
func (h *Handler) GetBroadcastRaw() BroadcastRawFunc {
	return h.broadcastRaw
}

// SetBroadcastRaw sets the raw broadcast function for streaming event relay.
func (h *Handler) SetBroadcastRaw(fn BroadcastRawFunc) {
	h.broadcastRaw = fn
}

// GetReplyFunc returns the current reply function (may be nil).
func (h *Handler) GetReplyFunc() ReplyFunc {
	return h.replyFunc
}

// SetReplyFunc sets the function that delivers assistant responses back to the
// originating channel (e.g., Telegram). Called after each successful agent run
// when a DeliveryContext is present.
func (h *Handler) SetReplyFunc(fn ReplyFunc) {
	h.replyFunc = fn
}

// SetMediaSendFunc sets the function that delivers files back to the
// originating channel (e.g., Telegram). Used by the send_file tool.
func (h *Handler) SetMediaSendFunc(fn MediaSendFunc) {
	h.mediaSendFn = fn
}

// SetTypingFunc sets the function that sends typing indicators to the
// originating channel (e.g., Telegram "typing..." status) during agent runs.
func (h *Handler) SetTypingFunc(fn TypingFunc) {
	h.typingFn = fn
}

// SetReactionFunc sets the function that manages emoji reactions on the
// triggering message to indicate agent status phases (thinking, tool use, done).
func (h *Handler) SetReactionFunc(fn ReactionFunc) {
	h.reactionFn = fn
}

// HandleBtw processes a side question (/btw) without affecting the main
// session context. It dispatches a lightweight chat.send-style request
// with the side question, using the fast model default.
func (h *Handler) HandleBtw(_ context.Context, sessionKey, question string) (string, error) {
	// Build a side-question request and dispatch through Send.
	// The answer is returned directly without persisting to the main transcript.
	req, err := protocol.NewRequestFrame("btw-internal", "chat.send", map[string]any{
		"sessionKey": sessionKey,
		"message":    question,
		"btw":        true,
	})
	if err != nil {
		return "", fmt.Errorf("btw request build failed: %w", err)
	}

	resp := h.Send(context.Background(), req)
	if resp == nil {
		return "", fmt.Errorf("btw returned nil response")
	}
	if !resp.OK {
		msg := "unknown error"
		if resp.Error != nil {
			msg = resp.Error.Message
		}
		return "", fmt.Errorf("btw failed: %s", msg)
	}

	// Extract text from response payload.
	var result struct {
		Text string `json:"text"`
	}
	if len(resp.Payload) > 0 {
		if err := json.Unmarshal(resp.Payload, &result); err != nil {
			return "", fmt.Errorf("btw response unmarshal failed: %w", err)
		}
	}
	return result.Text, nil
}

// Close stops background goroutines and cancels all active abort entries.
func (h *Handler) Close() {
	// Signal abortGCLoop to exit.
	select {
	case <-h.done:
	default:
		close(h.done)
	}

	h.abortMu.Lock()
	defer h.abortMu.Unlock()
	for _, entry := range h.abortMap {
		entry.CancelFn()
	}
	h.abortMap = make(map[string]*AbortEntry)
}

// --- RPC method handlers ---

// Send handles "chat.send" — the primary message ingestion endpoint.
// Sanitizes input, starts an async agent run, and immediately returns.
func (h *Handler) Send(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
	var p struct {
		SessionKey  string           `json:"sessionKey"`
		Message     string           `json:"message"`
		Attachments []ChatAttachment `json:"attachments,omitempty"`
		Delivery    *DeliveryContext `json:"delivery,omitempty"`
		ClientRunID string           `json:"clientRunId,omitempty"`
		Model       string           `json:"model,omitempty"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrInvalidRequest, "invalid chat.send params: "+err.Error()))
	}
	if p.SessionKey == "" {
		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrMissingParam, "sessionKey is required"))
	}
	if p.Message == "" && len(p.Attachments) == 0 {
		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrMissingParam, "message or attachments required"))
	}
	if len(p.Message) > h.maxMessageBytes {
		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrInvalidRequest,
			fmt.Sprintf("message too large: %d bytes exceeds limit of %d", len(p.Message), h.maxMessageBytes)))
	}

	// Pre-process slash commands before dispatching to agent.
	if slashResult := ParseSlashCommand(p.Message); slashResult != nil && slashResult.Handled {
		return h.handleSlashCommand(req.ID, p.SessionKey, p.Delivery, slashResult)
	}

	runParams := RunParams{
		SessionKey:  p.SessionKey,
		Message:     sanitizeInput(p.Message),
		Attachments: p.Attachments,
		Delivery:    p.Delivery,
		ClientRunID: p.ClientRunID,
		Model:       p.Model,
	}

	// Dual-core: if a task is running, route concurrently or interrupt.
	if tp := h.getTaskProgress(p.SessionKey); tp != nil {
		if classifyRoute(p.Message) == RouteConcurrent {
			// Same brain, parallel response — task continues uninterrupted.
			return h.startConcurrentResponse(req.ID, runParams, tp)
		}
		// Explicit interrupt: fall through to cancel + new task.
	}

	// Default path: interrupt any active run + concurrent response, start new task.
	h.CancelConcurrentResponse(p.SessionKey)
	h.InterruptActiveRun(p.SessionKey)

	return h.startAsyncRun(req.ID, runParams, false)
}

// SessionsSend handles "sessions.send" — interrupts any active run, then starts a new one.
func (h *Handler) SessionsSend(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
	var p struct {
		Key            string           `json:"key"`
		Message        string           `json:"message"`
		Thinking       string           `json:"thinking,omitempty"`
		Attachments    []ChatAttachment `json:"attachments,omitempty"`
		TimeoutMs      int              `json:"timeoutMs,omitempty"`
		IdempotencyKey string           `json:"idempotencyKey,omitempty"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrInvalidRequest, "invalid sessions.send params: "+err.Error()))
	}
	if p.Key == "" {
		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrMissingParam, "key is required"))
	}

	// Interrupt any active run + concurrent response for this session.
	h.CancelConcurrentResponse(p.Key)
	h.InterruptActiveRun(p.Key)

	runID := p.IdempotencyKey
	if runID == "" {
		runID = shortid.New("run")
	}

	return h.startAsyncRun(req.ID, RunParams{
		SessionKey:  p.Key,
		Message:     sanitizeInput(p.Message),
		Attachments: p.Attachments,
		ClientRunID: runID,
	}, false)
}

// SessionsSteer handles "sessions.steer" — patches session config, then starts a run.
func (h *Handler) SessionsSteer(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
	var p struct {
		Key          string `json:"key"`
		Message      string `json:"message,omitempty"`
		Thinking     string `json:"thinking,omitempty"`
		Model        string `json:"model,omitempty"`
		SystemPrompt string `json:"systemPrompt,omitempty"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrInvalidRequest, "invalid sessions.steer params: "+err.Error()))
	}
	if p.Key == "" {
		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrMissingParam, "key is required"))
	}

	// Interrupt any active run + concurrent response for this session.
	h.CancelConcurrentResponse(p.Key)
	h.InterruptActiveRun(p.Key)

	runID := shortid.New("steer")

	return h.startAsyncRun(req.ID, RunParams{
		SessionKey:  p.Key,
		Message:     sanitizeInput(p.Message),
		Model:       p.Model,
		System:      p.SystemPrompt,
		ClientRunID: runID,
	}, true)
}

// SessionsAbort handles "sessions.abort" — cancels an active run by key or runId.
func (h *Handler) SessionsAbort(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
	var p struct {
		Key   string `json:"key"`
		RunID string `json:"runId,omitempty"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrInvalidRequest, "invalid sessions.abort params: "+err.Error()))
	}
	if p.Key == "" && p.RunID == "" {
		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrMissingParam, "key or runId is required"))
	}

	h.abortMu.Lock()
	var abortedRunID string
	var sessionKey string
	if p.RunID != "" {
		if entry, ok := h.abortMap[p.RunID]; ok {
			entry.CancelFn()
			abortedRunID = p.RunID
			sessionKey = entry.SessionKey
			delete(h.abortMap, p.RunID)
		}
	} else {
		sessionKey = p.Key
		for id, entry := range h.abortMap {
			if entry.SessionKey == p.Key {
				entry.CancelFn()
				abortedRunID = id
				delete(h.abortMap, id)
			}
		}
	}
	h.abortMu.Unlock()

	// Cancel any concurrent response for the resolved session.
	if sessionKey != "" {
		h.CancelConcurrentResponse(sessionKey)
	}

	if abortedRunID == "" {
		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
			"ok":     true,
			"status": "no-active-run",
		})
		return resp
	}

	// Transition session out of running state.
	if sessionKey != "" {
		h.sessions.ApplyLifecycleEvent(sessionKey, session.LifecycleEvent{
			Phase: session.PhaseEnd,
			Ts:    time.Now().UnixMilli(),
		})
	}

	resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
		"ok":           true,
		"abortedRunId": abortedRunID,
		"status":       "aborted",
	})
	return resp
}

// startAsyncRun is the shared logic for Send/SessionsSend/SessionsSteer.
// It validates the session, creates abort context, and spawns the agent goroutine.
func (h *Handler) startAsyncRun(reqID string, params RunParams, isSteer bool) *protocol.ResponseFrame {
	// Ensure session exists.
	sess := h.sessions.Get(params.SessionKey)
	if sess == nil {
		sess = h.sessions.Create(params.SessionKey, session.KindDirect)
	}

	// Transition session to running.
	h.sessions.ApplyLifecycleEvent(params.SessionKey, session.LifecycleEvent{
		Phase: session.PhaseStart,
		Ts:    time.Now().UnixMilli(),
	})

	// Create a background context (not tied to the RPC request lifetime).
	runCtx, runCancel := context.WithCancel(context.Background())

	if params.ClientRunID != "" {
		h.abortMu.Lock()
		h.abortMap[params.ClientRunID] = &AbortEntry{
			SessionKey: params.SessionKey,
			ClientRun:  params.ClientRunID,
			CancelFn:   runCancel,
			ExpiresAt:  time.Now().Add(30 * time.Minute),
		}
		h.abortMu.Unlock()
	}

	// Broadcast session start event.
	if h.broadcast != nil {
		reason := "message_sent"
		if isSteer {
			reason = "steered"
		}
		h.broadcast("sessions.changed", map[string]any{
			"sessionKey": params.SessionKey,
			"reason":     reason,
			"status":     "running",
		})
	}

	// Dual-core: register task progress tracker so concurrent responses
	// can see what this task is doing.
	tp := NewTaskProgress(params.SessionKey, params.ClientRunID, params.Message)
	h.setTaskProgress(params.SessionKey, tp)

	// Spawn async agent run with panic recovery.
	deps := h.buildRunDeps()
	go func() {
		defer runCancel()
		defer h.cleanupAbort(params.ClientRunID)
		defer h.clearTaskProgressIfOwner(params.SessionKey, tp)
		defer func() {
			if r := recover(); r != nil {
				h.logger.Error("panic in agent run",
					"session", params.SessionKey,
					"runId", params.ClientRunID,
					"panic", r,
				)
				// Ensure session transitions out of running state.
				h.sessions.ApplyLifecycleEvent(params.SessionKey, session.LifecycleEvent{
					Phase: session.PhaseError,
					Ts:    time.Now().UnixMilli(),
				})
				if h.broadcast != nil {
					h.broadcast("sessions.changed", map[string]any{
						"sessionKey": params.SessionKey,
						"reason":     "panic",
						"status":     "failed",
					})
				}
			}
		}()
		runAgentAsync(runCtx, params, deps, tp)
	}()

	// Immediately return with runId.
	resp, _ := protocol.NewResponseOK(reqID, map[string]any{
		"runId":  params.ClientRunID,
		"status": "started",
	})
	return resp
}

// InterruptActiveRun cancels all active runs for a session key.
func (h *Handler) InterruptActiveRun(sessionKey string) {
	h.abortMu.Lock()
	var toDelete []string
	for id, entry := range h.abortMap {
		if entry.SessionKey == sessionKey {
			entry.CancelFn()
			toDelete = append(toDelete, id)
		}
	}
	for _, id := range toDelete {
		delete(h.abortMap, id)
	}
	h.abortMu.Unlock()
}

// getTaskProgress returns the live task progress for a session, or nil if no task is running.
func (h *Handler) getTaskProgress(sessionKey string) *TaskProgress {
	h.taskProgressMu.RLock()
	defer h.taskProgressMu.RUnlock()
	return h.taskProgress[sessionKey]
}

// setTaskProgress registers a task progress tracker for a session.
func (h *Handler) setTaskProgress(sessionKey string, tp *TaskProgress) {
	h.taskProgressMu.Lock()
	defer h.taskProgressMu.Unlock()
	h.taskProgress[sessionKey] = tp
}

// clearTaskProgressIfOwner removes the task progress tracker only if the
// caller's tp is still the active one. This prevents a race where goroutine G1
// (from a cancelled run) clears G2's (the new run's) task progress.
func (h *Handler) clearTaskProgressIfOwner(sessionKey string, tp *TaskProgress) {
	h.taskProgressMu.Lock()
	defer h.taskProgressMu.Unlock()
	if h.taskProgress[sessionKey] == tp {
		delete(h.taskProgress, sessionKey)
	}
}

// CancelConcurrentResponse cancels any active concurrent response for a session.
// Exported so that external abort paths (e.g., Propus StopGeneration) can clean up.
func (h *Handler) CancelConcurrentResponse(sessionKey string) {
	h.concRespMu.Lock()
	defer h.concRespMu.Unlock()
	if cancel, ok := h.concRespCancel[sessionKey]; ok {
		cancel()
		delete(h.concRespCancel, sessionKey)
	}
}

// startConcurrentResponse launches a parallel response run that shares the same
// identity and conversation history as the running task core. The task core
// continues uninterrupted. This is the dual-core "multitasking" path.
//
// Only one concurrent response per session at a time — a new message cancels
// any in-flight concurrent response before starting a fresh one.
func (h *Handler) startConcurrentResponse(reqID string, params RunParams, progress *TaskProgress) *protocol.ResponseFrame {
	if params.ClientRunID == "" {
		params.ClientRunID = shortid.New("conc")
	}

	// Cancel any previous concurrent response for this session.
	h.CancelConcurrentResponse(params.SessionKey)

	// Create a cancellable context for this concurrent response.
	ctx, cancel := context.WithCancel(context.Background())
	h.concRespMu.Lock()
	h.concRespCancel[params.SessionKey] = cancel
	h.concRespMu.Unlock()

	deps := h.buildRunDeps()
	go func() {
		defer cancel()
		defer func() {
			// Clean up cancel entry on completion.
			h.concRespMu.Lock()
			if h.concRespCancel[params.SessionKey] == cancel {
				delete(h.concRespCancel, params.SessionKey)
			}
			h.concRespMu.Unlock()
		}()
		defer func() {
			if r := recover(); r != nil {
				h.logger.Error("panic in concurrent response",
					"session", params.SessionKey,
					"runId", params.ClientRunID,
					"panic", r,
				)
			}
		}()
		runConcurrentResponse(ctx, params, deps, progress)
	}()

	resp, _ := protocol.NewResponseOK(reqID, map[string]any{
		"runId":  params.ClientRunID,
		"status": "started",
		"mode":   "concurrent_response",
	})
	return resp
}

// handleSlashCommand processes a recognized slash command and returns a response.
// This runs synchronously (no agent loop) and delivers a reply to the channel.
func (h *Handler) handleSlashCommand(
	reqID string,
	sessionKey string,
	delivery *DeliveryContext,
	cmd *SlashResult,
) *protocol.ResponseFrame {
	switch cmd.Command {
	case "reset":
		// Abort any active run, concurrent response, and clear transcript.
		h.CancelConcurrentResponse(sessionKey)
		h.InterruptActiveRun(sessionKey)
		if h.transcript != nil {
			if err := h.transcript.Delete(sessionKey); err != nil {
				h.logger.Warn("failed to delete transcript on reset", "error", err)
			}
		}
		h.sessions.ApplyLifecycleEvent(sessionKey, session.LifecycleEvent{
			Phase: session.PhaseEnd,
			Ts:    time.Now().UnixMilli(),
		})
		h.deliverSlashResponse(delivery, "세션이 초기화되었습니다.")

	case "kill":
		h.CancelConcurrentResponse(sessionKey)
		h.InterruptActiveRun(sessionKey)
		h.sessions.ApplyLifecycleEvent(sessionKey, session.LifecycleEvent{
			Phase: session.PhaseEnd,
			Ts:    time.Now().UnixMilli(),
		})
		h.deliverSlashResponse(delivery, "실행이 중단되었습니다.")

	case "status":
		status := h.buildSessionStatus(sessionKey)
		h.deliverSlashResponse(delivery, status)

	case "model":
		if cmd.Args != "" {
			h.defaultModel = cmd.Args
			h.deliverSlashResponse(delivery, fmt.Sprintf("모델이 %q(으)로 변경되었습니다.", cmd.Args))
		}

	case "think":
		h.deliverSlashResponse(delivery, "사고 모드가 토글되었습니다.")
	}

	return protocol.MustResponseOK(reqID, map[string]any{
		"command": cmd.Command,
		"handled": true,
	})
}

// deliverSlashResponse sends a slash command response back to the originating channel.
func (h *Handler) deliverSlashResponse(delivery *DeliveryContext, text string) {
	if h.replyFunc == nil || delivery == nil || text == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := h.replyFunc(ctx, delivery, text); err != nil {
		h.logger.Warn("slash command reply failed", "error", err)
	}
}

// buildSessionStatus constructs a human-readable session status string.
func (h *Handler) buildSessionStatus(sessionKey string) string {
	sess := h.sessions.Get(sessionKey)
	if sess == nil {
		return fmt.Sprintf("세션 %q: 정보 없음", sessionKey)
	}
	model := h.defaultModel
	if model == "" {
		model = defaultModel
	}
	return fmt.Sprintf("세션: %s\n모델: %s\n상태: %s",
		sessionKey, model, string(sess.Status))
}

// buildRunDeps assembles the dependency struct for runAgentAsync.
func (h *Handler) buildRunDeps() runDeps {
	return runDeps{
		sessions:        h.sessions,
		llmClient:       h.llmClient,
		transcript:      h.transcript,
		tools:           h.tools,
		authManager:     h.authManager,
		broadcast:       h.broadcast,
		broadcastRaw:    h.broadcastRaw,
		jobTracker:      h.jobTracker,
		replyFunc:       h.replyFunc,
		mediaSendFn:     h.mediaSendFn,
		typingFn:        h.typingFn,
		reactionFn:      h.reactionFn,
		providerConfigs: h.providerConfigs,
		logger:          h.logger,
		auroraStore:     h.auroraStore,
		vegaBackend:     h.vegaBackend,
		memoryStore:     h.memoryStore,
		memoryEmbedder:  h.memoryEmbedder,
		dreamTurnFn:     h.dreamTurnFn,
		agentLog:        h.agentLog,
		contextCfg:      h.contextCfg,
		compactionCfg:   h.compactionCfg,
		defaultModel:    h.defaultModel,
		defaultSystem:   h.defaultSystem,
		maxTokens:       h.maxTokens,
	}
}

// History handles "chat.history" — returns capped, sanitized transcript.
func (h *Handler) History(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
	var p struct {
		SessionKey string `json:"sessionKey"`
		Limit      int    `json:"limit,omitempty"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrInvalidRequest, "invalid chat.history params"))
	}
	if p.SessionKey == "" {
		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrMissingParam, "sessionKey is required"))
	}

	limit := p.Limit
	if limit <= 0 || limit > h.maxHistoryCount {
		limit = h.maxHistoryCount
	}

	// Use native transcript store when available.
	if h.transcript != nil {
		msgs, total, err := h.transcript.Load(p.SessionKey, limit)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, "transcript load error: "+err.Error()))
		}
		if msgs == nil {
			msgs = []ChatMessage{}
		}
		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
			"messages": msgs,
			"total":    total,
		})
		return resp
	}

	// No transcript store available — return empty history.
	resp := protocol.MustResponseOK(req.ID, map[string]any{
		"messages": []ChatMessage{},
		"total":    0,
	})
	return resp
}

// Abort handles "chat.abort" — cancels a running chat session.
func (h *Handler) Abort(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
	var p struct {
		ClientRunID string `json:"clientRunId"`
		SessionKey  string `json:"sessionKey"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrInvalidRequest, "invalid chat.abort params"))
	}
	if p.ClientRunID == "" && p.SessionKey == "" {
		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrMissingParam, "clientRunId or sessionKey is required"))
	}

	h.abortMu.Lock()
	var found bool
	var resolvedKey string
	if p.ClientRunID != "" {
		if entry, ok := h.abortMap[p.ClientRunID]; ok {
			resolvedKey = entry.SessionKey
			entry.CancelFn()
			delete(h.abortMap, p.ClientRunID)
			found = true
		}
	} else {
		resolvedKey = p.SessionKey
		for id, entry := range h.abortMap {
			if entry.SessionKey == p.SessionKey {
				entry.CancelFn()
				delete(h.abortMap, id)
				found = true
			}
		}
	}
	h.abortMu.Unlock()

	if !found {
		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrNotFound, "no active run found"))
	}

	// Cancel any concurrent response for the resolved session.
	if resolvedKey != "" {
		h.CancelConcurrentResponse(resolvedKey)
	} else if p.SessionKey != "" {
		h.CancelConcurrentResponse(p.SessionKey)
	}

	// Transition session to killed.
	key := resolvedKey
	if key == "" {
		key = p.SessionKey
	}
	if key != "" {
		h.sessions.ApplyLifecycleEvent(key, session.LifecycleEvent{
			Phase: session.PhaseEnd,
			Ts:    time.Now().UnixMilli(),
		})
		if h.broadcast != nil {
			h.broadcast("sessions.changed", map[string]any{
				"sessionKey": key,
				"reason":     "aborted",
				"status":     "killed",
			})
		}
	}

	resp := protocol.MustResponseOK(req.ID, map[string]bool{"aborted": true})
	return resp
}

// Inject handles "chat.inject" — injects a message directly into the transcript.
func (h *Handler) Inject(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
	var p struct {
		SessionKey string `json:"sessionKey"`
		Role       string `json:"role"`
		Content    string `json:"content"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrInvalidRequest, "invalid chat.inject params"))
	}
	if p.SessionKey == "" || p.Content == "" {
		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrMissingParam, "sessionKey and content are required"))
	}
	if p.Role == "" {
		p.Role = "assistant"
	}

	content := sanitizeInput(p.Content)

	// Use native transcript store if available.
	if h.transcript != nil {
		msg := ChatMessage{
			Role:      p.Role,
			Content:   content,
			Timestamp: time.Now().UnixMilli(),
		}
		if err := h.transcript.Append(p.SessionKey, msg); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrDependencyFailed, "transcript write error: "+err.Error()))
		}
		if h.broadcast != nil {
			h.broadcast("sessions.changed", map[string]any{
				"sessionKey": p.SessionKey,
				"reason":     "injected",
			})
		}
		resp, _ := protocol.NewResponseOK(req.ID, map[string]bool{"injected": true})
		return resp
	}

	// No transcript store available.
	return protocol.NewResponseError(req.ID, protocol.NewError(
		protocol.ErrDependencyFailed, "no transcript store available"))
}

// --- Helpers ---

func (h *Handler) cleanupAbort(clientRunID string) {
	if clientRunID == "" {
		return
	}
	h.abortMu.Lock()
	delete(h.abortMap, clientRunID)
	h.abortMu.Unlock()
}

// abortGCLoop periodically cleans up expired abort entries.
// Exits when h.done is closed (via Close()).
func (h *Handler) abortGCLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-h.done:
			return
		case <-ticker.C:
			h.abortMu.Lock()
			now := time.Now()
			for id, entry := range h.abortMap {
				if now.After(entry.ExpiresAt) {
					entry.CancelFn()
					delete(h.abortMap, id)
				}
			}
			h.abortMu.Unlock()
		}
	}
}

// budgetHistory truncates a history payload to fit within maxHistoryBytes.
func (h *Handler) budgetHistory(reqID string, payload json.RawMessage) *protocol.ResponseFrame {
	var parsed struct {
		Messages []json.RawMessage `json:"messages"`
		Total    int               `json:"total"`
	}
	if err := json.Unmarshal(payload, &parsed); err != nil {
		resp := protocol.MustResponseOK(reqID, map[string]any{
			"messages":  []any{},
			"total":     0,
			"truncated": true,
			"error":     "failed to parse history for budgeting",
		})
		return resp
	}

	// Keep messages from the end (most recent) until budget exhausted.
	// Collect in reverse, then flip to preserve chronological order.
	reversed := make([]json.RawMessage, 0, len(parsed.Messages))
	totalBytes := 0
	truncatedCount := 0
	for i := len(parsed.Messages) - 1; i >= 0; i-- {
		msgBytes := len(parsed.Messages[i])
		if msgBytes > h.maxMessageBytes {
			placeholder, _ := json.Marshal(map[string]any{
				"role":      "system",
				"content":   fmt.Sprintf("[message truncated: %d bytes]", msgBytes),
				"truncated": true,
			})
			msgBytes = len(placeholder)
			parsed.Messages[i] = placeholder
			truncatedCount++
		}
		if totalBytes+msgBytes > h.maxHistoryBytes {
			break
		}
		reversed = append(reversed, parsed.Messages[i])
		totalBytes += msgBytes
	}
	// Reverse to restore chronological order.
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}

	resp := protocol.MustResponseOK(reqID, map[string]any{
		"messages":       reversed,
		"total":          parsed.Total,
		"truncatedCount": truncatedCount,
		"budgeted":       true,
	})
	return resp
}

// sanitizeInput normalizes input text: NFC normalization approximation,
// strips control chars (except tab/newline/CR), and trims whitespace.
func sanitizeInput(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			i += size
			continue
		}
		// Allow tab, newline, carriage return.
		if r == '\t' || r == '\n' || r == '\r' {
			b.WriteRune(r)
			i += size
			continue
		}
		// Strip other control characters.
		if unicode.IsControl(r) {
			i += size
			continue
		}
		b.WriteRune(r)
		i += size
	}
	return strings.TrimSpace(b.String())
}

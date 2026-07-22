package localai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/tokenest"
)

// Errors returned by Hub.Submit.
var (
	ErrQueueFull       = errors.New("localai hub: queue full, request dropped")
	ErrHubShutdown     = errors.New("localai hub: shutting down")
	ErrUnhealthy       = errors.New("localai hub: model unhealthy")
	ErrRequestTooLarge = errors.New("localai hub: request exceeds token budget")
)

// mergeRequestBody builds the OpenAI-compatible ExtraBody for a hub request.
//
// The thinking-off decision is modelrole.ThinkingOffDirectiveFor's three-way:
// dual-mode models get their template toggle (deepseek-v4 →
// chat_template_kwargs.thinking=false — the previous non-reasoning branch
// sent the Qwen enable_thinking spelling, which dsv4 templates silently
// ignore, leaving thinking ON for every hub call), untoggleable reasoning
// models get nothing (a thinking-only template 400s on the kwarg), and
// vLLM-backed non-reasoning models keep NoThinking. reg (nil-safe) upgrades
// the resolution to the routing profile so deneb.json routing.toggleKwarg
// overrides shape hub calls too. callerExtra merges last so explicit fields
// win.
func mergeRequestBody(reg *modelrole.Registry, providerID, model string, callerExtra map[string]any) map[string]any {
	directive := reg.ThinkingOffDirectiveFor(providerID, model)
	merged := make(map[string]any, 1+len(callerExtra))
	if directive != nil {
		merged["chat_template_kwargs"] = map[string]any{directive.TemplateKwarg(): false}
	}
	for k, v := range callerExtra {
		merged[k] = v
	}
	return merged
}

// modelSamplingDefaults returns vendor-recommended sampling parameters for the
// model, sourced from its Profile (modelrole.ProfileFor) so the hub and
// reasoning detection share one model table. Returns nil pointers for models
// with no published override (use server defaults).
func modelSamplingDefaults(model string) (temp, topP *float64, topK *int) {
	p := modelrole.ProfileFor(model)
	return p.Temperature, p.TopP, p.TopK
}

// Config controls hub behavior.
type Config struct {
	// TokenBudget is the max estimated tokens across all in-flight requests.
	// Default: 65536.
	TokenBudget int64

	// CriticalOverdraw allows Critical-priority requests to exceed the budget
	// by this fraction. Default: 0.25 (25%).
	CriticalOverdraw float64

	// MaxQueueDepth caps the number of queued requests. Background requests
	// are dropped (oldest first) when exceeded. Default: 20.
	MaxQueueDepth int

	// CacheTTL is the default response cache TTL. Default: 5 minutes.
	CacheTTL time.Duration

	// CacheMaxEntries caps cached responses. Default: 200.
	CacheMaxEntries int
}

func (c *Config) withDefaults() Config {
	out := *c
	if out.TokenBudget <= 0 {
		out.TokenBudget = 65_536
	}
	if out.CriticalOverdraw <= 0 {
		out.CriticalOverdraw = 0.25
	}
	if out.MaxQueueDepth <= 0 {
		out.MaxQueueDepth = 20
	}
	if out.CacheTTL <= 0 {
		out.CacheTTL = defaultCacheTTL
	}
	if out.CacheMaxEntries <= 0 {
		out.CacheMaxEntries = defaultCacheMaxEntries
	}
	return out
}

// Hub is the centralized gateway for all local AI LLM requests.
type Hub struct {
	client     *llm.Client
	model      string
	providerID string // lightweight role's provider — gates the thinking template toggle
	baseURL    string
	apiKey     string
	registry   *modelrole.Registry

	// Vendor-recommended sampling defaults, resolved once at startup.
	defaultTemp *float64
	defaultTopP *float64
	defaultTopK *int

	cfg Config

	// Token budget admission control.
	budget *tokenBudgetLimiter

	// Priority queue.
	queue *requestQueue

	// Response cache.
	cache *responseCache

	// Health state.
	healthy         atomic.Bool
	lastHealthCheck atomic.Int64

	// Active request tracking for cancellation.
	activeReqs sync.Map // requestID (string) → *activeRequest

	// Optional observer for RL trajectory collection.
	// Called after each successful request completion with the request/response.
	observer func(Request, Response, error)

	// Metrics.
	Stats *HubStats

	// Lifecycle.
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	logger *slog.Logger

	reqIDCounter atomic.Int64
}

type activeRequest struct {
	id     string
	cancel context.CancelFunc
	tokens int64
}

// New creates and starts a Hub. Call Shutdown() to stop background goroutines.
func New(cfg Config, registry *modelrole.Registry, logger *slog.Logger) *Hub {
	cfg = cfg.withDefaults()
	ctx, cancel := context.WithCancel(context.Background())

	h := &Hub{
		registry: registry,
		cfg:      cfg,
		budget:   newTokenBudgetLimiter(cfg.TokenBudget, cfg.CriticalOverdraw),
		queue:    newRequestQueue(),
		cache:    newResponseCache(cfg.CacheTTL, cfg.CacheMaxEntries),
		Stats:    &HubStats{},
		ctx:      ctx,
		cancel:   cancel,
		logger:   logger,
	}
	// Resolve client and model from registry.
	if registry != nil {
		h.client = registry.Client(modelrole.RoleLightweight)
		h.model = registry.Model(modelrole.RoleLightweight)
		h.providerID = registry.Config(modelrole.RoleLightweight).ProviderID
		h.baseURL = registry.BaseURL(modelrole.RoleLightweight)
		h.apiKey = registry.APIKey(modelrole.RoleLightweight)
	}

	// Resolve vendor-recommended sampling defaults once at startup.
	h.defaultTemp, h.defaultTopP, h.defaultTopK = modelSamplingDefaults(h.model)

	// Start background goroutines.
	h.wg.Add(2)
	go h.dispatchLoop()
	go (&healthChecker{hub: h, baseURL: h.baseURL, apiKey: h.apiKey, started: time.Now()}).run()

	// Cache janitor.
	h.wg.Add(1)
	go h.cacheJanitor()

	return h
}

// Shutdown stops the hub, draining all queued requests.
func (h *Hub) Shutdown() {
	h.cancel()
	h.budget.close()
	h.queue.Close()
	h.queue.DrainAll(ErrHubShutdown)
	h.wg.Wait()
}

// IsHealthy returns the cached health status of the local AI server.
func (h *Hub) IsHealthy() bool {
	return h.healthy.Load()
}

// Model returns the configured lightweight model name.
func (h *Hub) Model() string { return h.model }

// Client returns the underlying LLM client (for callers that need streaming).
func (h *Hub) Client() *llm.Client { return h.client }

// Submit sends a request through the hub and blocks until completion.
// The request goes through cache check → priority queue → token budget
// admission → local AI dispatch.
func (h *Hub) Submit(ctx context.Context, req Request) (Response, error) {
	start := time.Now()
	h.Stats.Submitted.Add(1)
	if h.ctx.Err() != nil {
		return Response{}, ErrHubShutdown
	}
	if err := ctx.Err(); err != nil {
		h.Stats.Cancelled.Add(1)
		return Response{}, err
	}

	// Estimate input tokens if not provided.
	if req.EstInputTokens <= 0 {
		req.EstInputTokens = estimateInputTokens(&req)
	}

	// Cache check.
	if !req.NoCache {
		ttl := req.CacheTTL
		if ttl == 0 {
			ttl = h.cfg.CacheTTL
		}
		if ttl > 0 {
			if text, ok := h.cache.Get(&req, ttl); ok {
				h.Stats.CacheHits.Add(1)
				return Response{Text: text, FromCache: true, Duration: time.Since(start)}, nil
			}
		}
		h.Stats.CacheMisses.Add(1)
	}

	// Health gate: reject immediately if local AI is down and this isn't
	// a critical request that might benefit from fallback.
	if !h.healthy.Load() && req.Priority != PriorityCritical {
		h.Stats.Failed.Add(1)
		return Response{}, ErrUnhealthy
	}

	// Enqueue. resultCh is send-only in the entry; keep a local bidirectional
	// reference for the caller to receive on.
	ch := make(chan submitResult, 1)
	entry := &queueEntry{
		req:        &req,
		callerCtx:  ctx,
		resultCh:   ch,
		enqueuedAt: time.Now(),
	}
	if !h.queue.Push(entry) {
		return Response{}, ErrHubShutdown
	}

	// Drop oldest background if over depth.
	if h.queue.Len() > h.cfg.MaxQueueDepth {
		if h.queue.DropOldestBackground(h.cfg.MaxQueueDepth) {
			h.Stats.Dropped.Add(1)
		}
	}

	// Wait for result or caller cancellation.
	select {
	case <-ctx.Done():
		h.Stats.Cancelled.Add(1)
		return Response{}, ctx.Err()
	case res := <-ch:
		if err := ctx.Err(); err != nil {
			h.Stats.Cancelled.Add(1)
			return Response{}, err
		}
		if res.err != nil {
			return Response{}, res.err
		}
		res.resp.Duration = time.Since(start)
		return res.resp, nil
	}
}

// CallLocalLLM is a backward-compatible wrapper matching pilot.CallLocalLLM's
// signature. Callers that don't need full Request control use this.
func (h *Hub) CallLocalLLM(ctx context.Context, system, userMessage string, maxTokens int, extraBody ...rawJSON) (string, error) {
	resp, err := h.CallLocalLLMDetailed(ctx, system, userMessage, maxTokens, extraBody...)
	return resp.Text, err
}

// CallLocalLLMDetailed is CallLocalLLM but returns the full Response (text +
// token usage + the model that answered), so callers can record per-call usage
// for local models that never run a full agent turn.
func (h *Hub) CallLocalLLMDetailed(ctx context.Context, system, userMessage string, maxTokens int, extraBody ...rawJSON) (Response, error) {
	req := SimpleRequest(system, userMessage, maxTokens, PriorityCritical, "calllocal")
	if len(extraBody) > 0 && len(extraBody[0]) > 0 {
		var fields jsonObject
		if err := json.Unmarshal(extraBody[0], &fields); err != nil {
			return Response{}, fmt.Errorf("decode localai extra body: %w", err)
		}
		req.ExtraBody = fields
	}
	resp, err := h.Submit(ctx, req)
	if err == nil {
		return resp, nil
	}
	// Admission/health failures may use another configured model, but shutdown
	// and caller cancellation are terminal for this work item. Starting a direct
	// fallback after either would recreate the zombie work the hub just stopped.
	if h.registry == nil || errors.Is(err, ErrHubShutdown) || ctx.Err() != nil {
		return Response{}, err
	}

	// Fallback chain: try other model roles. Non-reasoning models are tried
	// first — a reasoning model's separate thinking channel makes it a slower,
	// less reliable summarizer — and reasoning models are kept only as a last
	// resort so a fully-reasoning chain still produces output.
	chain := h.registry.FallbackChain(modelrole.RoleLightweight)
	var deferredReasoning []modelrole.Role
	for _, role := range chain[1:] {
		if h.registry.RoleIsReasoning(role) {
			deferredReasoning = append(deferredReasoning, role)
			continue
		}
		if resp, ok := h.callFallbackRole(ctx, role, system, userMessage, maxTokens, extraBody...); ok {
			return resp, nil
		}
	}
	for _, role := range deferredReasoning {
		if resp, ok := h.callFallbackRole(ctx, role, system, userMessage, maxTokens, extraBody...); ok {
			return resp, nil
		}
	}
	return Response{}, err
}

// callFallbackRole runs callDirect for a single fallback role. It returns
// (text, true) on success and ("", false) when the role is unconfigured
// (nil client) or the call failed.
func (h *Hub) callFallbackRole(ctx context.Context, role modelrole.Role, system, userMessage string, maxTokens int, extraBody ...rawJSON) (Response, bool) {
	client := h.registry.Client(role)
	if client == nil {
		return Response{}, false
	}
	roleCfg := h.registry.Config(role)
	text, usage, err := h.callDirect(ctx, client, roleCfg.ProviderID, roleCfg.Model, system, userMessage, maxTokens, extraBody...)
	if err != nil {
		h.logger.Debug("localai hub: fallback role failed",
			"role", role, "reasoning", h.registry.RoleIsReasoning(role), "error", err)
		return Response{}, false
	}
	return Response{Text: text, Usage: usage, Model: roleCfg.Model}, true
}

// --- dispatch loop ---

func (h *Hub) dispatchLoop() {
	defer h.wg.Done()
	for {
		entry := h.queue.PopWait(h.ctx.Done())
		if entry == nil {
			return // shutdown
		}
		estimatedTokens := int64(entry.req.EstInputTokens) + int64(entry.req.MaxTokens)

		// Wait for token budget.
		if err := h.budget.acquire(entry.callerCtx, estimatedTokens, entry.req.Priority); err != nil {
			if h.ctx.Err() != nil {
				err = ErrHubShutdown
			}
			if errors.Is(err, ErrRequestTooLarge) {
				h.Stats.Failed.Add(1)
			}
			entry.resultCh <- submitResult{err: err}
			continue
		}

		h.wg.Add(1)
		go func(e *queueEntry, tokens int64) {
			defer h.wg.Done()
			defer h.budget.release(tokens)
			h.executeRequest(e)
		}(entry, estimatedTokens)
	}
}

func (h *Hub) executeRequest(entry *queueEntry) {
	req := entry.req

	// Preserve the caller's values/deadline while also stopping the request when
	// the hub shuts down. This context reaches the outbound HTTP stream, so a
	// disconnected or timed-out caller cannot leave GPU work running behind it.
	reqCtx, reqCancel := linkedRequestContext(entry.callerCtx, h.ctx)
	defer reqCancel()

	// Track for cancellation.
	id := fmt.Sprintf("localai-%d", h.reqIDCounter.Add(1))
	ar := &activeRequest{id: id, cancel: reqCancel, tokens: int64(req.EstInputTokens) + int64(req.MaxTokens)}
	h.activeReqs.Store(id, ar)
	defer h.activeReqs.Delete(id)

	// Build the LLM request. Reasoning models omit the enable_thinking flag
	// (see mergeRequestBody).
	merged := mergeRequestBody(h.registry, h.providerID, h.model, req.ExtraBody)

	// Inject server-side timeout to prevent zombie generation.
	if deadline, ok := reqCtx.Deadline(); ok {
		remaining := time.Until(deadline).Seconds() - 2.0
		if remaining > 1 {
			merged["timeout"] = remaining
		}
	}

	chatReq := llm.ChatRequest{
		Model:          h.model,
		Messages:       req.Messages,
		System:         llm.SystemString(req.System),
		MaxTokens:      req.MaxTokens,
		Stream:         true,
		Temperature:    h.defaultTemp,
		TopP:           h.defaultTopP,
		TopK:           h.defaultTopK,
		ExtraBody:      llmExtraBody(merged),
		ResponseFormat: req.ResponseFormat,
	}

	if h.client == nil {
		h.Stats.Failed.Add(1)
		entry.resultCh <- submitResult{err: errors.New("localai hub: client not initialized")}
		return
	}

	events, err := h.client.StreamChat(reqCtx, chatReq)
	if err != nil {
		err = h.requestError(reqCtx, entry, err)
		h.recordExecutionFailure(err)
		h.logger.Debug("localai hub: stream failed",
			"caller", req.CallerTag, "error", err)
		entry.resultCh <- submitResult{err: fmt.Errorf("localai stream: %w", err)}
		return
	}

	// Collect response.
	text, usage, err := collectStream(reqCtx, events)
	if err == nil && reqCtx.Err() != nil {
		err = reqCtx.Err()
	}
	if err != nil {
		err = h.requestError(reqCtx, entry, err)
		h.recordExecutionFailure(err)
		entry.resultCh <- submitResult{err: err}
		return
	}

	h.Stats.Completed.Add(1)

	// Notify RL observer (trajectory collection).
	if obs := h.observer; obs != nil {
		obs(*req, Response{Text: text, Usage: usage, Model: chatReq.Model}, nil)
	}

	// Cache the response.
	if !req.NoCache && text != "" {
		ttl := req.CacheTTL
		if ttl == 0 {
			ttl = h.cfg.CacheTTL
		}
		if ttl > 0 {
			h.cache.Put(req, text)
		}
	}

	entry.resultCh <- submitResult{resp: Response{Text: text, Usage: usage, Model: chatReq.Model}}
}

// linkedRequestContext keeps the caller context as the parent (preserving its
// values and deadline) and adds hub shutdown as a second cancellation source.
func linkedRequestContext(callerCtx, hubCtx context.Context) (context.Context, context.CancelFunc) {
	ctx, cancelCause := context.WithCancelCause(callerCtx)
	stopHubCancel := context.AfterFunc(hubCtx, func() { cancelCause(ErrHubShutdown) })
	return ctx, func() {
		stopHubCancel()
		cancelCause(context.Canceled)
	}
}

func (h *Hub) requestError(reqCtx context.Context, entry *queueEntry, err error) error {
	if errors.Is(context.Cause(reqCtx), ErrHubShutdown) || h.ctx.Err() != nil {
		return ErrHubShutdown
	}
	if entry.callerCtx != nil && entry.callerCtx.Err() != nil {
		return entry.callerCtx.Err()
	}
	return err
}

func (h *Hub) recordExecutionFailure(err error) {
	if errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, ErrHubShutdown) {
		return
	}
	h.Stats.Failed.Add(1)
}

// callDirect is a raw local AI call for fallback chains (bypasses queue/budget).
func (h *Hub) callDirect(ctx context.Context, client *llm.Client, providerID, model, system, userMessage string, maxTokens int, extraBody ...rawJSON) (string, llm.TokenUsage, error) {
	var callerExtra map[string]any
	if len(extraBody) > 0 && len(extraBody[0]) > 0 {
		if err := json.Unmarshal(extraBody[0], &callerExtra); err != nil {
			return "", llm.TokenUsage{}, fmt.Errorf("decode localai extra body: %w", err)
		}
	}
	merged := mergeRequestBody(h.registry, providerID, model, callerExtra)

	fbTemp, fbTopP, fbTopK := modelSamplingDefaults(model)
	req := llm.ChatRequest{
		Model:       model,
		Messages:    []llm.Message{llm.NewTextMessage("user", userMessage)},
		System:      llm.SystemString(system),
		MaxTokens:   maxTokens,
		Stream:      true,
		Temperature: fbTemp,
		TopP:        fbTopP,
		TopK:        fbTopK,
		ExtraBody:   llmExtraBody(merged),
	}

	events, err := client.StreamChat(ctx, req)
	if err != nil {
		return "", llm.TokenUsage{}, err
	}
	return collectStream(ctx, events)
}

// collectStream gathers the assistant text from a streaming response. Only
// text_delta blocks are collected — reasoning models additionally stream
// thinking_delta blocks (the translated reasoning_content channel) carrying
// private chain-of-thought, which must never leak into hub output such as
// compaction summaries.
func collectStream(ctx context.Context, events <-chan llm.StreamEvent) (string, llm.TokenUsage, error) {
	var usage llm.TokenUsage
	if events == nil {
		return "", usage, fmt.Errorf("localai: nil event channel")
	}
	var sb strings.Builder
	for {
		select {
		case <-ctx.Done():
			if sb.Len() > 0 {
				return strings.TrimSpace(sb.String()), usage, nil
			}
			return "", usage, ctx.Err()
		case ev, ok := <-events:
			if !ok {
				return strings.TrimSpace(sb.String()), usage, nil
			}
			switch ev.Type {
			case "content_block_delta":
				sb.WriteString(extractTextDelta(ev.Payload.Bytes()))
			case "message_start":
				var ms llm.MessageStart
				if json.Unmarshal(ev.Payload.Bytes(), &ms) == nil {
					usage.InputTokens = ms.Message.Usage.InputTokens
					if v := ms.Message.Usage.CacheReadInputTokens; v > 0 {
						usage.CacheReadInputTokens = v
					}
					if v := ms.Message.Usage.CacheCreationInputTokens; v > 0 {
						usage.CacheCreationInputTokens = v
					}
				}
			case "message_delta":
				var md llm.MessageDelta
				if json.Unmarshal(ev.Payload.Bytes(), &md) == nil {
					if v := md.Usage.OutputTokens; v > 0 {
						usage.OutputTokens = v
					}
					if v := md.Usage.CacheReadInputTokens; v > 0 {
						usage.CacheReadInputTokens = v
					}
					if v := md.Usage.CacheCreationInputTokens; v > 0 {
						usage.CacheCreationInputTokens = v
					}
				}
			}
		}
	}
}

// extractTextDelta returns the text of a content_block_delta payload, but only
// when the delta is a text_delta. thinking_delta and signature_delta payloads
// (emitted for reasoning models) yield "" so reasoning content is discarded.
func extractTextDelta(payload []byte) string {
	var cbd llm.ContentBlockDelta
	if err := json.Unmarshal(payload, &cbd); err != nil {
		return ""
	}
	if cbd.Delta.Type != "text_delta" {
		return ""
	}
	return cbd.Delta.Text
}

func (h *Hub) cacheJanitor() {
	defer h.wg.Done()
	for {
		select {
		case <-h.ctx.Done():
			return
		case <-time.After(cacheJanitorInterval):
			h.cache.Cleanup()
		}
	}
}

// --- token estimation ---

func estimateInputTokens(req *Request) int {
	// System prompt: full script-aware estimation.
	est := tokenest.Estimate(req.System)
	// Message content: byte-level heuristic (raw JSON bytes).
	for _, m := range req.Messages {
		est += tokenest.EstimateBytes(m.Content.Bytes())
	}
	if est < 1 {
		return 1
	}
	return est
}

func llmExtraBody(m map[string]any) map[string]llm.FlexibleJSON {
	if m == nil {
		return nil
	}
	out := make(map[string]llm.FlexibleJSON, len(m))
	for k, v := range m {
		out[k] = llm.FlexibleFromValue(v)
	}
	return out
}

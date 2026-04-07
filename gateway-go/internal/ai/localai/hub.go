package localai

import (
	"context"
	"errors"
	"fmt"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/tokenest"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Errors returned by Hub.Submit.
var (
	ErrQueueFull   = errors.New("localai hub: queue full, request dropped")
	ErrHubShutdown = errors.New("localai hub: shutting down")
	ErrUnhealthy   = errors.New("localai hub: model unhealthy")
)

// NoThinking is the default ExtraBody merged into every hub request.
// Previously disabled Qwen3.5 reasoning mode; currently empty (gemma4
// does not require extra template kwargs).
// Exported so pilot and memory packages can reference it without duplicating.
var NoThinking = map[string]any{}

// modelSamplingDefaults returns vendor-recommended sampling parameters for
// known local models. Returns nil pointers for unknown models (use server defaults).
// Sources:
//   - Gemma 4: Google model card (ai.google.dev/gemma/docs/core/model_card_4)
func modelSamplingDefaults(model string) (temp, topP *float64, topK *int) {
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "gemma-4") || strings.Contains(m, "gemma4"):
		return ptr(1.0), ptr(0.95), ptr(64)
	default:
		return nil, nil, nil
	}
}

func ptr[T any](v T) *T { return &v }

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
	client   *llm.Client
	model    string
	baseURL  string
	registry *modelrole.Registry

	// Vendor-recommended sampling defaults, resolved once at startup.
	defaultTemp *float64
	defaultTopP *float64
	defaultTopK *int

	cfg Config

	// Token budget admission control.
	inFlightTokens atomic.Int64
	budgetMu       sync.Mutex
	budgetCond     *sync.Cond

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
		queue:    newRequestQueue(),
		cache:    newResponseCache(cfg.CacheTTL, cfg.CacheMaxEntries),
		Stats:    &HubStats{},
		ctx:      ctx,
		cancel:   cancel,
		logger:   logger,
	}
	h.budgetCond = sync.NewCond(&h.budgetMu)

	// Resolve client and model from registry.
	if registry != nil {
		h.client = registry.Client(modelrole.RoleLightweight)
		h.model = registry.Model(modelrole.RoleLightweight)
		h.baseURL = registry.BaseURL(modelrole.RoleLightweight)
	}

	// Resolve vendor-recommended sampling defaults once at startup.
	h.defaultTemp, h.defaultTopP, h.defaultTopK = modelSamplingDefaults(h.model)

	// Start background goroutines.
	h.wg.Add(2)
	go h.dispatchLoop()
	go (&healthChecker{hub: h, baseURL: h.baseURL, started: time.Now()}).run()

	// Cache janitor.
	h.wg.Add(1)
	go h.cacheJanitor()

	return h
}

// Shutdown stops the hub, draining all queued requests.
func (h *Hub) Shutdown() {
	h.cancel()
	h.queue.Close()
	h.budgetCond.Broadcast()
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

// SetObserver registers a callback invoked after each completed request.
// Used by the RL collector to capture trajectories without modifying callers.
func (h *Hub) SetObserver(fn func(Request, Response, error)) { h.observer = fn }

// Submit sends a request through the hub and blocks until completion.
// The request goes through cache check → priority queue → token budget
// admission → local AI dispatch.
func (h *Hub) Submit(ctx context.Context, req Request) (Response, error) {
	start := time.Now()
	h.Stats.Submitted.Add(1)

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

	// Enqueue.
	entry := &queueEntry{
		req:        &req,
		resultCh:   make(chan submitResult, 1),
		enqueuedAt: time.Now(),
	}
	h.queue.Push(entry)

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
	case res := <-entry.resultCh:
		if res.err != nil {
			return Response{}, res.err
		}
		res.resp.Duration = time.Since(start)
		return res.resp, nil
	}
}

// CallLocalLLM is a backward-compatible wrapper matching pilot.CallLocalLLM's
// signature. Callers that don't need full Request control use this.
func (h *Hub) CallLocalLLM(ctx context.Context, system, userMessage string, maxTokens int, extraBody ...map[string]any) (string, error) {
	req := SimpleRequest(system, userMessage, maxTokens, PriorityCritical, "calllocal")
	if len(extraBody) > 0 && extraBody[0] != nil {
		req.ExtraBody = extraBody[0]
	}
	resp, err := h.Submit(ctx, req)
	if err != nil {
		// Fallback chain: try other model roles if local AI fails.
		if h.registry != nil {
			chain := h.registry.FallbackChain(modelrole.RoleLightweight)
			for _, role := range chain[1:] {
				fbClient := h.registry.Client(role)
				if fbClient == nil {
					continue
				}
				text, fbErr := h.callDirect(ctx, fbClient, h.registry.Model(role), system, userMessage, maxTokens, extraBody...)
				if fbErr == nil {
					return text, nil
				}
			}
		}
		return "", err
	}
	return resp.Text, nil
}

// --- dispatch loop ---

func (h *Hub) dispatchLoop() {
	defer h.wg.Done()
	for {
		entry := h.queue.PopWait(h.ctx.Done())
		if entry == nil {
			return // shutdown
		}
		estimatedTokens := int64(entry.req.EstInputTokens + entry.req.MaxTokens)

		// Wait for token budget.
		if !h.waitForBudget(estimatedTokens, entry.req.Priority) {
			entry.resultCh <- submitResult{err: ErrHubShutdown}
			continue
		}

		h.wg.Add(1)
		go func(e *queueEntry, tokens int64) {
			defer h.wg.Done()
			defer func() {
				h.inFlightTokens.Add(-tokens)
				h.budgetCond.Broadcast()
			}()
			h.executeRequest(e)
		}(entry, estimatedTokens)
	}
}

func (h *Hub) waitForBudget(tokens int64, priority Priority) bool {
	limit := h.cfg.TokenBudget
	if priority == PriorityCritical {
		limit = int64(float64(limit) * (1 + h.cfg.CriticalOverdraw))
	}

	h.budgetMu.Lock()
	defer h.budgetMu.Unlock()
	for h.inFlightTokens.Load()+tokens > limit {
		// Check shutdown.
		select {
		case <-h.ctx.Done():
			return false
		default:
		}
		h.budgetCond.Wait()
	}
	h.inFlightTokens.Add(tokens)
	return true
}

func (h *Hub) executeRequest(entry *queueEntry) {
	req := entry.req

	// Create a request-scoped context with the caller's timeout.
	reqCtx, reqCancel := context.WithCancel(h.ctx)
	defer reqCancel()

	// Track for cancellation.
	id := fmt.Sprintf("localai-%d", h.reqIDCounter.Add(1))
	ar := &activeRequest{id: id, cancel: reqCancel, tokens: int64(req.EstInputTokens + req.MaxTokens)}
	h.activeReqs.Store(id, ar)
	defer h.activeReqs.Delete(id)

	// Build the LLM request.
	merged := make(map[string]any, len(NoThinking)+len(req.ExtraBody))
	for k, v := range NoThinking {
		merged[k] = v
	}
	for k, v := range req.ExtraBody {
		merged[k] = v
	}

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
		ExtraBody:      merged,
		ResponseFormat: req.ResponseFormat,
	}

	if h.client == nil {
		h.Stats.Failed.Add(1)
		entry.resultCh <- submitResult{err: errors.New("localai hub: client not initialized")}
		return
	}

	events, err := h.client.StreamChat(reqCtx, chatReq)
	if err != nil {
		h.Stats.Failed.Add(1)
		h.logger.Debug("localai hub: stream failed",
			"caller", req.CallerTag, "error", err)
		entry.resultCh <- submitResult{err: fmt.Errorf("localai stream: %w", err)}
		return
	}

	// Collect response.
	text, err := collectStream(reqCtx, events)
	if err != nil {
		h.Stats.Failed.Add(1)
		entry.resultCh <- submitResult{err: err}
		return
	}

	h.Stats.Completed.Add(1)

	// Notify RL observer (trajectory collection).
	if obs := h.observer; obs != nil {
		obs(*req, Response{Text: text}, nil)
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

	entry.resultCh <- submitResult{resp: Response{Text: text}}
}

// callDirect is a raw local AI call for fallback chains (bypasses queue/budget).
func (h *Hub) callDirect(ctx context.Context, client *llm.Client, model, system, userMessage string, maxTokens int, extraBody ...map[string]any) (string, error) {
	merged := make(map[string]any, len(NoThinking))
	for k, v := range NoThinking {
		merged[k] = v
	}
	if len(extraBody) > 0 && extraBody[0] != nil {
		for k, v := range extraBody[0] {
			merged[k] = v
		}
	}

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
		ExtraBody:   merged,
	}

	events, err := client.StreamChat(ctx, req)
	if err != nil {
		return "", err
	}
	return collectStream(ctx, events)
}

// collectStream gathers all text deltas from a streaming response.
func collectStream(ctx context.Context, events <-chan llm.StreamEvent) (string, error) {
	if events == nil {
		return "", fmt.Errorf("localai: nil event channel")
	}
	var sb strings.Builder
	for {
		select {
		case <-ctx.Done():
			if sb.Len() > 0 {
				return strings.TrimSpace(sb.String()), nil
			}
			return "", ctx.Err()
		case ev, ok := <-events:
			if !ok {
				return strings.TrimSpace(sb.String()), nil
			}
			if ev.Type == "content_block_delta" {
				if text := extractDeltaText(ev.Payload); text != "" {
					sb.WriteString(text)
				}
			}
		}
	}
}

// extractDeltaText extracts text from {"delta":{"text":"..."}} payloads.
func extractDeltaText(payload []byte) string {
	// Fast path: scan for "text":" pattern.
	const needle = `"text":"`
	idx := strings.Index(string(payload), needle)
	if idx < 0 {
		return ""
	}
	start := idx + len(needle)
	end := start
	for end < len(payload) {
		if payload[end] == '\\' {
			end += 2
			continue
		}
		if payload[end] == '"' {
			break
		}
		end++
	}
	if end > len(payload) {
		end = len(payload)
	}
	return string(payload[start:end])
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
		est += tokenest.EstimateBytes([]byte(m.Content))
	}
	if est < 1 {
		return 1
	}
	return est
}

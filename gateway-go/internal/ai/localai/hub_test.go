package localai

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

func TestResponseCachePutGetRoundTrip(t *testing.T) {
	cache := newResponseCache(5*time.Minute, 100)

	req := SimpleRequest("sys", "hello", 100, PriorityNormal, "test")

	// Miss.
	if _, ok := cache.Get(&req, 0); ok {
		t.Fatal("expected cache miss")
	}

	// Put + hit.
	cache.Put(&req, "world")
	text, ok := cache.Get(&req, 0)
	if !ok || text != "world" {
		t.Fatalf("got ok=%v text=%q, want cache hit with 'world'", ok, text)
	}

	// Different request = miss.
	req2 := SimpleRequest("sys", "different", 100, PriorityNormal, "test")
	if _, ok := cache.Get(&req2, 0); ok {
		t.Fatal("expected cache miss for different request")
	}
}

func TestResponseCacheExpiredEntryMisses(t *testing.T) {
	cache := newResponseCache(10*time.Millisecond, 100)

	req := SimpleRequest("sys", "hello", 100, PriorityNormal, "test")
	cache.Put(&req, "world")

	time.Sleep(20 * time.Millisecond)
	if _, ok := cache.Get(&req, 0); ok {
		t.Fatal("expected expired cache entry to miss")
	}
}

func TestResponseCacheEviction(t *testing.T) {
	cache := newResponseCache(5*time.Minute, 3)

	for i := range 5 {
		req := SimpleRequest("sys", string(rune('a'+i)), 100, PriorityNormal, "test")
		cache.Put(&req, "val")
	}

	if cache.Len() > 3 {
		t.Fatalf("got %d, want max 3 entries", cache.Len())
	}
}

func TestPriorityQueueReturnsCriticalFirst(t *testing.T) {
	q := newRequestQueue()

	bg := &queueEntry{
		req:        &Request{Priority: PriorityBackground, CallerTag: "bg"},
		resultCh:   make(chan submitResult, 1),
		enqueuedAt: time.Now(),
	}
	crit := &queueEntry{
		req:        &Request{Priority: PriorityCritical, CallerTag: "crit"},
		resultCh:   make(chan submitResult, 1),
		enqueuedAt: time.Now().Add(time.Second), // enqueued later
	}

	q.Push(bg)
	q.Push(crit)

	// Critical should come out first despite being enqueued later.
	done := make(chan struct{})
	close(done) // non-blocking pop
	first := q.PopWait(done)
	if first == nil || first.req.CallerTag != "crit" {
		t.Fatalf("got %v, want critical first", first)
	}
}

func TestQueueDropOldestBackgroundEmitsError(t *testing.T) {
	q := newRequestQueue()

	// Add 3 entries: 1 normal + 2 background.
	normal := &queueEntry{
		req:        &Request{Priority: PriorityNormal, CallerTag: "normal"},
		resultCh:   make(chan submitResult, 1),
		enqueuedAt: time.Now(),
	}
	bg1Ch := make(chan submitResult, 1)
	bg1 := &queueEntry{
		req:        &Request{Priority: PriorityBackground, CallerTag: "bg1"},
		resultCh:   bg1Ch,
		enqueuedAt: time.Now(),
	}
	bg2 := &queueEntry{
		req:        &Request{Priority: PriorityBackground, CallerTag: "bg2"},
		resultCh:   make(chan submitResult, 1),
		enqueuedAt: time.Now().Add(time.Second),
	}
	q.Push(normal)
	q.Push(bg1)
	q.Push(bg2)

	// Drop with max depth 2. Should drop bg1 (oldest background).
	dropped := q.DropOldestBackground(2)
	if !dropped {
		t.Fatal("expected a drop")
	}

	// bg1's resultCh should have an error.
	select {
	case res := <-bg1Ch:
		if !errors.Is(res.err, ErrQueueFull) {
			t.Fatalf("got %v, want ErrQueueFull", res.err)
		}
	default:
		t.Fatal("bg1 should have received error")
	}

	if q.Len() != 2 {
		t.Fatalf("got %d, want 2 remaining", q.Len())
	}
}

func TestCacheKeyEncodesMaxTokens(t *testing.T) {
	r1 := SimpleRequest("sys", "hello", 100, PriorityNormal, "test")
	r2 := SimpleRequest("sys", "hello", 200, PriorityNormal, "test")

	k1 := cacheKey(&r1)
	k2 := cacheKey(&r2)
	if k1 == k2 {
		t.Fatal("different maxTokens should produce different cache keys")
	}
}

func TestMergeRequestBodyPreservesNoThinkingForNonReasoning(t *testing.T) {
	merged := mergeRequestBody(nil, "vllm", "gemma4", nil)
	ctk, ok := merged["chat_template_kwargs"].(map[string]any)
	if !ok {
		t.Fatalf("non-reasoning model: chat_template_kwargs missing, got %v", merged)
	}
	if got, exists := ctk["enable_thinking"]; !exists || got != false {
		t.Errorf("non-reasoning model: enable_thinking = %v, present=%v; want false and present", got, exists)
	}
}

func TestMergeRequestBodyReasoningModelWithoutChatTemplateKwargs(t *testing.T) {
	// A reasoning model must not receive enable_thinking — vLLM's
	// --reasoning-parser ignores it and a thinking-only chat template that
	// lacks the parameter rejects the request with a 400.
	merged := mergeRequestBody(nil, "vllm", "qwen3.6-35b-a3b", nil)
	if _, exists := merged["chat_template_kwargs"]; exists {
		t.Errorf("reasoning model: chat_template_kwargs must be omitted, got %v", merged)
	}
}

func TestMergeRequestBodyDualModeCreatesThinkingToggle(t *testing.T) {
	// deepseek-v4 is dual-mode: Profile.Reasoning=false by design, thinking
	// controlled ONLY via chat_template_kwargs.thinking. The old non-reasoning
	// branch sent the Qwen enable_thinking spelling, which dsv4 templates
	// silently ignore — thinking stayed on and ate small output budgets (the
	// 2026-07-02/03 wiki-dream failures). The toggle must now be attached.
	merged := mergeRequestBody(nil, "vllm", "deepseek-v4-flash", nil)
	ctk, ok := merged["chat_template_kwargs"].(map[string]any)
	if !ok {
		t.Fatalf("dsv4: chat_template_kwargs missing, got %v", merged)
	}
	if got, exists := ctk["thinking"]; !exists || got != false {
		t.Errorf("dsv4: thinking = %v, present=%v; want false and present", got, exists)
	}
	if _, has := ctk["enable_thinking"]; has {
		t.Errorf("dsv4: enable_thinking must not be sent (wrong spelling), got %v", ctk)
	}
}

func TestMergeRequestBodyPreservesCallerOverrides(t *testing.T) {
	caller := map[string]any{"timeout": 30.0, "chat_template_kwargs": "caller-value"}
	merged := mergeRequestBody(nil, "vllm", "qwen3.6-35b-a3b", caller)
	if merged["timeout"] != 30.0 {
		t.Errorf("caller timeout lost: got %v", merged["timeout"])
	}
	if merged["chat_template_kwargs"] != "caller-value" {
		t.Errorf("caller chat_template_kwargs not preserved: got %v", merged["chat_template_kwargs"])
	}
}

func TestExtractTextDeltaParsesDeltaPayloads(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    string
	}{
		{"text_delta", `{"index":1,"delta":{"type":"text_delta","text":"hello"}}`, "hello"},
		{"thinking_delta dropped", `{"index":0,"delta":{"type":"thinking_delta","text":"reasoning"}}`, ""},
		{"signature_delta dropped", `{"index":0,"delta":{"type":"signature_delta","text":"sig"}}`, ""},
		{"input_json_delta dropped", `{"index":2,"delta":{"type":"input_json_delta","partial_json":"{}"}}`, ""},
		{"malformed payload", `not json`, ""},
		{"empty text_delta", `{"index":1,"delta":{"type":"text_delta","text":""}}`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractTextDelta([]byte(tt.payload)); got != tt.want {
				t.Errorf("extractTextDelta(%s) = %q, want %q", tt.payload, got, tt.want)
			}
		})
	}
}

func TestCollectStream_DropsThinkingDeltas(t *testing.T) {
	mkDelta := func(typ, text string) llm.StreamEvent {
		var cbd llm.ContentBlockDelta
		cbd.Delta.Type = typ
		cbd.Delta.Text = text
		p, _ := json.Marshal(cbd)
		return llm.StreamEvent{Type: "content_block_delta", Payload: llm.FlexibleFromRaw(p)}
	}

	events := make(chan llm.StreamEvent, 8)
	events <- mkDelta("thinking_delta", "secret reasoning ")
	events <- mkDelta("text_delta", "real ")
	events <- mkDelta("thinking_delta", "more reasoning ")
	events <- mkDelta("text_delta", "answer")
	close(events)

	got, _, err := collectStream(context.Background(), events)
	if err != nil {
		t.Fatalf("collectStream error: %v", err)
	}
	if got != "real answer" {
		t.Errorf("collectStream = %q, want %q (reasoning content must be dropped)", got, "real answer")
	}
}

func TestSubmit_UnhealthyRejectsBackground(t *testing.T) {
	// Create a hub with no actual local AI server.
	cfg := Config{}
	h := &Hub{
		cfg:   cfg.withDefaults(),
		queue: newRequestQueue(),
		cache: newResponseCache(0, 0),
		Stats: &HubStats{},
	}
	ctx, cancel := context.WithCancel(context.Background())
	h.ctx = ctx
	h.cancel = cancel
	// healthy defaults to false.

	req := SimpleRequest("sys", "test", 100, PriorityBackground, "test")
	_, err := h.Submit(context.Background(), req)
	if !errors.Is(err, ErrUnhealthy) {
		t.Fatalf("got %v, want ErrUnhealthy for background on unhealthy hub", err)
	}

	h.cancel()
}

func TestSubmitRejectsOversizedRequestWithoutBlockingNext(t *testing.T) {
	h := newDispatchTestHub(t, Config{TokenBudget: 10}, nil)

	oversized := SimpleRequest("sys", "too large", 1, PriorityNormal, "test")
	oversized.EstInputTokens = 10 // total reservation 11 > the 10-token budget
	callCtx, callCancel := context.WithTimeout(context.Background(), time.Second)
	defer callCancel()
	if _, err := h.Submit(callCtx, oversized); !errors.Is(err, ErrRequestTooLarge) {
		t.Fatalf("oversized request error = %v, want ErrRequestTooLarge", err)
	}

	// The oversized request must not wedge the single dispatch loop. With no
	// client configured, a following admissible request reaches executeRequest
	// and returns its normal initialization error instead of timing out.
	small := SimpleRequest("sys", "small", 1, PriorityNormal, "test")
	small.EstInputTokens = 1
	_, err := h.Submit(callCtx, small)
	if err == nil || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrRequestTooLarge) {
		t.Fatalf("following request did not reach execution: %v", err)
	}
}

func TestSubmit_CallerCancellationStopsActiveRequest(t *testing.T) {
	client, started, upstreamCanceled := newBlockingLLMClient(t)
	h := newDispatchTestHub(t, Config{TokenBudget: 4}, client)

	callerCtx, cancelCaller := context.WithCancel(context.Background())
	req := SimpleRequest("sys", "cancel me", 1, PriorityNormal, "test")
	req.EstInputTokens = 3
	done := make(chan error, 1)
	go func() {
		_, err := h.Submit(callerCtx, req)
		done <- err
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("upstream request did not start")
	}
	cancelCaller()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Submit error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Submit did not return after caller cancellation")
	}
	select {
	case <-upstreamCanceled:
	case <-time.After(time.Second):
		t.Fatal("caller cancellation did not cancel the upstream LLM request")
	}

	// The active request's deferred release must return its full reservation.
	probeCtx, probeCancel := context.WithTimeout(context.Background(), time.Second)
	defer probeCancel()
	if err := h.budget.acquire(probeCtx, 4, PriorityNormal); err != nil {
		t.Fatalf("budget was not released after caller cancellation: %v", err)
	}
	h.budget.release(4)
	stats := h.Stats.Snapshot()
	if stats.Cancelled != 1 || stats.Failed != 0 {
		t.Fatalf("cancellation stats = %+v, want cancelled=1 failed=0", stats)
	}
}

func TestShutdownCancelsActiveRequest(t *testing.T) {
	client, started, upstreamCanceled := newBlockingLLMClient(t)
	h := newDispatchTestHub(t, Config{TokenBudget: 4}, client)

	req := SimpleRequest("sys", "shutdown", 1, PriorityNormal, "test")
	req.EstInputTokens = 3
	done := make(chan error, 1)
	go func() {
		_, err := h.Submit(context.Background(), req)
		done <- err
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("upstream request did not start")
	}
	shutdownDone := make(chan struct{})
	go func() {
		h.Shutdown()
		close(shutdownDone)
	}()

	select {
	case <-shutdownDone:
	case <-time.After(time.Second):
		t.Fatal("hub shutdown did not wait for and cancel the active request")
	}
	select {
	case err := <-done:
		if !errors.Is(err, ErrHubShutdown) {
			t.Fatalf("Submit error = %v, want ErrHubShutdown", err)
		}
	case <-time.After(time.Second):
		t.Fatal("active Submit did not return during hub shutdown")
	}
	select {
	case <-upstreamCanceled:
	case <-time.After(time.Second):
		t.Fatal("hub shutdown did not cancel the upstream LLM request")
	}
	if _, err := h.Submit(context.Background(), req); !errors.Is(err, ErrHubShutdown) {
		t.Fatalf("post-shutdown Submit error = %v, want ErrHubShutdown", err)
	}
	if stats := h.Stats.Snapshot(); stats.Failed != 0 {
		t.Fatalf("shutdown should not count as execution failure: %+v", stats)
	}
}

func newDispatchTestHub(t *testing.T, cfg Config, client *llm.Client) *Hub {
	t.Helper()
	cfg = cfg.withDefaults()
	ctx, cancel := context.WithCancel(context.Background())
	h := &Hub{
		client: client,
		cfg:    cfg,
		budget: newTokenBudgetLimiter(cfg.TokenBudget, cfg.CriticalOverdraw),
		queue:  newRequestQueue(),
		cache:  newResponseCache(cfg.CacheTTL, cfg.CacheMaxEntries),
		Stats:  &HubStats{},
		ctx:    ctx,
		cancel: cancel,
		logger: slog.Default(),
	}
	h.healthy.Store(true)
	h.wg.Add(1)
	go h.dispatchLoop()
	t.Cleanup(h.Shutdown)
	return h
}

func newBlockingLLMClient(t *testing.T) (*llm.Client, <-chan struct{}, <-chan struct{}) {
	t.Helper()
	started := make(chan struct{})
	canceled := make(chan struct{})
	var startedOnce sync.Once
	var canceledOnce sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		startedOnce.Do(func() { close(started) })
		<-r.Context().Done()
		canceledOnce.Do(func() { close(canceled) })
	}))
	t.Cleanup(srv.Close)
	return llm.NewClient(srv.URL, "test", llm.WithRetry(0, 0, 0)), started, canceled
}

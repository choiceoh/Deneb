package localai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

func TestCacheKeySemanticIdentityContract(t *testing.T) {
	base := SimpleRequest("system", "hello", 100, PriorityNormal, "caller-a")
	base.ExtraBody = map[string]any{
		"temperature": 0.2,
		"nested": map[string]any{
			"enabled": true,
		},
	}
	base.ResponseFormat = &llm.ResponseFormat{
		Type:       "json_schema",
		JSONSchema: llm.FlexibleFromRaw([]byte(`{"name":"answer","schema":{"type":"object"}}`)),
	}

	clone := func() Request {
		out := base
		out.Messages = append([]llm.Message(nil), base.Messages...)
		out.ExtraBody = map[string]any{
			"nested": map[string]any{
				"enabled": true,
			},
			"temperature": 0.2,
		}
		rf := *base.ResponseFormat
		rf.JSONSchema = llm.FlexibleFromRaw(append([]byte(nil), base.ResponseFormat.JSONSchema.Bytes()...))
		out.ResponseFormat = &rf
		return out
	}

	same := []struct {
		name   string
		mutate func(*Request)
	}{
		{
			name:   "identical clone",
			mutate: func(*Request) {},
		},
		{
			name: "caller tag is operational metadata",
			mutate: func(r *Request) {
				r.CallerTag = "caller-b"
			},
		},
		{
			name: "priority is scheduling metadata",
			mutate: func(r *Request) {
				r.Priority = PriorityCritical
			},
		},
		{
			name: "token estimate is admission metadata",
			mutate: func(r *Request) {
				r.EstInputTokens = 999
			},
		},
		{
			name: "cache policy does not alter generation",
			mutate: func(r *Request) {
				r.NoCache = true
				r.CacheTTL = time.Second
			},
		},
	}
	baseKey := cacheKey(&base)
	for _, tt := range same {
		t.Run("same/"+tt.name, func(t *testing.T) {
			r := clone()
			tt.mutate(&r)
			if got := cacheKey(&r); got != baseKey {
				t.Fatalf("operational-only mutation changed key: %x != %x", got, baseKey)
			}
		})
	}

	different := []struct {
		name   string
		mutate func(*Request)
	}{
		{
			name: "system prompt",
			mutate: func(r *Request) {
				r.System = "other"
			},
		},
		{
			name: "message role",
			mutate: func(r *Request) {
				r.Messages[0].Role = "assistant"
			},
		},
		{
			name: "message content",
			mutate: func(r *Request) {
				r.Messages[0].Content = llm.FlexibleFromRaw([]byte(`"different"`))
			},
		},
		{
			name: "message count",
			mutate: func(r *Request) {
				r.Messages = append(r.Messages, llm.NewTextMessage("assistant", "next"))
			},
		},
		{
			name: "max tokens",
			mutate: func(r *Request) {
				r.MaxTokens++
			},
		},
		{
			name: "response type",
			mutate: func(r *Request) {
				r.ResponseFormat.Type = "json_object"
			},
		},
		{
			name: "response schema",
			mutate: func(r *Request) {
				r.ResponseFormat.JSONSchema = llm.FlexibleFromRaw([]byte(`{"name":"other"}`))
			},
		},
		{
			name: "response format removed",
			mutate: func(r *Request) {
				r.ResponseFormat = nil
			},
		},
		{
			name: "temperature extra body",
			mutate: func(r *Request) {
				r.ExtraBody["temperature"] = 0.9
			},
		},
		{
			name: "nested extra body",
			mutate: func(r *Request) {
				r.ExtraBody["nested"] = map[string]any{"enabled": false}
			},
		},
		{
			name: "extra body field added",
			mutate: func(r *Request) {
				r.ExtraBody["top_p"] = 0.8
			},
		},
	}
	for _, tt := range different {
		t.Run("different/"+tt.name, func(t *testing.T) {
			r := clone()
			tt.mutate(&r)
			if got := cacheKey(&r); got == baseKey {
				t.Fatalf("semantic mutation retained key: %x", got)
			}
		})
	}

	t.Run("map insertion order is stable", func(t *testing.T) {
		a := clone()
		b := clone()
		a.ExtraBody = map[string]any{"a": 1, "b": 2, "c": 3}
		b.ExtraBody = map[string]any{"c": 3, "a": 1, "b": 2}
		if cacheKey(&a) != cacheKey(&b) {
			t.Fatal("JSON-equivalent maps produced different cache keys")
		}
	})
}

func TestCacheKeySeparatesBoundaryCollisions(t *testing.T) {
	// These requests produced the exact same concatenated byte stream before
	// cache fields were length framed:
	// user + "xassistant" + assistant + "y"
	// user + "x"          + assistant + "assistanty"
	a := Request{
		Messages: []llm.Message{
			llm.NewTextMessage("user", "xassistant"),
			llm.NewTextMessage("assistant", "y"),
		},
		MaxTokens: 100,
	}
	b := Request{
		Messages: []llm.Message{
			llm.NewTextMessage("user", "x"),
			llm.NewTextMessage("assistant", "assistanty"),
		},
		MaxTokens: 100,
	}
	if cacheKey(&a) == cacheKey(&b) {
		t.Fatal("message boundary collision was not separated")
	}

	c := Request{System: "ab", Messages: []llm.Message{{Role: "c", Content: llm.FlexibleFromRaw([]byte(`"d"`))}}}
	d := Request{System: "a", Messages: []llm.Message{{Role: "bc", Content: llm.FlexibleFromRaw([]byte(`"d"`))}}}
	if cacheKey(&c) == cacheKey(&d) {
		t.Fatal("system/role boundary collision was not separated")
	}
}

func TestResponseCacheContract(t *testing.T) {
	t.Run("nonpositive construction values use bounded defaults", func(t *testing.T) {
		cache := newResponseCache(0, 0)
		if cache.defaultTTL != defaultCacheTTL {
			t.Fatalf("ttl = %s, want %s", cache.defaultTTL, defaultCacheTTL)
		}
		for i := 0; i < defaultCacheMaxEntries+25; i++ {
			req := SimpleRequest("s", fmt.Sprintf("message-%d", i), 5, PriorityNormal, "test")
			cache.Put(&req, "value")
		}
		if cache.Len() != defaultCacheMaxEntries {
			t.Fatalf("len = %d, want %d", cache.Len(), defaultCacheMaxEntries)
		}
	})

	t.Run("get override can expire before default ttl", func(t *testing.T) {
		cache := newResponseCache(time.Hour, 3)
		req := SimpleRequest("s", "m", 5, PriorityNormal, "test")
		cache.Put(&req, "answer")
		key := cacheKey(&req)
		cache.lru.Put(key, cachedResponse{text: "answer", createdAt: time.Now().Add(-2 * time.Second)})
		if _, ok := cache.Get(&req, time.Second); ok {
			t.Fatal("per-call ttl did not expire old entry")
		}
		if got, ok := cache.Get(&req, time.Hour); !ok || got != "answer" {
			t.Fatalf("longer ttl miss = %q/%v", got, ok)
		}
	})

	t.Run("cleanup removes expired and retains fresh", func(t *testing.T) {
		cache := newResponseCache(time.Minute, 4)
		oldReq := SimpleRequest("s", "old", 5, PriorityNormal, "test")
		newReq := SimpleRequest("s", "new", 5, PriorityNormal, "test")
		cache.lru.Put(cacheKey(&oldReq), cachedResponse{text: "old", createdAt: time.Now().Add(-2 * time.Minute)})
		cache.lru.Put(cacheKey(&newReq), cachedResponse{text: "new", createdAt: time.Now()})
		cache.Cleanup()
		if cache.Len() != 1 {
			t.Fatalf("len after cleanup = %d, want 1", cache.Len())
		}
		if _, ok := cache.Get(&oldReq, 0); ok {
			t.Fatal("expired entry survived cleanup")
		}
		if got, ok := cache.Get(&newReq, 0); !ok || got != "new" {
			t.Fatalf("fresh entry missing = %q/%v", got, ok)
		}
	})

	t.Run("lru access keeps hot entry", func(t *testing.T) {
		cache := newResponseCache(time.Hour, 2)
		a := SimpleRequest("s", "a", 5, PriorityNormal, "test")
		b := SimpleRequest("s", "b", 5, PriorityNormal, "test")
		c := SimpleRequest("s", "c", 5, PriorityNormal, "test")
		cache.Put(&a, "a")
		cache.Put(&b, "b")
		if _, ok := cache.Get(&a, 0); !ok {
			t.Fatal("hot entry unexpectedly missed")
		}
		cache.Put(&c, "c")
		if _, ok := cache.Get(&b, 0); ok {
			t.Fatal("least recently used entry was not evicted")
		}
		if _, ok := cache.Get(&a, 0); !ok {
			t.Fatal("recently accessed entry was evicted")
		}
	})
}

func queueTestEntry(tag string, priority Priority, at time.Time) (*queueEntry, chan submitResult) {
	ch := make(chan submitResult, 1)
	return &queueEntry{
		req:        &Request{Priority: priority, CallerTag: tag},
		callerCtx:  context.Background(),
		resultCh:   ch,
		enqueuedAt: at,
	}, ch
}

func TestRequestQueueOrderingAndLifecycleContract(t *testing.T) {
	t.Run("priority then fifo", func(t *testing.T) {
		q := newRequestQueue()
		base := time.Now()
		fixtures := []struct {
			tag      string
			priority Priority
			offset   time.Duration
		}{
			{tag: "background-old", priority: PriorityBackground, offset: 0},
			{tag: "normal-new", priority: PriorityNormal, offset: 4 * time.Millisecond},
			{tag: "critical-new", priority: PriorityCritical, offset: 5 * time.Millisecond},
			{tag: "normal-old", priority: PriorityNormal, offset: 2 * time.Millisecond},
			{tag: "critical-old", priority: PriorityCritical, offset: time.Millisecond},
			{tag: "background-new", priority: PriorityBackground, offset: 6 * time.Millisecond},
		}
		for _, fixture := range fixtures {
			entry, _ := queueTestEntry(fixture.tag, fixture.priority, base.Add(fixture.offset))
			if !q.Push(entry) {
				t.Fatalf("push %s rejected", fixture.tag)
			}
		}
		want := []string{
			"critical-old",
			"critical-new",
			"normal-old",
			"normal-new",
			"background-old",
			"background-new",
		}
		got := make([]string, 0, len(want))
		for range want {
			entry := q.PopWait(nil)
			if entry == nil {
				t.Fatal("queue returned nil before empty")
			}
			got = append(got, entry.req.CallerTag)
			if entry.index != -1 {
				t.Fatalf("popped entry index = %d, want -1", entry.index)
			}
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("order = %v, want %v", got, want)
		}
	})

	t.Run("close unblocks pop and rejects future push", func(t *testing.T) {
		q := newRequestQueue()
		done := make(chan *queueEntry, 1)
		go func() {
			done <- q.PopWait(nil)
		}()
		q.Close()
		q.Close()
		select {
		case got := <-done:
			if got != nil {
				t.Fatalf("closed empty queue returned %+v", got)
			}
		case <-time.After(time.Second):
			t.Fatal("close did not unblock PopWait")
		}
		entry, _ := queueTestEntry("late", PriorityNormal, time.Now())
		if q.Push(entry) {
			t.Fatal("push after close succeeded")
		}
		if q.Len() != 0 {
			t.Fatalf("closed queue len = %d", q.Len())
		}
	})

	t.Run("close with queued item does not dispatch it", func(t *testing.T) {
		q := newRequestQueue()
		entry, _ := queueTestEntry("queued", PriorityNormal, time.Now())
		q.Push(entry)
		q.Close()
		if got := q.PopWait(nil); got != nil {
			t.Fatalf("closed queue dispatched %+v", got)
		}
		if q.Len() != 1 {
			t.Fatalf("close should leave queued work for DrainAll, len=%d", q.Len())
		}
	})
}

func TestRequestQueueDropAndDrainContract(t *testing.T) {
	t.Run("drop oldest background only above depth", func(t *testing.T) {
		q := newRequestQueue()
		base := time.Now()
		normal, normalCh := queueTestEntry("normal", PriorityNormal, base)
		oldBG, oldCh := queueTestEntry("old-background", PriorityBackground, base.Add(time.Millisecond))
		newBG, newCh := queueTestEntry("new-background", PriorityBackground, base.Add(2*time.Millisecond))
		q.Push(normal)
		q.Push(oldBG)
		q.Push(newBG)
		if q.DropOldestBackground(3) {
			t.Fatal("drop occurred at exact depth")
		}
		if !q.DropOldestBackground(2) {
			t.Fatal("oldest background was not dropped above depth")
		}
		select {
		case res := <-oldCh:
			if !errors.Is(res.err, ErrQueueFull) {
				t.Fatalf("drop error = %v", res.err)
			}
		default:
			t.Fatal("dropped entry was not notified")
		}
		select {
		case res := <-newCh:
			t.Fatalf("newer background incorrectly notified: %+v", res)
		default:
		}
		select {
		case res := <-normalCh:
			t.Fatalf("normal entry incorrectly notified: %+v", res)
		default:
		}
		if oldBG.index != -1 {
			t.Fatalf("removed entry index = %d", oldBG.index)
		}
	})

	t.Run("no background means no drop", func(t *testing.T) {
		q := newRequestQueue()
		for i := 0; i < 3; i++ {
			entry, _ := queueTestEntry(fmt.Sprintf("normal-%d", i), PriorityNormal, time.Now().Add(time.Duration(i)))
			q.Push(entry)
		}
		if q.DropOldestBackground(1) {
			t.Fatal("normal request was dropped")
		}
		if q.Len() != 3 {
			t.Fatalf("len = %d, want 3", q.Len())
		}
	})

	t.Run("drain returns supplied error to every priority", func(t *testing.T) {
		q := newRequestQueue()
		wantErr := errors.New("maintenance")
		channels := make([]chan submitResult, 0, 3)
		for i, priority := range []Priority{PriorityCritical, PriorityNormal, PriorityBackground} {
			entry, ch := queueTestEntry(fmt.Sprintf("entry-%d", i), priority, time.Now())
			q.Push(entry)
			channels = append(channels, ch)
		}
		q.DrainAll(wantErr)
		if q.Len() != 0 {
			t.Fatalf("len after drain = %d", q.Len())
		}
		for i, ch := range channels {
			select {
			case res := <-ch:
				if !errors.Is(res.err, wantErr) {
					t.Fatalf("entry %d error = %v", i, res.err)
				}
			case <-time.After(time.Second):
				t.Fatalf("entry %d was not drained", i)
			}
		}
	})
}

func TestTokenBudgetLimiterAdmissionContract(t *testing.T) {
	t.Run("normal and critical limits differ", func(t *testing.T) {
		l := newTokenBudgetLimiter(100, 0.25)
		if l.limit != 100 || l.criticalLimit != 125 {
			t.Fatalf("limits = %d/%d, want 100/125", l.limit, l.criticalLimit)
		}
		if err := l.acquire(context.Background(), 100, PriorityNormal); err != nil {
			t.Fatal(err)
		}
		l.release(100)
		if err := l.acquire(context.Background(), 125, PriorityCritical); err != nil {
			t.Fatal(err)
		}
		l.release(125)
		if err := l.acquire(context.Background(), 101, PriorityNormal); !errors.Is(err, ErrRequestTooLarge) {
			t.Fatalf("normal oversized = %v", err)
		}
		if err := l.acquire(context.Background(), 126, PriorityCritical); !errors.Is(err, ErrRequestTooLarge) {
			t.Fatalf("critical oversized = %v", err)
		}
	})

	t.Run("cancelled context never reserves", func(t *testing.T) {
		l := newTokenBudgetLimiter(10, 0)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := l.acquire(ctx, 1, PriorityNormal); !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want canceled", err)
		}
		if l.inFlight != 0 {
			t.Fatalf("inFlight = %d, want 0", l.inFlight)
		}
	})

	t.Run("release wakes all contenders without over-admission", func(t *testing.T) {
		l := newTokenBudgetLimiter(10, 0)
		if err := l.acquire(context.Background(), 10, PriorityNormal); err != nil {
			t.Fatal(err)
		}
		const waiters = 5
		acquired := make(chan int, waiters)
		release := make(chan struct{})
		var wg sync.WaitGroup
		for i := range waiters {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := l.acquire(context.Background(), 5, PriorityNormal); err != nil {
					t.Errorf("waiter %d: %v", i, err)
					return
				}
				acquired <- i
				<-release
				l.release(5)
			}()
		}
		l.release(10)
		for batch := 0; batch < waiters; {
			select {
			case <-acquired:
				batch++
				l.mu.Lock()
				if l.inFlight > l.limit {
					t.Fatalf("over-admitted %d > %d", l.inFlight, l.limit)
				}
				l.mu.Unlock()
				release <- struct{}{}
			case <-time.After(time.Second):
				t.Fatalf("only %d/%d waiters admitted", batch, waiters)
			}
		}
		wg.Wait()
		l.mu.Lock()
		defer l.mu.Unlock()
		if l.inFlight != 0 {
			t.Fatalf("inFlight after releases = %d", l.inFlight)
		}
	})

	t.Run("close is idempotent and terminal", func(t *testing.T) {
		l := newTokenBudgetLimiter(10, 0)
		l.close()
		l.close()
		if err := l.acquire(context.Background(), 0, PriorityNormal); !errors.Is(err, ErrHubShutdown) {
			t.Fatalf("zero-token post-close acquire = %v", err)
		}
	})
}

func TestLinkedRequestContextContract(t *testing.T) {
	type ctxKey string
	caller := context.WithValue(context.Background(), ctxKey("request"), "value")
	caller, cancelCaller := context.WithTimeout(caller, time.Minute)
	defer cancelCaller()
	hub, cancelHub := context.WithCancel(context.Background())
	linked, cleanup := linkedRequestContext(caller, hub)
	defer cleanup()
	if got := linked.Value(ctxKey("request")); got != "value" {
		t.Fatalf("caller value = %v", got)
	}
	callerDeadline, _ := caller.Deadline()
	linkedDeadline, ok := linked.Deadline()
	if !ok || !linkedDeadline.Equal(callerDeadline) {
		t.Fatalf("deadline = %v/%v, want %v", linkedDeadline, ok, callerDeadline)
	}
	cancelHub()
	select {
	case <-linked.Done():
		if !errors.Is(context.Cause(linked), ErrHubShutdown) {
			t.Fatalf("cause = %v, want ErrHubShutdown", context.Cause(linked))
		}
	case <-time.After(time.Second):
		t.Fatal("hub cancellation did not reach linked context")
	}

	t.Run("caller cancellation remains caller cancellation", func(t *testing.T) {
		caller, cancelCaller := context.WithCancel(context.Background())
		hub, cancelHub := context.WithCancel(context.Background())
		defer cancelHub()
		linked, cleanup := linkedRequestContext(caller, hub)
		defer cleanup()
		cancelCaller()
		<-linked.Done()
		if !errors.Is(context.Cause(linked), context.Canceled) {
			t.Fatalf("cause = %v, want context.Canceled", context.Cause(linked))
		}
	})
}

func TestCollectStreamContract(t *testing.T) {
	delta := func(typ, text string) llm.StreamEvent {
		payload, err := json.Marshal(map[string]any{
			"delta": map[string]string{
				"type": typ,
				"text": text,
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		return llm.StreamEvent{Type: "content_block_delta", Payload: llm.FlexibleFromRaw(payload)}
	}

	tests := []struct {
		name   string
		events []llm.StreamEvent
		want   string
	}{
		{
			name: "empty closed stream",
			want: "",
		},
		{
			name: "text chunks are concatenated and trimmed",
			events: []llm.StreamEvent{
				delta("text_delta", "  hello"),
				delta("text_delta", " world  "),
			},
			want: "hello world",
		},
		{
			name: "non-delta events are ignored",
			events: []llm.StreamEvent{
				{Type: "message_start", Payload: llm.FlexibleFromRaw([]byte(`{}`))},
				delta("text_delta", "answer"),
				{Type: "message_stop", Payload: llm.FlexibleFromRaw([]byte(`{}`))},
			},
			want: "answer",
		},
		{
			name: "private reasoning and malformed payload are ignored",
			events: []llm.StreamEvent{
				delta("thinking_delta", "secret"),
				{Type: "content_block_delta", Payload: llm.FlexibleFromRaw([]byte(`not-json`))},
				delta("signature_delta", "signature"),
				delta("text_delta", "safe"),
			},
			want: "safe",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := make(chan llm.StreamEvent, len(tt.events))
			for _, event := range tt.events {
				ch <- event
			}
			close(ch)
			got, _, err := collectStream(context.Background(), ch)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("text = %q, want %q", got, tt.want)
			}
		})
	}

	if _, _, err := collectStream(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "nil event channel") {
		t.Fatalf("nil stream error = %v", err)
	}

	t.Run("cancellation before text returns context error", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		ch := make(chan llm.StreamEvent)
		got, _, err := collectStream(ctx, ch)
		if got != "" || !errors.Is(err, context.Canceled) {
			t.Fatalf("result = %q/%v", got, err)
		}
	})

	t.Run("cancellation after partial text returns partial result", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		ch := make(chan llm.StreamEvent, 1)
		ch <- delta("text_delta", "partial")
		gotCh := make(chan struct {
			text string
			err  error
		}, 1)
		go func() {
			text, _, err := collectStream(ctx, ch)
			gotCh <- struct {
				text string
				err  error
			}{text: text, err: err}
		}()
		time.Sleep(10 * time.Millisecond)
		cancel()
		got := <-gotCh
		if got.text != "partial" || got.err != nil {
			t.Fatalf("result = %q/%v", got.text, got.err)
		}
	})
}

func TestHealthCheckerContract(t *testing.T) {
	t.Run("warmup interval transitions to steady state", func(t *testing.T) {
		hc := &healthChecker{started: time.Now()}
		if got := hc.interval(); got != healthWarmupTTL {
			t.Fatalf("warm interval = %s, want %s", got, healthWarmupTTL)
		}
		hc.started = time.Now().Add(-healthWarmupPeriod - time.Second)
		if got := hc.interval(); got != healthCheckInterval {
			t.Fatalf("steady interval = %s, want %s", got, healthCheckInterval)
		}
	})

	t.Run("ping sends bearer auth and requires 200", func(t *testing.T) {
		var requests atomic.Int64
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests.Add(1)
			if r.Method != http.MethodGet || r.URL.Path != "/models" {
				t.Errorf("request = %s %s", r.Method, r.URL.Path)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer secret" {
				t.Errorf("authorization = %q", got)
			}
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		hub := &Hub{ctx: ctx}
		hc := &healthChecker{hub: hub, baseURL: srv.URL, apiKey: "secret"}
		if !hc.pingModels() {
			t.Fatal("healthy models endpoint reported false")
		}
		if requests.Load() != 1 {
			t.Fatalf("requests = %d, want 1", requests.Load())
		}
	})

	tests := []struct {
		name   string
		status int
	}{
		{name: "unauthorized", status: http.StatusUnauthorized},
		{name: "not found", status: http.StatusNotFound},
		{name: "server error", status: http.StatusInternalServerError},
		{name: "no content is not models success", status: http.StatusNoContent},
	}
	for _, tt := range tests {
		t.Run("status/"+tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
			}))
			defer srv.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			hc := &healthChecker{hub: &Hub{ctx: ctx}, baseURL: srv.URL}
			if hc.pingModels() {
				t.Fatalf("HTTP %d reported healthy", tt.status)
			}
		})
	}

	t.Run("check publishes health and timestamp", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		hub := &Hub{ctx: ctx}
		hc := &healthChecker{hub: hub, baseURL: srv.URL}
		before := time.Now().Unix()
		hc.check()
		if !hub.IsHealthy() {
			t.Fatal("check did not store healthy=true")
		}
		if got := hub.lastHealthCheck.Load(); got < before || got > time.Now().Unix() {
			t.Fatalf("last health check = %d, outside expected range", got)
		}
	})
}

func TestConfigAndStatsContract(t *testing.T) {
	t.Run("defaults fill only nonpositive fields", func(t *testing.T) {
		input := Config{
			TokenBudget:      -1,
			CriticalOverdraw: -1,
			MaxQueueDepth:    -1,
			CacheTTL:         -1,
			CacheMaxEntries:  -1,
		}
		got := input.withDefaults()
		if got.TokenBudget != 65_536 {
			t.Fatalf("token budget = %d", got.TokenBudget)
		}
		if got.CriticalOverdraw != 0.25 {
			t.Fatalf("critical overdraw = %f", got.CriticalOverdraw)
		}
		if got.MaxQueueDepth != 20 {
			t.Fatalf("queue depth = %d", got.MaxQueueDepth)
		}
		if got.CacheTTL != defaultCacheTTL {
			t.Fatalf("cache ttl = %s", got.CacheTTL)
		}
		if got.CacheMaxEntries != defaultCacheMaxEntries {
			t.Fatalf("cache entries = %d", got.CacheMaxEntries)
		}
		if input.TokenBudget != -1 {
			t.Fatal("withDefaults mutated receiver")
		}
	})

	t.Run("explicit values survive", func(t *testing.T) {
		input := Config{
			TokenBudget:      123,
			CriticalOverdraw: 0.5,
			MaxQueueDepth:    7,
			CacheTTL:         9 * time.Second,
			CacheMaxEntries:  11,
		}
		if got := input.withDefaults(); !reflect.DeepEqual(got, input) {
			t.Fatalf("got %+v, want %+v", got, input)
		}
	})

	t.Run("snapshot atomically captures all counters", func(t *testing.T) {
		stats := &HubStats{}
		stats.Submitted.Store(11)
		stats.Completed.Store(7)
		stats.Failed.Store(2)
		stats.Cancelled.Store(3)
		stats.Dropped.Store(5)
		stats.CacheHits.Store(13)
		stats.CacheMisses.Store(17)
		want := StatsSnapshot{
			Submitted:   11,
			Completed:   7,
			Failed:      2,
			Cancelled:   3,
			Dropped:     5,
			CacheHits:   13,
			CacheMisses: 17,
		}
		if got := stats.Snapshot(); got != want {
			t.Fatalf("snapshot = %+v, want %+v", got, want)
		}
	})
}

func TestSimpleRequestAndEstimateContract(t *testing.T) {
	req := SimpleRequest("system", "사용자 메시지", 321, PriorityBackground, "dream")
	if req.System != "system" || req.MaxTokens != 321 || req.Priority != PriorityBackground || req.CallerTag != "dream" {
		t.Fatalf("request metadata = %+v", req)
	}
	if len(req.Messages) != 1 || req.Messages[0].Role != "user" {
		t.Fatalf("messages = %+v", req.Messages)
	}
	var content string
	if err := json.Unmarshal(req.Messages[0].Content.Bytes(), &content); err != nil {
		t.Fatal(err)
	}
	if content != "사용자 메시지" {
		t.Fatalf("content = %q", content)
	}

	tests := []struct {
		name string
		req  Request
	}{
		{name: "empty request", req: Request{}},
		{name: "system only", req: Request{System: "short system"}},
		{name: "ascii message", req: SimpleRequest("", "hello world", 1, PriorityNormal, "")},
		{name: "korean message", req: SimpleRequest("", "안녕하세요 세계", 1, PriorityNormal, "")},
		{name: "multiple messages", req: Request{Messages: []llm.Message{
			llm.NewTextMessage("user", "first"),
			llm.NewTextMessage("assistant", "second"),
			llm.NewTextMessage("user", "third"),
		}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := estimateInputTokens(&tt.req)
			if got < 1 {
				t.Fatalf("estimate = %d, want at least 1", got)
			}
		})
	}
}

func TestRequestErrorAndFailureAccounting(t *testing.T) {
	newHub := func() (*Hub, context.CancelFunc) {
		ctx, cancel := context.WithCancel(context.Background())
		return &Hub{ctx: ctx, Stats: &HubStats{}, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}, cancel
	}

	t.Run("caller cancellation wins over transport error", func(t *testing.T) {
		h, cancelHub := newHub()
		defer cancelHub()
		caller, cancelCaller := context.WithCancel(context.Background())
		cancelCaller()
		entry := &queueEntry{callerCtx: caller}
		if got := h.requestError(caller, entry, errors.New("transport")); !errors.Is(got, context.Canceled) {
			t.Fatalf("error = %v", got)
		}
	})

	t.Run("hub shutdown wins over transport error", func(t *testing.T) {
		h, cancelHub := newHub()
		cancelHub()
		entry := &queueEntry{callerCtx: context.Background()}
		if got := h.requestError(context.Background(), entry, errors.New("transport")); !errors.Is(got, ErrHubShutdown) {
			t.Fatalf("error = %v", got)
		}
	})

	t.Run("ordinary transport error passes through", func(t *testing.T) {
		h, cancelHub := newHub()
		defer cancelHub()
		want := errors.New("transport")
		entry := &queueEntry{callerCtx: context.Background()}
		if got := h.requestError(context.Background(), entry, want); !errors.Is(got, want) {
			t.Fatalf("error = %v", got)
		}
	})

	t.Run("only operational failures increment failed", func(t *testing.T) {
		h, cancelHub := newHub()
		defer cancelHub()
		for _, err := range []error{context.Canceled, context.DeadlineExceeded, ErrHubShutdown} {
			h.recordExecutionFailure(err)
		}
		if got := h.Stats.Failed.Load(); got != 0 {
			t.Fatalf("terminal context errors counted failed=%d", got)
		}
		h.recordExecutionFailure(errors.New("provider 500"))
		if got := h.Stats.Failed.Load(); got != 1 {
			t.Fatalf("provider failure count = %d", got)
		}
	})
}

func TestHealthRunStopsWithHubContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	hub := &Hub{ctx: ctx}
	hub.wg.Add(1)
	hc := &healthChecker{
		hub:     hub,
		baseURL: srv.URL,
		started: time.Now(),
	}
	done := make(chan struct{})
	go func() {
		hc.run()
		close(done)
	}()
	deadline := time.Now().Add(time.Second)
	for hub.lastHealthCheck.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if hub.lastHealthCheck.Load() == 0 {
		t.Fatal("health loop did not perform immediate check")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("health loop did not stop")
	}
	if !hub.IsHealthy() {
		t.Fatal("successful immediate health result was not stored")
	}
}

func TestConcurrentStatsSnapshotIsRaceSafe(t *testing.T) {
	stats := &HubStats{}
	const workers = 8
	const iterations = 500
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range iterations {
				stats.Submitted.Add(1)
				stats.Completed.Add(1)
				stats.CacheHits.Add(1)
				_ = stats.Snapshot()
			}
		}()
	}
	wg.Wait()
	want := int64(workers * iterations)
	got := stats.Snapshot()
	if got.Submitted != want || got.Completed != want || got.CacheHits != want {
		t.Fatalf("snapshot = %+v, want selected counters=%d", got, want)
	}
}

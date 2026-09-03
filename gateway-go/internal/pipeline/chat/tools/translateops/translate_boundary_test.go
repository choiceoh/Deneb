package translateops

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestBoundaryTranslateInputEnvelopeMatrix(t *testing.T) {
	prefix := translateSegmentEnvelopePrefix
	tests := []struct {
		name string
		raw  string
		want translateInput
	}{
		{name: "plain", raw: "plain", want: translateInput{Text: "plain"}},
		{name: "empty plain", raw: "", want: translateInput{Text: ""}},
		{name: "malformed envelope", raw: prefix + `{`, want: translateInput{Text: prefix + `{`}},
		{name: "empty envelope fallback", raw: prefix + `{}`, want: translateInput{Text: prefix + `{}`}},
		{name: "text envelope", raw: prefix + `{"text":"hello"}`, want: translateInput{Text: "hello"}},
		{name: "metadata trimmed", raw: prefix + `{"text":"hello","context":" ctx ","role":" heading "}`, want: translateInput{Text: "hello", Context: "ctx", Role: "heading"}},
		{name: "parts joined", raw: prefix + `{"parts":["one","two"]}`, want: translateInput{Text: "onetwo", Parts: []string{"one", "two"}}},
		{name: "empty parts dropped", raw: prefix + `{"parts":["one","","two"]}`, want: translateInput{Text: "onetwo", Parts: []string{"one", "two"}}},
		{name: "parts beat text", raw: prefix + `{"text":"ignored","parts":["one","two"]}`, want: translateInput{Text: "onetwo", Parts: []string{"one", "two"}}},
		{name: "whitespace part retained", raw: prefix + `{"parts":[" ","x"]}`, want: translateInput{Text: " x", Parts: []string{" ", "x"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseTranslateInput(tt.raw); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parseTranslateInput = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestBoundaryTranslateBatchRangeLimits(t *testing.T) {
	// Sizes derive from the bounds themselves: these constants are tuned against
	// measured DeepL latency (see translate.go), and a test that hard-codes the
	// old numbers fails the tuning rather than the behaviour.
	const tail = 5
	inputs := make([]translateInput, translateMaxSegmentsPerBatch*2+tail)
	for i := range inputs {
		inputs[i] = translateInput{Text: "x"} // 1 char each → the SEGMENT bound decides
	}
	want := []translateBatchRange{
		{Start: 0, End: translateMaxSegmentsPerBatch},
		{Start: translateMaxSegmentsPerBatch, End: translateMaxSegmentsPerBatch * 2},
		{Start: translateMaxSegmentsPerBatch * 2, End: translateMaxSegmentsPerBatch*2 + tail},
	}
	if got := translateBatchRanges(inputs); !reflect.DeepEqual(got, want) {
		t.Fatalf("short input ranges = %#v, want %#v", got, want)
	}
	// Two inputs that together clear the char bound, then a third that cannot join.
	half := (translateMaxCharsPerBatch / 2) + 100
	large := []translateInput{
		{Text: strings.Repeat("a", half)},
		{Text: strings.Repeat("b", translateMaxCharsPerBatch-half)},
		{Text: "c"},
	}
	want = []translateBatchRange{{Start: 0, End: 2}, {Start: 2, End: 3}}
	if got := translateBatchRanges(large); !reflect.DeepEqual(got, want) {
		t.Fatalf("char-bound ranges = %#v, want %#v", got, want)
	}
	// Part-count bound: a batch must never flatten past DeepL's per-request text
	// limit, because translateBatchDeepL rejects such a batch outright and the
	// caller then pays a wasted round trip before splitting it.
	perInput := 10
	parts := make([]string, perInput)
	for i := range parts {
		parts[i] = "p"
	}
	withParts := make([]translateInput, 12) // 120 flattened texts
	for i := range withParts {
		withParts[i] = translateInput{Text: strings.Repeat("p", perInput), Parts: parts}
	}
	for _, r := range translateBatchRanges(withParts) {
		texts := 0
		for _, in := range withParts[r.Start:r.End] {
			texts += translateInputTexts(in)
		}
		if texts > maxDeepLTextsPerRequest {
			t.Fatalf("range %#v flattens to %d texts, over DeepL's %d limit", r, texts, maxDeepLTextsPerRequest)
		}
	}
	if got := translateBatchRanges(nil); len(got) != 0 {
		t.Fatalf("nil ranges = %#v", got)
	}
}

func TestBoundaryTranslateInputCostContextDiscount(t *testing.T) {
	tests := []struct {
		in   translateInput
		want int
	}{
		{in: translateInput{}, want: 0},
		{in: translateInput{Text: "abcd"}, want: 4},
		{in: translateInput{Context: "abcd"}, want: 1},
		{in: translateInput{Text: "abcd", Context: "abcdefgh"}, want: 6},
		{in: translateInput{Text: "가"}, want: len("가")},
	}
	for _, tt := range tests {
		if got := translateInputCost(tt.in); got != tt.want {
			t.Fatalf("translateInputCost(%#v) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestBoundaryTranslateRangeFailureBisectsAndPreservesOrder(t *testing.T) {
	original := translateBatchFn
	t.Cleanup(func() { translateBatchFn = original })
	var mu sync.Mutex
	var calls [][]string
	translateBatchFn = func(_ context.Context, batch []translateInput, _ string) ([]string, translateBatchOutcome) {
		texts := make([]string, len(batch))
		for i := range batch {
			texts[i] = batch[i].Text
		}
		mu.Lock()
		calls = append(calls, texts)
		mu.Unlock()
		if len(batch) > 1 {
			return nil, batchRetryable
		}
		return []string{"T:" + batch[0].Text}, batchOK
	}
	inputs := []translateInput{{Text: "a"}, {Text: "b"}, {Text: "c"}, {Text: "d"}}
	out := []string{"a", "b", "c", "d"}
	var st translateRangeState
	translateRange(context.Background(), inputs, out, &st, 0, len(inputs), "Korean")
	if !reflect.DeepEqual(out, []string{"T:a", "T:b", "T:c", "T:d"}) {
		t.Fatalf("translated output = %v", out)
	}
	if st.done.Load() != int64(len(inputs)) {
		t.Fatalf("translated count = %d, want %d", st.done.Load(), len(inputs))
	}
	if len(calls) != 7 {
		t.Fatalf("bisection calls = %d, want 7: %#v", len(calls), calls)
	}
}

func TestBoundaryTranslateSegmentsConcurrentBatchLimit(t *testing.T) {
	original := translateBatchFn
	t.Cleanup(func() { translateBatchFn = original })
	var active atomic.Int32
	var maxActive atomic.Int32
	translateBatchFn = func(_ context.Context, batch []translateInput, _ string) ([]string, translateBatchOutcome) {
		current := active.Add(1)
		defer active.Add(-1)
		for {
			old := maxActive.Load()
			if current <= old || maxActive.CompareAndSwap(old, current) {
				break
			}
		}
		time.Sleep(2 * time.Millisecond)
		out := make([]string, len(batch))
		for i := range batch {
			out[i] = "T:" + batch[i].Text
		}
		return out, batchOK
	}
	segments := make([]string, translateMaxSegmentsPerBatch*8)
	for i := range segments {
		segments[i] = fmt.Sprintf("segment-%03d", i)
	}
	got, err := TranslateSegments(context.Background(), segments, "Korean")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(segments) {
		t.Fatalf("translated length = %d", len(got))
	}
	for i := range got {
		if got[i] != "T:"+segments[i] {
			t.Fatalf("output %d = %q", i, got[i])
		}
	}
	if maxActive.Load() > translateMaxConcurrentBatches {
		t.Fatalf("max concurrent batches = %d", maxActive.Load())
	}
}

// TestBoundaryDeepLRetryClassification pins WHICH failures are worth a second
// call. Retrying an auth or quota error just doubles the wait before the page
// falls back to originals; retrying a 429 is the whole point of the retry.
func TestBoundaryDeepLRetryClassification(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   bool
	}{
		{429, true},
		{500, true},
		{503, true},
		{599, true},
		{403, false}, // bad key
		{456, false}, // quota exhausted — retrying cannot help
		{400, false},
		{0, false}, // no response at all
	} {
		if got := deepLStatusWorthRetry(tc.status); got != tc.want {
			t.Errorf("deepLStatusWorthRetry(%d) = %v, want %v", tc.status, got, tc.want)
		}
	}
	if deepLRetryWait(429) != deeplRetryWaitCap {
		t.Errorf("a rate limit must wait the cap, got %v", deepLRetryWait(429))
	}
	if deepLRetryWait(503) >= deeplRetryWaitCap {
		t.Errorf("a 5xx should back off less than a rate limit, got %v", deepLRetryWait(503))
	}
}

// TestBoundarySleepCtxHonoursCancellation: the retry wait must not outlive the
// caller — a cancelled page navigation should not hold a translate goroutine.
func TestBoundarySleepCtxHonoursCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if sleepCtx(ctx, time.Minute) {
		t.Fatal("sleepCtx reported a completed wait on a cancelled context")
	}
	if !sleepCtx(context.Background(), time.Millisecond) {
		t.Fatal("sleepCtx should report completion on a normal wait")
	}
}

// A refusing TRANSLATOR must cost one call, not 2n-1. translateRange answers a
// failure by splitting, which is right for a batch DeepL choked on and exactly
// wrong for an auth error, an exhausted quota or a rate limit: every half gets
// the identical answer. With the 429 retry from #5010 each of those doomed
// leaves could also sleep 3s first, so a 40-segment range could burn ~79 calls
// and most of a minute to learn one thing.
func TestBoundaryHopelessTranslatorDoesNotBisect(t *testing.T) {
	original := translateBatchFn
	t.Cleanup(func() { translateBatchFn = original })
	var calls atomic.Int32
	translateBatchFn = func(_ context.Context, _ []translateInput, _ string) ([]string, translateBatchOutcome) {
		calls.Add(1)
		return nil, batchHopeless
	}

	inputs := make([]translateInput, 40)
	out := make([]string, len(inputs))
	for i := range inputs {
		inputs[i] = translateInput{Text: fmt.Sprintf("segment-%02d", i)}
		out[i] = inputs[i].Text
	}
	var st translateRangeState
	translateRange(context.Background(), inputs, out, &st, 0, len(inputs), "Korean")

	if got := calls.Load(); got != 1 {
		t.Fatalf("hopeless range made %d calls, want exactly 1 (bisection would make 79)", got)
	}
	if st.done.Load() != 0 {
		t.Fatalf("translated count = %d, want 0", st.done.Load())
	}
	if !st.hopeless.Load() {
		t.Fatal("hopeless was not latched for the sibling ranges")
	}
}

// The latch is shared across a whole TranslateSegments call, so the ranges still
// queued behind a refusing translator are never sent at all.
func TestBoundaryHopelessStopsTheRemainingRanges(t *testing.T) {
	original := translateBatchFn
	t.Cleanup(func() { translateBatchFn = original })
	var calls atomic.Int32
	translateBatchFn = func(_ context.Context, _ []translateInput, _ string) ([]string, translateBatchOutcome) {
		calls.Add(1)
		return nil, batchHopeless
	}

	segments := make([]string, translateMaxSegmentsPerBatch*8)
	for i := range segments {
		segments[i] = fmt.Sprintf("segment-%03d", i)
	}
	got, err := TranslateSegments(context.Background(), segments, "Korean")
	if err == nil {
		t.Fatal("a refusing translator reported success")
	}
	if len(got) != len(segments) {
		t.Fatalf("out length = %d, want %d", len(got), len(segments))
	}
	// Ranges already dispatched into the semaphore still run; the point is that
	// the rest never queue and nothing bisects. Without the latch this same input
	// costs one call per range PLUS a full bisection under each.
	if n := calls.Load(); n > int32(translateMaxConcurrentBatches) {
		t.Fatalf("calls = %d, want at most the concurrency limit %d", n, translateMaxConcurrentBatches)
	}
}

// The status classification is where the bisect decision is actually made, so
// pin both sides of it against real HTTP responses.
func TestBoundaryDeepLStatusDecidesWhetherSplittingCanHelp(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   translateBatchOutcome
		calls  int
	}{
		// Account-level: the same answer comes back for every half.
		{"quota exhausted", 456, `{"message":"Quota exceeded"}`, batchHopeless, 1},
		{"auth", http.StatusForbidden, `{"message":"Authorization failed"}`, batchHopeless, 1},
		// Payload-level: a smaller request is exactly the remedy.
		{"bad request", http.StatusBadRequest, `{"message":"Bad request"}`, batchRetryable, 1},
		// Rate limit is account-level too — and splitting makes it WORSE, since
		// the halves are more requests, not fewer. Retried first, then hopeless.
		{"rate limited", http.StatusTooManyRequests, `{"message":"Too many requests"}`, batchHopeless, deeplRetryAttempts},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resetTranslateTextCache()
			oldClient := deeplHTTPClient
			t.Cleanup(func() { deeplHTTPClient = oldClient })
			calls := 0
			deeplHTTPClient = &http.Client{Transport: deepLRoundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return deepLTestResponse(tc.status, tc.body), nil
			})}
			t.Setenv("DEEPL_API_KEY", "test-key")

			out, outcome := translateBatchDeepL(context.Background(), []translateInput{{Text: "Hello"}}, "ko")
			if outcome != tc.want {
				t.Fatalf("status %d → outcome %v, want %v", tc.status, outcome, tc.want)
			}
			if out != nil {
				t.Fatalf("status %d returned out=%v, want nil", tc.status, out)
			}
			if calls != tc.calls {
				t.Fatalf("status %d made %d HTTP calls, want %d", tc.status, calls, tc.calls)
			}
		})
	}
}

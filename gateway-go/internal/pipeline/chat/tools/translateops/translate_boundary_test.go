package translateops

import (
	"context"
	"fmt"
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
	inputs := make([]translateInput, 45)
	for i := range inputs {
		inputs[i] = translateInput{Text: "x"}
	}
	want := []translateBatchRange{{Start: 0, End: 20}, {Start: 20, End: 40}, {Start: 40, End: 45}}
	if got := translateBatchRanges(inputs); !reflect.DeepEqual(got, want) {
		t.Fatalf("short input ranges = %#v, want %#v", got, want)
	}
	large := []translateInput{
		{Text: strings.Repeat("a", 700)},
		{Text: strings.Repeat("b", 500)},
		{Text: "c"},
	}
	want = []translateBatchRange{{Start: 0, End: 2}, {Start: 2, End: 3}}
	if got := translateBatchRanges(large); !reflect.DeepEqual(got, want) {
		t.Fatalf("char-bound ranges = %#v, want %#v", got, want)
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
	translateBatchFn = func(_ context.Context, batch []translateInput, _ string) ([]string, bool) {
		texts := make([]string, len(batch))
		for i := range batch {
			texts[i] = batch[i].Text
		}
		mu.Lock()
		calls = append(calls, texts)
		mu.Unlock()
		if len(batch) > 1 {
			return nil, false
		}
		return []string{"T:" + batch[0].Text}, true
	}
	inputs := []translateInput{{Text: "a"}, {Text: "b"}, {Text: "c"}, {Text: "d"}}
	out := []string{"a", "b", "c", "d"}
	translateRange(context.Background(), inputs, out, 0, len(inputs), "Korean")
	if !reflect.DeepEqual(out, []string{"T:a", "T:b", "T:c", "T:d"}) {
		t.Fatalf("translated output = %v", out)
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
	translateBatchFn = func(_ context.Context, batch []translateInput, _ string) ([]string, bool) {
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
		return out, true
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

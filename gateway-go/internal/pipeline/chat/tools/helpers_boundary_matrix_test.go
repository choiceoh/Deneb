package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/polaris"
)

func TestBoundaryGenericTruncateRuneMatrix(t *testing.T) {
	tests := []struct {
		name string
		text string
		max  int
		want string
	}{
		{name: "empty zero", text: "", max: 0, want: ""},
		{name: "ascii below", text: "ab", max: 3, want: "ab"},
		{name: "ascii exact", text: "abc", max: 3, want: "abc"},
		{name: "ascii over", text: "abcd", max: 3, want: "abc..."},
		{name: "Korean below", text: "가나", max: 3, want: "가나"},
		{name: "Korean exact", text: "가나다", max: 3, want: "가나다"},
		{name: "Korean over", text: "가나다라", max: 3, want: "가나다..."},
		{name: "emoji rune", text: "A📎B", max: 2, want: "A📎..."},
		{name: "newline rune", text: "a\nb", max: 2, want: "a\n..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := truncate(tt.text, tt.max); got != tt.want {
				t.Fatalf("truncate(%q, %d) = %q, want %q", tt.text, tt.max, got, tt.want)
			}
			if got := truncateRunes(tt.text, tt.max); !utf8.ValidString(got) {
				t.Fatalf("truncateRunes returned invalid UTF-8: %x", []byte(got))
			}
		})
	}
}

func TestBoundaryFormatBytesMatrix(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{bytes: -1, want: "-1 B"},
		{bytes: 0, want: "0 B"},
		{bytes: 1, want: "1 B"},
		{bytes: 999, want: "999 B"},
		{bytes: 1023, want: "1023 B"},
		{bytes: 1024, want: "1.0 KB"},
		{bytes: 1536, want: "1.5 KB"},
		{bytes: 1024 * 1024, want: "1.0 MB"},
		{bytes: 5 * 1024 * 1024, want: "5.0 MB"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("bytes_%d", tt.bytes), func(t *testing.T) {
			if got := formatBytes(tt.bytes); got != tt.want {
				t.Fatalf("formatBytes(%d) = %q, want %q", tt.bytes, got, tt.want)
			}
		})
	}
}

func TestBoundaryFirstLineMatrix(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{name: "empty", text: "", want: ""},
		{name: "one line", text: "one", want: "one"},
		{name: "one line preserves edges", text: "  one  ", want: "  one  "},
		{name: "newline trims first", text: "  one  \ntwo", want: "one"},
		{name: "empty first", text: "\ntwo", want: ""},
		{name: "CR retained before LF then trimmed", text: "one\r\ntwo", want: "one"},
		{name: "multiple lines", text: "first\nsecond\nthird", want: "first"},
		{name: "unicode", text: "  첫 줄 📎  \n둘째", want: "첫 줄 📎"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := firstLine(tt.text); got != tt.want {
				t.Fatalf("firstLine(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}

func TestBoundaryPolarisTimeRangeMatrix(t *testing.T) {
	location := time.FixedZone("KST", 9*60*60)
	now := time.Date(2026, 7, 11, 15, 30, 0, 0, location)
	nodes := []polaris.SummaryNode{
		{ID: 1, CreatedAt: now.AddDate(0, 0, -8).UnixMilli()},
		{ID: 2, CreatedAt: now.AddDate(0, 0, -7).UnixMilli()},
		{ID: 3, CreatedAt: time.Date(2026, 7, 11, 0, 0, 0, 0, location).UnixMilli()},
		{ID: 4, CreatedAt: now.UnixMilli()},
		{ID: 5, CreatedAt: now.Add(time.Hour).UnixMilli()},
	}
	tests := []struct {
		name      string
		rangeName string
		want      []int64
	}{
		{name: "empty means all", rangeName: "", want: []int64{1, 2, 3, 4, 5}},
		{name: "all", rangeName: "all", want: []int64{1, 2, 3, 4, 5}},
		{name: "unknown means all", rangeName: "month", want: []int64{1, 2, 3, 4, 5}},
		{name: "today inclusive midnight", rangeName: "today", want: []int64{3, 4, 5}},
		{name: "week inclusive cutoff", rangeName: "this_week", want: []int64{2, 3, 4, 5}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterByTimeRange(nodes, tt.rangeName, now)
			ids := make([]int64, len(got))
			for i := range got {
				ids[i] = got[i].ID
			}
			if !reflect.DeepEqual(ids, tt.want) {
				t.Fatalf("IDs = %v, want %v", ids, tt.want)
			}
		})
	}
}

func TestBoundarySerializeExpandMessagesBudgetMatrix(t *testing.T) {
	msgs := []toolport.ChatMessage{
		toolport.NewTextChatMessage("user", "hello", 1),
		toolport.NewTextChatMessage("assistant", "안녕하세요", 2),
		toolport.NewTextChatMessage("tool", "result", 3),
	}
	full := "[user]: hello\n\n[assistant]: 안녕하세요\n\n[tool]: result\n\n"
	tests := []struct {
		name string
		max  int
		want string
	}{
		{name: "large budget", max: 1000, want: full},
		{name: "exact full budget", max: len(full), want: full},
		{name: "first only", max: len("[user]: hello\n\n"), want: "[user]: hello\n\n... (나머지 3건 생략)\n"},
		{name: "zero budget", max: 0, want: "... (나머지 3건 생략)\n"},
		{name: "negative budget", max: -1, want: "... (나머지 3건 생략)\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := serializeExpandMessages(msgs, tt.max); got != tt.want {
				t.Fatalf("serializeExpandMessages(max=%d) = %q, want %q", tt.max, got, tt.want)
			}
		})
	}
	if got := serializeExpandMessages(nil, 100); got != "" {
		t.Fatalf("nil messages = %q", got)
	}
}

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

func TestBoundaryWorkFeedPriorityMatrix(t *testing.T) {
	tests := []struct {
		raw  string
		want int
	}{
		{raw: "", want: 0},
		{raw: "unknown", want: 0},
		{raw: "urgent", want: tooldeps.WorkFeedPriorityUrgent},
		{raw: " URGENT ", want: tooldeps.WorkFeedPriorityUrgent},
		{raw: "긴급", want: tooldeps.WorkFeedPriorityUrgent},
		{raw: "high", want: tooldeps.WorkFeedPriorityHigh},
		{raw: "높음", want: tooldeps.WorkFeedPriorityHigh},
		{raw: "normal", want: tooldeps.WorkFeedPriorityNormal},
		{raw: "보통", want: tooldeps.WorkFeedPriorityNormal},
		{raw: "low", want: tooldeps.WorkFeedPriorityLow},
		{raw: "낮음", want: tooldeps.WorkFeedPriorityLow},
		{raw: "highest", want: 0},
	}
	for _, tt := range tests {
		if got := workFeedPriority(tt.raw); got != tt.want {
			t.Fatalf("workFeedPriority(%q) = %d, want %d", tt.raw, got, tt.want)
		}
	}
}

func TestBoundaryWorkFeedTitleFallbackMatrix(t *testing.T) {
	tests := []struct {
		name string
		item tooldeps.WorkFeedItem
		want string
	}{
		{name: "title wins", item: tooldeps.WorkFeedItem{Title: " Title ", Summary: "summary", Body: "body"}, want: "Title"},
		{name: "summary fallback", item: tooldeps.WorkFeedItem{Summary: " summary line \nsecond", Body: "body"}, want: "summary line "},
		{name: "body fallback", item: tooldeps.WorkFeedItem{Body: " body line \nsecond"}, want: "body line "},
		{name: "empty", item: tooldeps.WorkFeedItem{}, want: ""},
		{name: "blank title skipped", item: tooldeps.WorkFeedItem{Title: " ", Summary: "summary"}, want: "summary"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := workFeedTitle(tt.item); got != tt.want {
				t.Fatalf("workFeedTitle = %q, want %q", got, tt.want)
			}
		})
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

func TestBoundaryToolPolarisRejectsMalformedAndUnknownActions(t *testing.T) {
	tool := ToolPolaris(nil, nil)
	if _, err := tool(context.Background(), json.RawMessage(`{`)); err == nil {
		t.Fatal("malformed input accepted")
	}
	for _, input := range []string{`{}`, `{"action":""}`, `{"action":"unknown"}`, `{"action":"SEARCH"}`} {
		got, err := tool(context.Background(), json.RawMessage(input))
		if err != nil {
			t.Fatalf("input %s: %v", input, err)
		}
		if !strings.Contains(got, "search, describe, expand") {
			t.Fatalf("input %s output = %q", input, got)
		}
	}
}

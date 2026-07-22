package streaming

import (
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// thinkingPreviewSink captures the `preview` of every chat.thinking frame and
// signals `hit` once (via once) when a frame carrying `want` arrives.
func thinkingPreviewSink(dst *[]string, mu *sync.Mutex, want string, hit chan<- struct{}, once *sync.Once, subscribers int) BroadcastRawFunc {
	return func(event string, data []byte) int {
		if event != EventThinking {
			return subscribers
		}
		var m struct {
			Payload struct {
				Preview string `json:"preview"`
			} `json:"payload"`
		}
		_ = json.Unmarshal(data, &m)
		mu.Lock()
		*dst = append(*dst, m.Payload.Preview)
		mu.Unlock()
		if want != "" && m.Payload.Preview == want {
			once.Do(func() { close(hit) })
		}
		return subscribers
	}
}

// With a summarizer wired, the chip never shows the raw reasoning peek: the first
// frame is a bare "깊이 생각 중" (empty preview) and the async Korean summary
// supersedes it. The raw reasoning is often the model's zh/en self-talk, so
// leaking it — even for one window — defeats the Korean summary.
func TestEmitThinkingSummarizerSuppressesRawPeek(t *testing.T) {
	var mu sync.Mutex
	var previews []string
	hit := make(chan struct{})
	var once sync.Once
	const summary = "지난 거래 대조 중"

	sb := NewBroadcaster(thinkingPreviewSink(&previews, &mu, summary, hit, &once, 1), "s1", "r1")
	sb.SetThinkingSummarizer(func(tail, _ string) (string, bool) {
		if tail == "" {
			return "", false
		}
		return summary, true
	})

	sb.EmitThinking("발신자 주소를 확인하고 과거 거래 이력을 대조해야 한다. 계좌 변경 여부를 본다. ")

	select {
	case <-hit:
	case <-time.After(2 * time.Second):
		t.Fatal("summary preview was never emitted")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(previews) < 2 {
		t.Fatalf("want at least a bare frame + summary, got %v", previews)
	}
	if previews[0] != "" {
		t.Fatalf("first frame carried preview %q; want a bare chip (no raw reasoning peek)", previews[0])
	}
	if last := previews[len(previews)-1]; last != summary {
		t.Fatalf("last preview = %q, want summary %q (all: %v)", last, summary, previews)
	}
	for _, p := range previews {
		if p != "" && p != summary {
			t.Fatalf("unexpected preview %q — only a bare chip or the summary is allowed, never raw reasoning (all: %v)", p, previews)
		}
	}
}

// Once a summary has landed, a later throttle window renders it as the steady
// chip synchronously — the raw reasoning preview must not flash back in between
// summaries (the "요약 사이에 추론이 겹쳐 금방 지나감" readability bug).
func TestEmitThinkingSteadySummaryNoRawFlash(t *testing.T) {
	var mu sync.Mutex
	var previews []string
	hit := make(chan struct{})
	var once sync.Once
	const summary = "지난 3개월 거래 내역과 계좌 변경 여부를 대조하는 중"

	sb := NewBroadcaster(thinkingPreviewSink(&previews, &mu, summary, hit, &once, 1), "s1", "r1")
	sb.SetThinkingSummarizer(func(_, _ string) (string, bool) { return summary, true })

	sb.EmitThinking("발신자 주소를 확인하고 과거 거래 이력을 대조해야 한다. 계좌 변경 여부를 본다. ")
	select {
	case <-hit:
	case <-time.After(2 * time.Second):
		t.Fatal("summary preview was never emitted")
	}

	mu.Lock()
	before := len(previews)
	mu.Unlock()

	// Reopen the throttle window (same package → can poke the unexported gate) and
	// pulse again; the synchronous frame this produces must carry the stored
	// summary, not a raw reasoning peek.
	sb.lastThinkingNs.Store(0)
	sb.EmitThinking("계좌 변경 여부를 확인하는 중이다. 이어서 첨부 파일을 검토한다. ")

	mu.Lock()
	defer mu.Unlock()
	if len(previews) <= before {
		t.Fatalf("second pulse emitted no synchronous frame (previews: %v)", previews)
	}
	if previews[before] != summary {
		t.Fatalf("second-window sync frame = %q, want steady summary %q (all: %v)", previews[before], summary, previews)
	}
}

// The advance gate makes a landed summary linger: a second window with only a
// sliver of new reasoning must NOT re-run the tiny model (a calmer chip, no
// paraphrasing an unchanged tail).
func TestEmitThinkingAdvanceGateLingersSummary(t *testing.T) {
	var calls atomic.Int64
	var mu sync.Mutex
	var previews []string
	hit := make(chan struct{})
	var once sync.Once
	const summary = "지난 거래 대조 중"

	sb := NewBroadcaster(thinkingPreviewSink(&previews, &mu, summary, hit, &once, 1), "s1", "r1")
	sb.SetThinkingSummarizer(func(_, _ string) (string, bool) {
		calls.Add(1)
		return summary, true
	})

	// First window: plenty of new reasoning → the summarizer runs and lands.
	sb.EmitThinking("발신자 주소를 확인하고 과거 거래 이력을 대조해야 한다. 계좌 변경 여부를 확인한다. ")
	select {
	case <-hit:
	case <-time.After(2 * time.Second):
		t.Fatal("first summary never landed")
	}

	// Reopen the window but add only a sliver of new reasoning (< advance gate).
	sb.lastThinkingNs.Store(0)
	sb.EmitThinking("음 ")

	time.Sleep(100 * time.Millisecond) // let any erroneously-spawned summary run
	if got := calls.Load(); got != 1 {
		t.Fatalf("summarizer ran %d times; want 1 — a barely-advanced tail must not re-run it", got)
	}
}

// EmitToolStart's action context reaches the summarizer, so the progress line can
// name the real action instead of only the model's internal musing.
func TestEmitThinkingSummarizerReceivesRecentTools(t *testing.T) {
	gotTools := make(chan string, 1)
	sb := NewBroadcaster(func(string, []byte) int { return 1 }, "s1", "r1")
	sb.SetThinkingSummarizer(func(_, tools string) (string, bool) {
		select {
		case gotTools <- tools:
		default:
		}
		return "요약", true
	})

	sb.EmitToolStart("web_search", "t1", "삼성전자 실적")
	sb.EmitThinking("검색 결과를 살펴보고 지난 거래와 대조해야 한다. 계좌 변경 여부를 확인한다. ")

	select {
	case tools := <-gotTools:
		if !strings.Contains(tools, "web_search") || !strings.Contains(tools, "삼성전자 실적") {
			t.Fatalf("summarizer tools = %q; want it to include the recent tool name + detail", tools)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("summarizer was never called")
	}
}

// With no subscribers (broadcastRaw returns 0), the model summarizer must not be
// spent on an unwatched run.
func TestEmitThinkingSummarizerSkippedWithoutSubscribers(t *testing.T) {
	called := make(chan struct{}, 1)
	sb := NewBroadcaster(func(string, []byte) int { return 0 }, "s1", "r1")
	sb.SetThinkingSummarizer(func(_, _ string) (string, bool) {
		select {
		case called <- struct{}{}:
		default:
		}
		return "x", true
	})

	sb.EmitThinking("추론 텍스트를 충분히 길게 넣어서 최소 길이 게이트를 넘긴다. 다음 단계로 넘어간다. ")

	select {
	case <-called:
		t.Fatal("summarizer was called despite zero subscribers")
	case <-time.After(150 * time.Millisecond):
	}
}

// A nil summarizer leaves the deterministic preview exactly as before: one frame,
// no async refinement.
func TestEmitThinkingNoSummarizerKeepsDeterministicChip(t *testing.T) {
	var mu sync.Mutex
	var previews []string
	sb := NewBroadcaster(thinkingPreviewSink(&previews, &mu, "", nil, &sync.Once{}, 1), "s1", "r1")

	sb.EmitThinking("발신자 주소부터 확인해야 합니다. 계좌가 바뀌었다. ")
	time.Sleep(50 * time.Millisecond) // no async frame should appear

	mu.Lock()
	defer mu.Unlock()
	if len(previews) != 1 {
		t.Fatalf("want exactly one deterministic frame, got %v", previews)
	}
	if previews[0] == "" {
		t.Fatalf("deterministic preview should be non-empty for a completed sentence")
	}
}

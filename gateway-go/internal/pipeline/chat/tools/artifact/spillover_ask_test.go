package artifact

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
)

// askRecorder is a stub local model that records what it was asked.
type askRecorder struct {
	mu      sync.Mutex
	prompts []string
	reply   func(user string) (string, error)
}

func (r *askRecorder) fn() tooldeps.LocalAIFunc {
	return func(_ context.Context, _, user string, _ int) (string, error) {
		r.mu.Lock()
		r.prompts = append(r.prompts, user)
		r.mu.Unlock()
		return r.reply(user)
	}
}

func (r *askRecorder) calls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.prompts...)
}

func spillWithLines(t *testing.T, n int) (tooldeps.SpilloverStore, context.Context, string) {
	t.Helper()
	store := agent.NewSpilloverStore(t.TempDir())
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "line %d payload\n", i)
	}
	id, err := store.Store("client:test", "exec", b.String())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	return store, toolport.WithSessionKey(context.Background(), "client:test"), id
}

// The whole point of the question path is that the blob never reaches the root
// context: the tool must return the delegate's answer, not the source lines.
func TestSpilloverQuestionReturnsAnswerNotBlob(t *testing.T) {
	store, ctx, id := spillWithLines(t, 200)
	rec := &askRecorder{reply: func(string) (string, error) {
		return "설정값은 42다 [L7]", nil
	}}
	fn := ToolSpilloverRead(store, rec.fn())

	out := callSpill(ctx, t, fn, map[string]any{"spill_id": id, "question": "설정값이 뭐야?"})

	if !strings.Contains(out, "설정값은 42다 [L7]") {
		t.Fatalf("delegate answer missing:\n%s", out)
	}
	if strings.Contains(out, "line 150 payload") {
		t.Errorf("blob body leaked into the answer — the root was supposed to never see it:\n%s", out)
	}
	if !strings.Contains(out, "read_spillover") || !strings.Contains(out, "offset") {
		t.Errorf("answer must tell the root how to verify a cited line:\n%s", out)
	}
	if len(rec.calls()) == 0 {
		t.Fatal("delegate was never called")
	}
	if !strings.Contains(rec.calls()[0], "설정값이 뭐야?") {
		t.Errorf("question not forwarded to the delegate: %q", rec.calls()[0])
	}
}

// Chunks are line-numbered so the delegate can cite positions the root can
// re-open with offset. An answer the root cannot verify is worse than a page.
func TestSpilloverQuestionChunksCarryLineNumbers(t *testing.T) {
	store, ctx, id := spillWithLines(t, 100)
	rec := &askRecorder{reply: func(string) (string, error) { return "답", nil }}
	fn := ToolSpilloverRead(store, rec.fn())

	callSpill(ctx, t, fn, map[string]any{"spill_id": id, "question": "q"})

	calls := rec.calls()
	if len(calls) == 0 {
		t.Fatal("delegate was never called")
	}
	if !strings.Contains(calls[0], "1: line 1 payload") {
		t.Errorf("chunk must prefix each line with its 1-based number:\n%s", calls[0])
	}
}

// A blob too large for the fan-out is covered partially. That is acceptable —
// silently presenting it as a complete reading is not.
func TestSpilloverQuestionStatesPartialCoverage(t *testing.T) {
	store, ctx, id := spillWithLines(t, 20000) // far beyond 4 chunks × 12K chars
	rec := &askRecorder{reply: func(string) (string, error) { return "부분 답", nil }}
	fn := ToolSpilloverRead(store, rec.fn())

	out := callSpill(ctx, t, fn, map[string]any{"spill_id": id, "question": "q"})

	if !strings.Contains(out, "근거 범위") {
		t.Fatalf("coverage line missing:\n%s", out)
	}
	if !strings.Contains(out, "전체를 다 보지는 않았습니다") {
		t.Errorf("partial scan must be stated, not implied:\n%s", out)
	}
	if got := len(rec.calls()); got > spillAskMaxChunks+1 { // +1 for the reduce pass
		t.Errorf("fan-out exceeded its bound: %d calls", got)
	}
}

// A dead delegate must not fail the read — it degrades to paging, and says so
// rather than returning page 1 as if it had answered.
func TestSpilloverQuestionFallsBackToPagingOnDelegateFailure(t *testing.T) {
	store, ctx, id := spillWithLines(t, 50)
	rec := &askRecorder{reply: func(string) (string, error) {
		return "", errors.New("hub down")
	}}
	fn := ToolSpilloverRead(store, rec.fn())

	out := callSpill(ctx, t, fn, map[string]any{"spill_id": id, "question": "q"})

	if !strings.Contains(out, "위임 실패") {
		t.Errorf("silent fallback hides that the question was never answered:\n%s", out)
	}
	if !strings.Contains(out, "line 1 payload") {
		t.Errorf("fallback should still return a readable page:\n%s", out)
	}
}

// With no delegate wired at all the tool stays exactly as it was.
func TestSpilloverQuestionWithoutDelegatePages(t *testing.T) {
	store, ctx, id := spillWithLines(t, 50)
	fn := ToolSpilloverRead(store, nil)

	out := callSpill(ctx, t, fn, map[string]any{"spill_id": id, "question": "q"})

	if !strings.Contains(out, "line 1 payload") {
		t.Errorf("paging-only mode must still serve the page:\n%s", out)
	}
	if strings.Contains(out, "위임 답변") {
		t.Errorf("no delegate wired but an answer was claimed:\n%s", out)
	}
}

// One failing chunk must not sink the read: the surviving partials are still
// evidence.
func TestSpilloverQuestionSurvivesPartialChunkFailure(t *testing.T) {
	store, ctx, id := spillWithLines(t, 4000)
	var n int
	var mu sync.Mutex
	rec := &askRecorder{reply: func(string) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		n++
		if n == 1 {
			return "", errors.New("transient")
		}
		return "살아남은 답 [L900]", nil
	}}
	fn := ToolSpilloverRead(store, rec.fn())

	out := callSpill(ctx, t, fn, map[string]any{"spill_id": id, "question": "q"})

	if !strings.Contains(out, "살아남은 답") {
		t.Fatalf("surviving chunk answers were dropped:\n%s", out)
	}
}

// spillAskChunks must not exceed its bounds — this is the cost ceiling an
// interactive turn depends on.
func TestSpillAskChunksBounded(t *testing.T) {
	lines := make([]string, 50000)
	for i := range lines {
		lines[i] = strings.Repeat("x", 40)
	}

	chunks := spillAskChunks(lines)

	if len(chunks) > spillAskMaxChunks {
		t.Fatalf("chunk count %d exceeds bound %d", len(chunks), spillAskMaxChunks)
	}
	for i, c := range chunks {
		if len(c.text) > spillAskChunkMaxChars+64 {
			t.Errorf("chunk %d is %d chars, over the %d bound", i, len(c.text), spillAskChunkMaxChars)
		}
		if c.firstLine < 1 || c.lastLine < c.firstLine {
			t.Errorf("chunk %d has a malformed line range %d–%d", i, c.firstLine, c.lastLine)
		}
	}
}

// Empty input must not produce a phantom chunk (and thus a delegate call over
// nothing).
func TestSpillAskChunksEmpty(t *testing.T) {
	if got := spillAskChunks(nil); len(got) != 0 {
		t.Fatalf("nil lines produced %d chunks", len(got))
	}
}

// A malformed question payload must be rejected like any other bad input.
func TestSpilloverQuestionRejectsMalformedInput(t *testing.T) {
	store, ctx, _ := spillWithLines(t, 10)
	fn := ToolSpilloverRead(store, (&askRecorder{reply: func(string) (string, error) { return "x", nil }}).fn())

	if _, err := fn(ctx, json.RawMessage(`{`)); err == nil {
		t.Fatal("malformed JSON accepted")
	}
}

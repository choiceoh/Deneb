package artifact

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
)

// spillFixture stores numbered lines ("line 1".."line N") and returns the tool
// plus a session-decorated ctx (spillover access is session-scoped) and the ID.
func spillFixture(t *testing.T, n int) (toolport.ToolFunc, context.Context, string) {
	t.Helper()
	store := agent.NewSpilloverStore(t.TempDir())
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	id, err := store.Store("client:test", "exec", b.String())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	ctx := toolport.WithSessionKey(context.Background(), "client:test")
	return ToolSpilloverRead(store, nil), ctx, id
}

func callSpill(ctx context.Context, t *testing.T, fn toolport.ToolFunc, params map[string]any) string {
	t.Helper()
	raw, _ := json.Marshal(params)
	out, err := fn(ctx, raw)
	if err != nil {
		t.Fatalf("read_spillover %v: %v", params, err)
	}
	return out
}

// Default read is a bounded page with a continuation hint — never the whole
// blob (which spilled precisely because it was too large).
func TestSpilloverRead_DefaultPageAndContinuation(t *testing.T) {
	fn, ctx, id := spillFixture(t, 1000)

	out := callSpill(ctx, t, fn, map[string]any{"spill_id": id})
	if !strings.Contains(out, "1–400줄 표시") {
		t.Errorf("expected default 400-line page header, got head: %.120s", out)
	}
	if !strings.Contains(out, "line 400\n") || strings.Contains(out, "line 401\n") {
		t.Errorf("page must end at line 400")
	}
	if !strings.Contains(out, "offset=401") {
		t.Errorf("expected continuation hint offset=401")
	}

	// Second page picks up where the hint points.
	out = callSpill(ctx, t, fn, map[string]any{"spill_id": id, "offset": 401, "limit": 100})
	if !strings.Contains(out, "401–500줄 표시") || !strings.Contains(out, "line 401") {
		t.Errorf("expected 401–500 window, got head: %.120s", out)
	}
	// Out-of-range offset gives guidance, not an error.
	out = callSpill(ctx, t, fn, map[string]any{"spill_id": id, "offset": 5000})
	if !strings.Contains(out, "범위 밖") {
		t.Errorf("expected out-of-range guidance, got: %.120s", out)
	}
}

// grep returns matching lines with line numbers so the model can jump to the
// region via offset.
func TestSpilloverRead_Grep(t *testing.T) {
	fn, ctx, id := spillFixture(t, 1000)

	out := callSpill(ctx, t, fn, map[string]any{"spill_id": id, "grep": `^line 77\d$`})
	if !strings.Contains(out, "매치 10줄") {
		t.Errorf("expected 10 matches (770-779), got head: %.160s", out)
	}
	if !strings.Contains(out, "770: line 770") {
		t.Errorf("expected numbered match lines, got: %.200s", out)
	}
	// Invalid regex → guidance string, not a hard error.
	out = callSpill(ctx, t, fn, map[string]any{"spill_id": id, "grep": "["})
	if !strings.Contains(out, "잘못") {
		t.Errorf("expected invalid-regex guidance, got: %.120s", out)
	}
	// No match → clean message.
	out = callSpill(ctx, t, fn, map[string]any{"spill_id": id, "grep": "없는패턴"})
	if !strings.Contains(out, "매치 없음") {
		t.Errorf("expected no-match message, got: %.120s", out)
	}
}

// The page respects the char budget even when limit asks for more lines.
func TestSpilloverRead_CharBudget(t *testing.T) {
	store := agent.NewSpilloverStore(t.TempDir())
	wide := strings.Repeat(strings.Repeat("가", 1000)+"\n", 100) // ~3KB/line × 100
	id, err := store.Store("client:test", "exec", wide)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	fn := ToolSpilloverRead(store, nil)
	ctx := toolport.WithSessionKey(context.Background(), "client:test")

	out := callSpill(ctx, t, fn, map[string]any{"spill_id": id, "limit": 100})
	if len(out) > spillPageMaxChars+2000 {
		t.Errorf("page exceeded char budget: %d chars", len(out))
	}
	if !strings.Contains(out, "[계속:") {
		t.Errorf("char-budget stop must still emit continuation hint")
	}
}

// Access from a different session is refused (unchanged behavior).
func TestSpilloverRead_SessionScoped(t *testing.T) {
	fn, _, id := spillFixture(t, 10)
	other := toolport.WithSessionKey(context.Background(), "client:other")
	raw, _ := json.Marshal(map[string]any{"spill_id": id})
	if _, err := fn(other, raw); err == nil {
		t.Fatal("expected cross-session access to be refused")
	}
}

// A single line larger than the page budget must be clipped. The budget check
// only bites once something is buffered, so an oversized first line would go
// out whole, blow the tool-output cap, and be re-spilled — handing the model a
// new pointer instead of a page, and chaining spills on every retry.
func TestSpillPageClipsOversizedSingleLine(t *testing.T) {
	store := agent.NewSpilloverStore(t.TempDir())
	huge := strings.Repeat("j", spillPageMaxChars*5)
	id, err := store.Store("client:test", "exec", huge+"\nsecond line\n")
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	ctx := toolport.WithSessionKey(context.Background(), "client:test")
	fn := ToolSpilloverRead(store, nil)

	out := callSpill(ctx, t, fn, map[string]any{"spill_id": id})

	if len(out) > spillPageMaxChars+2000 {
		t.Errorf("page returned %d chars — oversized line escaped the budget", len(out))
	}
	if !strings.Contains(out, "잘렸습니다") {
		t.Errorf("clipped line must say so:\n%s", out[:min(600, len(out))])
	}
}

// grep must clip an oversized match, not skip it. Skipping made the page
// renderer's "search inside this line with grep" hint a dead end: the only
// route into a long line's content refused to emit any of it.
func TestSpillGrepClipsRatherThanSkipsOversizedMatch(t *testing.T) {
	store := agent.NewSpilloverStore(t.TempDir())
	huge := "NEEDLE" + strings.Repeat("q", spillPageMaxChars*4)
	id, err := store.Store("client:test", "exec", huge+"\n")
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	ctx := toolport.WithSessionKey(context.Background(), "client:test")
	fn := ToolSpilloverRead(store, nil)

	out := callSpill(ctx, t, fn, map[string]any{"spill_id": id, "grep": "NEEDLE"})

	if !strings.Contains(out, "NEEDLE") {
		t.Fatalf("oversized match was skipped — the recovery path is a dead end:\n%s", out)
	}
	if !strings.Contains(out, "생략]") {
		t.Errorf("clipped match must say what was dropped:\n%s", out[:min(500, len(out))])
	}
	if len(out) > spillPageMaxChars+2000 {
		t.Errorf("grep result %d chars — clip did not bound the match", len(out))
	}
}

// A match sitting past the kept prefix must still be visible. Head-truncating
// while matching the full line reported a hit whose text the model could not
// see — and searching inside a long line is the whole reason this path exists.
func TestSpillGrepShowsMatchesPastTheHead(t *testing.T) {
	store := agent.NewSpilloverStore(t.TempDir())
	// The needle sits far past spillGrepLineMaxChars.
	line := strings.Repeat("q", spillGrepLineMaxChars*3) + "NEEDLE" + strings.Repeat("z", 5000)
	id, err := store.Store("client:test", "exec", line+"\n")
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	ctx := toolport.WithSessionKey(context.Background(), "client:test")
	fn := ToolSpilloverRead(store, nil)

	out := callSpill(ctx, t, fn, map[string]any{"spill_id": id, "grep": "NEEDLE"})

	if !strings.Contains(out, "NEEDLE") {
		t.Fatalf("match past the head was reported but not shown:\n%s", out[:min(600, len(out))])
	}
	if !strings.Contains(out, "앞 ") {
		t.Errorf("window must say how much was dropped before the match:\n%s", out[:min(600, len(out))])
	}
	if len(out) > spillPageMaxChars+2000 {
		t.Errorf("grep result %d chars — window did not bound the match", len(out))
	}
}

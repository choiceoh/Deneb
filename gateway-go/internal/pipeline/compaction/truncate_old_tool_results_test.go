package compaction

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// toolResultMsg + textMsg helpers come from polaris_test.go (same package).

func assistantMsg(t *testing.T, text string) llm.Message {
	t.Helper()
	blocks := []llm.ContentBlock{{Type: "text", Text: text}}
	raw, err := json.Marshal(blocks)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return llm.Message{Role: "assistant", Content: llm.FlexibleFromRaw(raw)}
}

func firstToolResultContent(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var blocks []llm.ContentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, b := range blocks {
		if b.Type == "tool_result" {
			return b.Content
		}
	}
	return ""
}

const stubPlaceholder = "[older tool output cleared to save context]"

func TestTruncateOldToolResults_StubsOversizedContent(t *testing.T) {
	long := strings.Repeat("a", 500)
	messages := []llm.Message{
		assistantMsg(t, "a1"),
		toolResultMsg(long),
		assistantMsg(t, "a2"),
		assistantMsg(t, "a3"),
		assistantMsg(t, "a4"),
		assistantMsg(t, "a5"),
	}
	out, stubbed := TruncateOldToolResults(messages, 4, 256)
	if stubbed != 1 {
		t.Errorf("stubbed = %d, want 1", stubbed)
	}
	if got := firstToolResultContent(t, json.RawMessage(out[1].Content.Bytes())); got != stubPlaceholder {
		t.Errorf("content = %q, want placeholder", got)
	}
}

// TestTruncateOldToolResults_KeepsActivationNotice: a stubbed result that
// carried a deferred-tool activation notice keeps the notice line in the stub,
// so the next run's history replay (chat/deferred_replay.go) still sees the
// activation evidence after cheap pruning.
func TestTruncateOldToolResults_KeepsActivationNotice(t *testing.T) {
	long := strings.Repeat("본문 ", 200) +
		"\n\n[스킬 필요 도구 활성화: graphify — 스키마가 로드되어 fetch_tools 없이 바로 호출할 수 있습니다.]"
	messages := []llm.Message{
		assistantMsg(t, "a1"),
		toolResultMsg(long),
		assistantMsg(t, "a2"),
		assistantMsg(t, "a3"),
		assistantMsg(t, "a4"),
		assistantMsg(t, "a5"),
	}
	out, stubbed := TruncateOldToolResults(messages, 4, 256)
	if stubbed != 1 {
		t.Fatalf("stubbed = %d, want 1", stubbed)
	}
	got := firstToolResultContent(t, json.RawMessage(out[1].Content.Bytes()))
	if !strings.HasPrefix(got, stubPlaceholder) {
		t.Errorf("stub must lead with the placeholder, got %q", got)
	}
	if !strings.Contains(got, "[스킬 필요 도구 활성화: graphify — ") {
		t.Errorf("stub must keep the activation notice, got %q", got)
	}
	if strings.Contains(got, "본문") {
		t.Errorf("stub must not keep the body, got %q", got)
	}
}

func TestTruncateOldToolResults_PreservesShortContent(t *testing.T) {
	short := strings.Repeat("a", 100)
	messages := []llm.Message{
		assistantMsg(t, "a1"),
		toolResultMsg(short),
		assistantMsg(t, "a2"),
		assistantMsg(t, "a3"),
		assistantMsg(t, "a4"),
		assistantMsg(t, "a5"),
	}
	out, stubbed := TruncateOldToolResults(messages, 4, 256)
	if stubbed != 0 {
		t.Errorf("stubbed = %d, want 0", stubbed)
	}
	if got := firstToolResultContent(t, json.RawMessage(out[1].Content.Bytes())); got != short {
		t.Errorf("short content modified: %q", got)
	}
}

func TestTruncateOldToolResults_ProtectsRecentTurns(t *testing.T) {
	long := strings.Repeat("z", 500)
	// tool_result sits AFTER a3 — among the last 4 assistant turns, must be preserved.
	messages := []llm.Message{
		assistantMsg(t, "a1"),
		assistantMsg(t, "a2"),
		assistantMsg(t, "a3"),
		toolResultMsg(long),
		assistantMsg(t, "a4"),
	}
	out, stubbed := TruncateOldToolResults(messages, 4, 256)
	if stubbed != 0 {
		t.Errorf("stubbed = %d, want 0 (within protected tail)", stubbed)
	}
	if got := firstToolResultContent(t, json.RawMessage(out[3].Content.Bytes())); got != long {
		t.Errorf("recent content modified")
	}
}

func TestTruncateOldToolResults_NoOpWhenInsufficientTurns(t *testing.T) {
	long := strings.Repeat("a", 500)
	messages := []llm.Message{
		assistantMsg(t, "a1"),
		toolResultMsg(long),
		assistantMsg(t, "a2"),
	}
	_, stubbed := TruncateOldToolResults(messages, 4, 256)
	if stubbed != 0 {
		t.Errorf("stubbed = %d, want 0 (only 2 assistant turns)", stubbed)
	}
}

func TestTruncateOldToolResults_HandlesMultipleBlocksInMessage(t *testing.T) {
	long := strings.Repeat("a", 500)
	short := "ok"
	blocks := []llm.ContentBlock{
		{Type: "tool_result", ToolUseID: "t1", Content: long},
		{Type: "tool_result", ToolUseID: "t2", Content: short},
		{Type: "tool_result", ToolUseID: "t3", Content: long},
	}
	raw, _ := json.Marshal(blocks)
	messages := []llm.Message{
		assistantMsg(t, "a1"),
		{Role: "user", Content: llm.FlexibleFromRaw(raw)},
		assistantMsg(t, "a2"),
		assistantMsg(t, "a3"),
		assistantMsg(t, "a4"),
		assistantMsg(t, "a5"),
	}
	out, stubbed := TruncateOldToolResults(messages, 4, 256)
	if stubbed != 2 {
		t.Errorf("stubbed = %d, want 2", stubbed)
	}
	var got []llm.ContentBlock
	if err := json.Unmarshal(out[1].Content.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got[0].Content != stubPlaceholder {
		t.Errorf("block 0 not stubbed: %q", got[0].Content)
	}
	if got[1].Content != short {
		t.Errorf("block 1 (short) modified: %q", got[1].Content)
	}
	if got[2].Content != stubPlaceholder {
		t.Errorf("block 2 not stubbed: %q", got[2].Content)
	}
}

func TestTruncateOldToolResults_NonToolResultBlocksUntouched(t *testing.T) {
	long := strings.Repeat("a", 500)
	blocks := []llm.ContentBlock{
		{Type: "text", Text: long},
		{Type: "tool_use", ID: "t1", Name: "exec", Input: llm.FlexibleFromRaw([]byte(`{"cmd":"echo"}`))},
	}
	raw, _ := json.Marshal(blocks)
	messages := []llm.Message{
		assistantMsg(t, "a1"),
		{Role: "assistant", Content: llm.FlexibleFromRaw(raw)},
		assistantMsg(t, "a2"),
		assistantMsg(t, "a3"),
		assistantMsg(t, "a4"),
		assistantMsg(t, "a5"),
		assistantMsg(t, "a6"),
	}
	_, stubbed := TruncateOldToolResults(messages, 4, 256)
	if stubbed != 0 {
		t.Errorf("stubbed = %d, want 0 (no tool_result blocks)", stubbed)
	}
}

func TestTruncateOldToolResults_CJKRunesNotBytes(t *testing.T) {
	// 한글 200자 ≈ 600 bytes. Threshold 256 RUNES → not stubbed.
	korean200 := strings.Repeat("가", 200)
	messages := []llm.Message{
		assistantMsg(t, "a1"),
		toolResultMsg(korean200),
		assistantMsg(t, "a2"),
		assistantMsg(t, "a3"),
		assistantMsg(t, "a4"),
		assistantMsg(t, "a5"),
	}
	out, stubbed := TruncateOldToolResults(messages, 4, 256)
	if stubbed != 0 {
		t.Errorf("stubbed = %d, want 0 (200 runes < 256)", stubbed)
	}
	if got := firstToolResultContent(t, json.RawMessage(out[1].Content.Bytes())); got != korean200 {
		t.Errorf("Korean short content modified")
	}

	// 한글 300자 → over rune threshold.
	korean300 := strings.Repeat("나", 300)
	messages2 := []llm.Message{
		assistantMsg(t, "a1"),
		toolResultMsg(korean300),
		assistantMsg(t, "a2"),
		assistantMsg(t, "a3"),
		assistantMsg(t, "a4"),
		assistantMsg(t, "a5"),
	}
	out2, stubbed2 := TruncateOldToolResults(messages2, 4, 256)
	if stubbed2 != 1 {
		t.Errorf("stubbed = %d, want 1 (300 runes > 256)", stubbed2)
	}
	if got := firstToolResultContent(t, json.RawMessage(out2[1].Content.Bytes())); got != stubPlaceholder {
		t.Errorf("Korean long content not stubbed: %q", got)
	}
}

// TestTruncateOldToolResults_CJKRuneBoundary pins the exact 255/256/257-rune
// boundary (improvement-ideas §3.2): minChars gates on rune count (<= survives,
// > is stubbed), never on byte length, and the counter is code-point-based so it
// stays consistent for both NFC and NFD Hangul (a decomposed syllable is 2
// code points). Mixed Hangul+ASCII at the boundary is covered too.
func TestTruncateOldToolResults_CJKRuneBoundary(t *testing.T) {
	cases := []struct {
		name     string
		content  string
		runes    int
		wantStub bool
	}{
		{"NFC_255", strings.Repeat("가", 255), 255, false},
		{"NFC_256", strings.Repeat("가", 256), 256, false},
		{"NFC_257", strings.Repeat("가", 257), 257, true},
		{"mixed_256", strings.Repeat("가", 128) + strings.Repeat("a", 128), 256, false},
		{"mixed_257", strings.Repeat("가", 129) + strings.Repeat("a", 128), 257, true},
		// NFD "가" = U+1100 U+1161 (2 code points), so 128 syllables = 256 runes.
		{"NFD_256", strings.Repeat("\u1100\u1161", 128), 256, false},
		{"NFD_257", strings.Repeat("\u1100\u1161", 129), 258, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := utf8.RuneCountInString(tc.content); got != tc.runes {
				t.Fatalf("fixture rune count = %d, want %d", got, tc.runes)
			}
			msgs := []llm.Message{
				assistantMsg(t, "a1"),
				toolResultMsg(tc.content),
				assistantMsg(t, "a2"),
				assistantMsg(t, "a3"),
				assistantMsg(t, "a4"),
				assistantMsg(t, "a5"),
			}
			out, stubbed := TruncateOldToolResults(msgs, 4, 256)
			got := firstToolResultContent(t, json.RawMessage(out[1].Content.Bytes()))
			if tc.wantStub {
				if stubbed != 1 {
					t.Fatalf("stubbed = %d, want 1", stubbed)
				}
				if got != stubPlaceholder {
					t.Fatalf("content not stubbed at boundary: %q", got)
				}
			} else {
				if stubbed != 0 {
					t.Fatalf("stubbed = %d, want 0", stubbed)
				}
				if got != tc.content {
					t.Fatalf("content modified below boundary (byte-identity broken)")
				}
			}
		})
	}
}

func TestTruncateOldToolResults_DoesNotMutateInput(t *testing.T) {
	long := strings.Repeat("x", 500)
	messages := []llm.Message{
		assistantMsg(t, "a1"),
		toolResultMsg(long),
		assistantMsg(t, "a2"),
		assistantMsg(t, "a3"),
		assistantMsg(t, "a4"),
		assistantMsg(t, "a5"),
	}
	originalContent := messages[1].Content.String()
	_, _ = TruncateOldToolResults(messages, 4, 256)
	if messages[1].Content.String() != originalContent {
		t.Errorf("input mutated; placeholder leaked into source slice")
	}
}

// TestTruncateOldToolResults_PreservesSpilloverPointer: a spilled tool result's
// full output still lives on disk, reachable via its read_spillover("sp_…")
// pointer. Stubbing must KEEP that pointer, or the spill file is stranded — the
// agent can no longer page through the output it was told to read_spillover.
func TestTruncateOldToolResults_PreservesSpilloverPointer(t *testing.T) {
	head := strings.Repeat("x", 300)
	spilled := head + "\n\n... [149798 lines truncated — use read_spillover(\"sp_c0953cb7\") for full content] ...\n\n" + head
	messages := []llm.Message{
		assistantMsg(t, "a1"),
		toolResultMsg(spilled),
		assistantMsg(t, "a2"),
		assistantMsg(t, "a3"),
		assistantMsg(t, "a4"),
		assistantMsg(t, "a5"),
	}
	out, stubbed := TruncateOldToolResults(messages, 4, 256)
	if stubbed != 1 {
		t.Fatalf("expected 1 stub, got %d", stubbed)
	}
	got := firstToolResultContent(t, json.RawMessage(out[1].Content.Bytes()))
	if !strings.Contains(got, `read_spillover("sp_c0953cb7")`) {
		t.Fatalf("spillover pointer stripped from stub (spill file stranded): %q", got)
	}
	if len(got) >= len(spilled) {
		t.Fatalf("expected the stub to shrink the content, got %d chars (was %d)", len(got), len(spilled))
	}
	// A non-spilled oversized result still gets the bare placeholder.
	plain := []llm.Message{
		assistantMsg(t, "a1"), toolResultMsg(strings.Repeat("z", 500)),
		assistantMsg(t, "a2"), assistantMsg(t, "a3"), assistantMsg(t, "a4"), assistantMsg(t, "a5"),
	}
	outP, _ := TruncateOldToolResults(plain, 4, 256)
	if firstToolResultContent(t, json.RawMessage(outP[1].Content.Bytes())) != stubPlaceholder {
		t.Errorf("non-spilled result should get the bare placeholder")
	}
}

// TestMicroCompact_SkipsSpilledResult: the spill pointer can sit between a
// head's opening ``` and a tail's closing ```; codeBlockRE would swallow the
// pointer with the fence. MicroCompact must skip a spilled result wholesale.
func TestMicroCompact_PreservesSpilledResultUnpruned(t *testing.T) {
	spilled := "```\n" + strings.Repeat("x", 300) +
		"\n... [999 lines truncated — use read_spillover(\"sp_deadbeef\") for full content] ...\n" +
		strings.Repeat("y", 300) + "\n```"
	messages := []llm.Message{
		assistantMsg(t, "a1"),
		toolResultMsg(spilled),
		assistantMsg(t, "a2"),
		assistantMsg(t, "a3"),
		assistantMsg(t, "a4"),
		assistantMsg(t, "a5"),
	}
	out, pruned := MicroCompact(messages, 4)
	if pruned != 0 {
		t.Fatalf("expected the spilled result to be skipped (pruned=0), got %d", pruned)
	}
	if !strings.Contains(firstToolResultContent(t, json.RawMessage(out[1].Content.Bytes())), `read_spillover("sp_deadbeef")`) {
		t.Fatalf("MicroCompact stripped the spillover pointer via stripCodeFences")
	}
}

func TestSpilloverRef_ParsesSpillIDFromText(t *testing.T) {
	cases := []struct{ in, want string }{
		{`... [12 lines truncated — use read_spillover("sp_c0953cb7") for full content] ...`, "sp_c0953cb7"},
		{"plain output, no spill", ""},
		{`read_spillover("sp_deadbeef")`, "sp_deadbeef"},
		{`read_spillover(sp_noquotes)`, ""}, // the embedded marker always quotes the id
		{`[compressed by pilot — original 32089 chars; 원문: read_spillover(spill_id="sp_cafebabe")]`, "sp_cafebabe"},
	}
	for _, c := range cases {
		if got := spilloverRef(c.in); got != c.want {
			t.Errorf("spilloverRef(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestTruncateOldToolResults_PreservesCompressedSpillHandle: pilot-compressed
// results name the spill as read_spillover(spill_id="sp_…"). Tier 2b must keep
// that pointer — otherwise the summary survives but the full output is stranded.
func TestTruncateOldToolResults_PreservesCompressedSpillHandle(t *testing.T) {
	compressed := strings.Repeat("요약 줄\n", 40) +
		`[compressed by pilot — original 32089 chars; 원문: read_spillover(spill_id="sp_cafebabe")]
` + strings.Repeat("요약 줄\n", 10)
	messages := []llm.Message{
		assistantMsg(t, "a1"),
		toolResultMsg(compressed),
		assistantMsg(t, "a2"),
		assistantMsg(t, "a3"),
		assistantMsg(t, "a4"),
		assistantMsg(t, "a5"),
	}
	out, stubbed := TruncateOldToolResults(messages, 4, 256)
	if stubbed != 1 {
		t.Fatalf("expected 1 stub, got %d", stubbed)
	}
	got := firstToolResultContent(t, json.RawMessage(out[1].Content.Bytes()))
	if !strings.Contains(got, `read_spillover("sp_cafebabe")`) {
		t.Fatalf("compressed spill handle stripped from stub: %q", got)
	}
}

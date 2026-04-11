package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

func TestRankLines_ErrorInMiddle(t *testing.T) {
	// Build log with an error buried in the middle — the main scenario
	// this feature is designed for.
	var lines []string
	for i := 0; i < 50; i++ {
		lines = append(lines, fmt.Sprintf("compiling package_%03d... ok", i))
	}
	lines = append(lines, "src/main.go:42: error: undefined variable 'foo'")
	lines = append(lines, "src/main.go:43: note: did you mean 'bar'?")
	for i := 50; i < 100; i++ {
		lines = append(lines, fmt.Sprintf("compiling package_%03d... ok", i))
	}
	content := strings.Join(lines, "\n")

	result := RankLines(content, 1024)

	if !strings.Contains(result, "error: undefined variable") {
		t.Error("error line should be preserved in ranked output")
	}
	if !strings.Contains(result, "did you mean") {
		t.Error("context line near error should be preserved")
	}
	if !strings.Contains(result, "lines omitted") {
		t.Error("should contain omission markers for discarded lines")
	}
}

func TestRankLines_NoErrors(t *testing.T) {
	// Normal output without error keywords — first/last lines should
	// be preferred due to positional bonuses.
	var lines []string
	for i := 0; i < 200; i++ {
		lines = append(lines, fmt.Sprintf("line %03d: processing item %d", i, i))
	}
	content := strings.Join(lines, "\n")

	// Budget large enough that both first-10% and last-20% groups fit,
	// but not the entire content.
	result := RankLines(content, 2048)

	if !strings.Contains(result, "line 000") {
		t.Error("first line should be preserved (header bonus)")
	}
	if !strings.Contains(result, "line 199") {
		t.Error("last line should be preserved (recency bonus)")
	}
	if !strings.Contains(result, "lines omitted") {
		t.Error("middle content should have omission markers")
	}
}

func TestRankLines_WithinBudget(t *testing.T) {
	content := "line 1\nline 2\nline 3\nline 4\nline 5"

	result := RankLines(content, 10000)

	if result != content {
		t.Error("content within budget should be returned unchanged")
	}
}

func TestRankLines_ContextExpansion(t *testing.T) {
	// Error on line 10, context ±2 (lines 8-12) should all be included
	// even though those neighbor lines have no keywords.
	var lines []string
	for i := 0; i < 50; i++ {
		if i == 10 {
			lines = append(lines, "FATAL: segmentation fault at 0xdeadbeef")
		} else {
			lines = append(lines, fmt.Sprintf("routine log entry number %d with extra padding to consume budget space quickly", i))
		}
	}
	content := strings.Join(lines, "\n")

	result := RankLines(content, 1200)

	if !strings.Contains(result, "FATAL: segmentation fault") {
		t.Error("fatal line must be included")
	}
	// Panic/fatal anchors use wider context (±5), so nearby lines are included.
	if !strings.Contains(result, "entry number 8") {
		t.Error("context line -2 from fatal should be included")
	}
	if !strings.Contains(result, "entry number 9") {
		t.Error("context line -1 from fatal should be included")
	}
	if !strings.Contains(result, "entry number 11") {
		t.Error("context line +1 from fatal should be included")
	}
	if !strings.Contains(result, "entry number 12") {
		t.Error("context line +2 from fatal should be included")
	}
}

func TestRankLines_FewLines_FallsBackToHeadTail(t *testing.T) {
	// ≤3 lines: RankLines should fall back to TruncateHeadTail.
	content := strings.Repeat("A", 500) + "\n" + strings.Repeat("B", 500) + "\n" + strings.Repeat("C", 500)

	result := RankLines(content, 600)

	// Should behave like TruncateHeadTail — contains truncation marker.
	if !strings.Contains(result, "lines truncated") {
		t.Error("few-line content should fall back to head/tail truncation")
	}
}

func TestRankLines_PreservesOriginalOrder(t *testing.T) {
	lines := []string{
		"=== BUILD START ===",
		"compiling main.go",
		"compiling util.go",
		"compiling handler.go",
		"error: handler.go:15 undefined reference",
		"compiling config.go",
		"compiling server.go",
		"warning: unused import in config.go",
		"compiling routes.go",
		"=== BUILD FAILED ===",
	}
	content := strings.Join(lines, "\n")

	result := RankLines(content, 300)

	// Error should appear before warning in the output (original order).
	errIdx := strings.Index(result, "error: handler.go")
	warnIdx := strings.Index(result, "warning: unused")
	if errIdx < 0 {
		t.Fatal("error line should be present")
	}
	if warnIdx < 0 {
		t.Fatal("warning line should be present")
	}
	if errIdx > warnIdx {
		t.Error("error should appear before warning (original order preserved)")
	}
}

func TestScoreLine_Cumulative(t *testing.T) {
	// A line with multiple matching patterns should get cumulative score.
	// "panic" (+15) + "error" (+10) + base (1) = 26
	score := scoreLine("panic: runtime error: invalid memory address", 5, 100)
	if score < 26 {
		t.Errorf("expected cumulative score >= 26, got %d", score)
	}
}

func TestScoreLine_PositionalBonus(t *testing.T) {
	plain := "just a normal line"

	// First 10% gets +2.
	early := scoreLine(plain, 0, 100)
	// Middle gets base only.
	mid := scoreLine(plain, 50, 100)
	// Last 20% gets +3.
	late := scoreLine(plain, 90, 100)

	if early <= mid {
		t.Errorf("early line (%d) should score higher than mid (%d)", early, mid)
	}
	if late <= mid {
		t.Errorf("late line (%d) should score higher than mid (%d)", late, mid)
	}
	if late <= early {
		t.Errorf("late line (%d) should score higher than early (%d)", late, early)
	}
}

func TestScoreLine_HTTPStatus(t *testing.T) {
	statusLine := `GET /api/health returned 500 Internal Server Error`
	plain := "some normal log line"
	if scoreLine(statusLine, 50, 100) <= scoreLine(plain, 50, 100) {
		t.Error("HTTP error status line should score higher than plain line")
	}
}

func TestScoreLine_JSONError(t *testing.T) {
	jsonLine := `  "error": "connection refused"`
	plain := "some normal log line"
	if scoreLine(jsonLine, 50, 100) <= scoreLine(plain, 50, 100) {
		t.Error("JSON error field should score higher than plain line")
	}
}

func TestScoreLine_SectionSeparator(t *testing.T) {
	sep := "=== BUILD OUTPUT ==="
	plain := "some normal log line"
	if scoreLine(sep, 50, 100) <= scoreLine(plain, 50, 100) {
		t.Error("section separator should score higher than plain line")
	}
}

func TestRankLines_GoPanicStackTrace(t *testing.T) {
	// Simulate a Go panic with a multi-frame stack trace (>2 lines deep).
	// The goroutine line gets panicContextRadius (±5), so all 7 stack lines
	// (indices 20-26) are within range.
	var lines []string
	for i := 0; i < 20; i++ {
		lines = append(lines, fmt.Sprintf("normal log line %d with enough padding to fill budget", i))
	}
	lines = append(lines, "goroutine 1 [running]:")
	lines = append(lines, "main.crasher(0x0)")
	lines = append(lines, "\t/app/main.go:42 +0x48")
	lines = append(lines, "main.handler(0xc0000b2000)")
	lines = append(lines, "\t/app/handler.go:15 +0x1a0")
	lines = append(lines, "main.main()")             // index 25, within ±5 of 20
	lines = append(lines, "\t/app/main.go:10 +0x25") // index 26, just outside ±5
	for i := 27; i < 60; i++ {
		lines = append(lines, fmt.Sprintf("normal log line %d with enough padding to fill budget", i))
	}
	content := strings.Join(lines, "\n")

	// Budget enough for priority context + some recency lines.
	result := RankLines(content, 1500)

	// Lines within ±5 of goroutine (index 20) = indices 15-25.
	for _, want := range []string{
		"goroutine 1",
		"main.crasher",
		"/app/main.go:42",
		"main.handler",
		"/app/handler.go:15",
		"main.main()",
	} {
		if !strings.Contains(result, want) {
			t.Errorf("panic stack trace should contain %q", want)
		}
	}
}

func TestCompactPriorToolResults_UsesRankLines(t *testing.T) {
	// Build a message with a tool_result exceeding CompactedMaxOutput (4K).
	var logLines []string
	for i := 0; i < 200; i++ {
		logLines = append(logLines, fmt.Sprintf("build step %03d: compiling package with verbose output padding here", i))
	}
	logLines[100] = "error: compilation failed at step 100"
	bigOutput := strings.Join(logLines, "\n")
	if len(bigOutput) <= CompactedMaxOutput {
		t.Fatalf("test content must exceed CompactedMaxOutput (%d), got %d", CompactedMaxOutput, len(bigOutput))
	}

	blocks := []llm.ContentBlock{{
		Type:      "tool_result",
		ToolUseID: "tool_1",
		Content:   bigOutput,
	}}
	raw, _ := json.Marshal(blocks)

	messages := []llm.Message{
		{Role: "user", Content: raw},
		{Role: "assistant", Content: json.RawMessage(`"thinking..."`)},
	}

	n := CompactPriorToolResults(messages, 1) // compact messages before index 1
	if n != 1 {
		t.Fatalf("expected 1 block compacted, got %d", n)
	}

	// Unmarshal the compacted content and verify the error survived.
	var result []llm.ContentBlock
	if err := json.Unmarshal(messages[0].Content, &result); err != nil {
		t.Fatalf("unmarshal compacted: %v", err)
	}
	if !strings.Contains(result[0].Content, "compilation failed") {
		t.Error("error line should survive compaction via RankLines")
	}
	if !strings.Contains(result[0].Content, "ranked:") {
		t.Error("compacted output should contain ranked header")
	}
}

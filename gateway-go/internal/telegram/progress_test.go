package telegram

import (
	"context"
	"strings"
	"testing"
)

func TestRenderText_NoStatusInserts(t *testing.T) {
	pt := &ProgressTracker{}
	pt.steps = []ProgressStep{
		{Tool: "read", Status: "done"},
		{Tool: "grep", Status: "done"},
		{Tool: "edit", Status: "running"},
	}

	text := pt.renderText()
	if !strings.Contains(text, "✅ 파일 읽기") {
		t.Error("expected Korean label for read tool")
	}
	if !strings.Contains(text, "⏳ 파일 수정") {
		t.Error("expected running icon for edit tool")
	}
	if strings.Contains(text, "💭") {
		t.Error("unexpected status insert with no inserts configured")
	}
}

func TestRenderText_WithStatusInserts(t *testing.T) {
	pt := &ProgressTracker{
		statusInserts: map[int]string{
			1: "코드 구조 파악하는 중",
		},
	}
	pt.steps = []ProgressStep{
		{Tool: "read", Status: "done"},
		{Tool: "grep", Status: "done"},
		{Tool: "edit", Status: "running"},
	}

	text := pt.renderText()
	if !strings.Contains(text, "💭 코드 구조 파악하는 중") {
		t.Errorf("expected status insert after step 1, got:\n%s", text)
	}

	// Verify ordering: status insert should appear between grep and edit lines.
	grepIdx := strings.Index(text, "코드 검색")
	statusIdx := strings.Index(text, "💭")
	editIdx := strings.Index(text, "파일 수정")
	if grepIdx > statusIdx || statusIdx > editIdx {
		t.Errorf("status insert not between expected steps, grep=%d status=%d edit=%d", grepIdx, statusIdx, editIdx)
	}
}

func TestOnToolComplete_TriggersStatusAtInterval(t *testing.T) {
	called := make(chan []string, 1)
	mockSummarize := func(ctx context.Context, reasons []string) (string, error) {
		called <- reasons
		return "테스트 요약", nil
	}

	pt := NewProgressTracker(nil, 0, mockSummarize)

	ctx := context.Background()

	// Simulate 4 tools: start + complete each.
	tools := []string{"read", "grep", "read", "grep"}
	for _, name := range tools {
		pt.OnToolStart(ctx, name, "thinking about "+name)
	}
	for i, name := range tools {
		pt.OnToolComplete(ctx, name, false)
		if i < len(tools)-1 {
			// Should not trigger summary before 4th completion.
			select {
			case <-called:
				t.Fatalf("summary triggered too early at completion %d", i+1)
			default:
			}
		}
	}

	// 4th completion should trigger the summary goroutine.
	reasons := <-called
	if len(reasons) == 0 {
		t.Fatal("expected non-empty reasons in summary call")
	}
	if len(reasons) != 4 {
		t.Errorf("expected 4 reasons, got %d", len(reasons))
	}
}

func TestOnToolComplete_NoSummaryWithoutFn(t *testing.T) {
	pt := NewProgressTracker(nil, 0, nil)
	ctx := context.Background()

	for i := 0; i < 8; i++ {
		pt.OnToolStart(ctx, "read", "thinking")
		pt.OnToolComplete(ctx, "read", false)
	}

	// No panic, no status inserts.
	pt.mu.Lock()
	inserts := len(pt.statusInserts)
	pt.mu.Unlock()
	if inserts != 0 {
		t.Errorf("expected no status inserts without summarizeFn, got %d", inserts)
	}
}

func TestOnToolComplete_NoSummaryWithEmptyReasons(t *testing.T) {
	called := false
	mockSummarize := func(ctx context.Context, reasons []string) (string, error) {
		called = true
		return "should not be called", nil
	}

	pt := NewProgressTracker(nil, 0, mockSummarize)
	ctx := context.Background()

	// Complete 4 tools without any reason text.
	for i := 0; i < 4; i++ {
		pt.OnToolStart(ctx, "read", "")
		pt.OnToolComplete(ctx, "read", false)
	}

	if called {
		t.Error("summary should not be called when all reasons are empty")
	}
}

func TestSanitizeSummary(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"코드 구조 파악하는 중", "코드 구조 파악하는 중"},
		{"\"코드 분석하는 중\"", "코드 분석하는 중"},
		{"💭 버그 찾는 중\n추가 설명", "버그 찾는 중"},
		{"", ""},
		{"  ", ""},
		{strings.Repeat("가", 50), strings.Repeat("가", 30)},
	}
	for _, tt := range tests {
		got := sanitizeSummary(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeSummary(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestOnToolStart_AccumulatesReasons(t *testing.T) {
	pt := NewProgressTracker(nil, 0, nil)
	ctx := context.Background()

	pt.OnToolStart(ctx, "read", "first reason")
	pt.OnToolStart(ctx, "grep", "")
	pt.OnToolStart(ctx, "edit", "third reason")

	pt.mu.Lock()
	defer pt.mu.Unlock()

	if len(pt.reasons) != 2 {
		t.Errorf("expected 2 reasons (empty skipped), got %d", len(pt.reasons))
	}
	if pt.reasons[0] != "first reason" || pt.reasons[1] != "third reason" {
		t.Errorf("unexpected reasons: %v", pt.reasons)
	}
}

func TestOnToolStart_TruncatesLongReasons(t *testing.T) {
	pt := NewProgressTracker(nil, 0, nil)
	ctx := context.Background()

	longReason := strings.Repeat("가", 500)
	pt.OnToolStart(ctx, "read", longReason)

	pt.mu.Lock()
	defer pt.mu.Unlock()

	if len([]rune(pt.reasons[0])) != maxReasonLen {
		t.Errorf("expected reason truncated to %d runes, got %d", maxReasonLen, len([]rune(pt.reasons[0])))
	}
}

package telegram

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRenderText_NoStatusInserts(t *testing.T) {
	pt := &ProgressTracker{}
	pt.steps = []ProgressStep{
		{Tool: "read", Status: "done", ArgHint: "progress.go", Elapsed: 100 * time.Millisecond},
		{Tool: "grep", Status: "done", ArgHint: "OnToolStart", Elapsed: 350 * time.Millisecond},
		{Tool: "edit", Status: "running", ArgHint: "progress.go"},
	}

	text := pt.renderText()
	if !strings.Contains(text, "✅ 파일 읽기 — progress.go") {
		t.Errorf("expected Korean label with arg hint for read tool, got:\n%s", text)
	}
	if !strings.Contains(text, "✅ 코드 검색 — OnToolStart") {
		t.Errorf("expected Korean label with arg hint for grep tool, got:\n%s", text)
	}
	if !strings.Contains(text, "⏳ 파일 수정 — progress.go") {
		t.Errorf("expected running icon with arg hint for edit tool, got:\n%s", text)
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
	mockSummarize := func(ctx context.Context, activities []string) (string, error) {
		called <- activities
		return "테스트 요약", nil
	}

	pt := NewProgressTracker(nil, 0, mockSummarize)

	ctx := context.Background()

	// Simulate 4 tools: start + complete each.
	tools := []string{"read", "grep", "read", "grep"}
	inputs := []string{
		`{"path":"server.go"}`,
		`{"pattern":"OnToolStart"}`,
		`{"path":"hooks.go"}`,
		`{"pattern":"StreamHooks"}`,
	}
	for i, name := range tools {
		pt.OnToolStart(ctx, name, []byte(inputs[i]))
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
	activities := <-called
	if len(activities) == 0 {
		t.Fatal("expected non-empty activities in summary call")
	}
	if len(activities) != 4 {
		t.Errorf("expected 4 activities, got %d", len(activities))
	}
	// Verify activities contain tool arg hints.
	if !strings.Contains(activities[0], "read: server.go") {
		t.Errorf("expected activity with arg hint, got %q", activities[0])
	}
}

func TestOnToolComplete_NoSummaryWithoutFn(t *testing.T) {
	pt := NewProgressTracker(nil, 0, nil)
	ctx := context.Background()

	for i := 0; i < 8; i++ {
		pt.OnToolStart(ctx, "read", []byte(`{"path":"test.go"}`))
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

func TestOnToolComplete_NoSummaryWithEmptyInput(t *testing.T) {
	called := make(chan struct{}, 1)
	mockSummarize := func(ctx context.Context, activities []string) (string, error) {
		select {
		case called <- struct{}{}:
		default:
		}
		return "요약 결과", nil
	}

	pt := NewProgressTracker(nil, 0, mockSummarize)
	ctx := context.Background()

	// Complete 4 tools without any input (nil → activity is just tool name).
	for i := 0; i < 4; i++ {
		pt.OnToolStart(ctx, "read", nil)
		pt.OnToolComplete(ctx, "read", false)
	}

	// Activities are still generated (tool name without hint), so summarizer
	// should still be called — the tool name alone is useful context.
	select {
	case <-called:
		// summarizer was called as expected
	case <-time.After(2 * time.Second):
		t.Error("expected summarizer to be called after 4 completions, but it was not")
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
		{strings.Repeat("가", 50), strings.Repeat("가", 40)},
		// Reasoning model preamble stripping.
		{"Thinking Process:\n코드 분석하는 중", "코드 분석하는 중"},
		{"Analysis:\n버그 원인 분석하는 중", "버그 원인 분석하는 중"},
		{"<think>reasoning here</think>\n코드 검색하는 중", "코드 검색하는 중"},
		{"<think>hmm</think>\nThinking Process:\n의존성 확인하는 중", "의존성 확인하는 중"},
	}
	for _, tt := range tests {
		got := sanitizeSummary(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeSummary(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestExtractArgHint(t *testing.T) {
	tests := []struct {
		tool  string
		input string
		want  string
	}{
		{"read", `{"path":"gateway-go/internal/telegram/progress.go"}`, "gateway-go/internal/telegram/p…"},
		{"edit", `{"file":"progress.go","old":"a","new":"b"}`, "progress.go"},
		{"exec", `{"command":"make test"}`, "make test"},
		{"grep", `{"pattern":"OnToolStart","path":"gateway-go/"}`, "OnToolStart in gateway-go/"},
		{"web", `{"query":"golang json unmarshal"}`, "golang json unmarshal"},
		{"read", `{}`, ""},
		{"read", ``, ""},
		{"read", `invalid json`, ""},
		{"unknown_tool", `{"path":"some/file.go"}`, "some/file.go"},
		{"memory", `{"query":"progress tracker"}`, "progress tracker"},
	}
	for _, tt := range tests {
		got := extractArgHint(tt.tool, []byte(tt.input))
		if got != tt.want {
			t.Errorf("extractArgHint(%q, %q) = %q, want %q", tt.tool, tt.input, got, tt.want)
		}
	}
}

func TestExtractArgHint_TruncatesLongValues(t *testing.T) {
	longPath := strings.Repeat("a", 50) + ".go"
	input := `{"path":"` + longPath + `"}`
	got := extractArgHint("read", []byte(input))
	if len([]rune(got)) > maxArgHintLen+1 { // +1 for the trailing "…"
		t.Errorf("expected truncated hint, got %d runes: %q", len([]rune(got)), got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("expected truncated hint to end with …, got %q", got)
	}
}

func TestRenderText_ElapsedTime(t *testing.T) {
	pt := &ProgressTracker{}
	pt.steps = []ProgressStep{
		{Tool: "read", Status: "done", Elapsed: 150 * time.Millisecond},
		{Tool: "exec", Status: "done", ArgHint: "make test", Elapsed: 2500 * time.Millisecond},
		{Tool: "grep", Status: "running", ArgHint: "pattern"},
	}

	text := pt.renderText()

	// Completed steps should show elapsed time.
	if !strings.Contains(text, "(150ms)") {
		t.Errorf("expected 150ms elapsed for read, got:\n%s", text)
	}
	if !strings.Contains(text, "(2.5s)") {
		t.Errorf("expected 2.5s elapsed for exec, got:\n%s", text)
	}
	// Running steps should NOT show elapsed time.
	grepLine := ""
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "코드 검색") {
			grepLine = line
			break
		}
	}
	if strings.Contains(grepLine, "(") {
		t.Errorf("running step should not show elapsed time, got: %s", grepLine)
	}
}

func TestOnToolStart_AccumulatesActivities(t *testing.T) {
	pt := NewProgressTracker(nil, 0, nil)
	ctx := context.Background()

	pt.OnToolStart(ctx, "read", []byte(`{"path":"server.go"}`))
	pt.OnToolStart(ctx, "grep", nil)
	pt.OnToolStart(ctx, "edit", []byte(`{"file":"server.go"}`))

	pt.mu.Lock()
	defer pt.mu.Unlock()

	if len(pt.activities) != 3 {
		t.Errorf("expected 3 activities, got %d", len(pt.activities))
	}
	if pt.activities[0] != "read: server.go" {
		t.Errorf("expected 'read: server.go', got %q", pt.activities[0])
	}
	if pt.activities[1] != "grep" {
		t.Errorf("expected 'grep' (no hint), got %q", pt.activities[1])
	}
	if pt.activities[2] != "edit: server.go" {
		t.Errorf("expected 'edit: server.go', got %q", pt.activities[2])
	}
}

func TestFormatElapsed(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{50 * time.Millisecond, "50ms"},
		{999 * time.Millisecond, "999ms"},
		{1000 * time.Millisecond, "1.0s"},
		{1500 * time.Millisecond, "1.5s"},
		{10 * time.Second, "10.0s"},
	}
	for _, tt := range tests {
		got := formatElapsed(tt.d)
		if got != tt.want {
			t.Errorf("formatElapsed(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

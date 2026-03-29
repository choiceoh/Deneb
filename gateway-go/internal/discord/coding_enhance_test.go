package discord

import (
	"strings"
	"testing"
)

func TestFormatDiffPreviewEmbed(t *testing.T) {
	embed := FormatDiffPreviewEmbed("main.go", 10, 3, "+added line\n-removed line")
	if embed.Title != "🔍 변경 미리보기" {
		t.Errorf("unexpected title: %s", embed.Title)
	}
	if embed.Color != ColorInfo {
		t.Errorf("expected info color, got %#x", embed.Color)
	}
	if len(embed.Fields) != 3 {
		t.Fatalf("expected 3 fields, got %d", len(embed.Fields))
	}
	if !strings.Contains(embed.Fields[0].Value, "main.go") {
		t.Errorf("expected file name in first field")
	}
}

func TestFormatMultiFileDiffPreviewEmbed(t *testing.T) {
	changes := []FileChange{
		{Name: "a.go", Added: 5, Removed: 2},
		{Name: "b.go", Added: 3, Removed: 0},
	}
	embed := FormatMultiFileDiffPreviewEmbed(changes, 8, 2)
	if embed.Title != "🔍 변경 미리보기" {
		t.Errorf("unexpected title: %s", embed.Title)
	}
	if len(embed.Fields) != 4 {
		t.Fatalf("expected 4 fields, got %d", len(embed.Fields))
	}
}

func TestDiffPreviewButtons(t *testing.T) {
	buttons := DiffPreviewButtons("discord:123")
	if len(buttons) != 1 {
		t.Fatalf("expected 1 action row, got %d", len(buttons))
	}
	if len(buttons[0].Components) != 3 {
		t.Fatalf("expected 3 buttons, got %d", len(buttons[0].Components))
	}
	prefixes := []string{"diffapply:", "diffreject:", "difffull:"}
	for i, btn := range buttons[0].Components {
		if !strings.HasPrefix(btn.CustomID, prefixes[i]) {
			t.Errorf("button %d: expected prefix %s, got %s", i, prefixes[i], btn.CustomID)
		}
	}
}

func TestFormatEnhancedDashboardEmbed(t *testing.T) {
	data := DashboardData{
		Branch:       "main",
		ChangedFiles: 3,
		FilesSummary: "`M file.go`",
		BuildStatus:  "✅ 성공",
		TestStatus:   "✅ 전체 통과",
		LintStatus:   "✅ 깨끗",
		RecentLog:    "• abc1234 feat: something",
		Upstream:     "origin/main",
		StashCount:   1,
	}
	embed := FormatEnhancedDashboardEmbed(data)
	if embed.Title != "📊 프로젝트 현황" {
		t.Errorf("unexpected title: %s", embed.Title)
	}
	// Should have: branch, upstream, changes, build, test, lint, stash, recent log = 8 fields.
	if len(embed.Fields) < 7 {
		t.Errorf("expected at least 7 fields, got %d", len(embed.Fields))
	}
}

func TestDashboardButtons(t *testing.T) {
	buttons := DashboardButtons("discord:456")
	if len(buttons) != 1 {
		t.Fatalf("expected 1 action row, got %d", len(buttons))
	}
	if len(buttons[0].Components) != 4 {
		t.Fatalf("expected 4 buttons, got %d", len(buttons[0].Components))
	}
}

func TestErrorRecoveryButtons(t *testing.T) {
	buttons := ErrorRecoveryButtons("discord:789")
	if len(buttons) != 1 {
		t.Fatalf("expected 1 action row, got %d", len(buttons))
	}
	if len(buttons[0].Components) != 3 {
		t.Fatalf("expected 3 buttons, got %d", len(buttons[0].Components))
	}
	prefixes := []string{"autofix:", "altfix:", "revert:"}
	for i, btn := range buttons[0].Components {
		if !strings.HasPrefix(btn.CustomID, prefixes[i]) {
			t.Errorf("button %d: expected prefix %s, got %s", i, prefixes[i], btn.CustomID)
		}
	}
}

func TestSmartTestButtons(t *testing.T) {
	// Failed case.
	failButtons := SmartTestButtons("discord:111", true)
	if len(failButtons[0].Components) != 3 {
		t.Fatalf("expected 3 buttons for failed, got %d", len(failButtons[0].Components))
	}
	if !strings.HasPrefix(failButtons[0].Components[0].CustomID, "autofix:") {
		t.Errorf("first button should be autofix")
	}

	// Pass case.
	passButtons := SmartTestButtons("discord:222", false)
	if len(passButtons[0].Components) != 3 {
		t.Fatalf("expected 3 buttons for passed, got %d", len(passButtons[0].Components))
	}
	if !strings.HasPrefix(passButtons[0].Components[0].CustomID, "testall:") {
		t.Errorf("first button should be testall")
	}
}

func TestGitWorkflowButtons(t *testing.T) {
	buttons := GitWorkflowButtons("discord:333")
	if len(buttons) != 1 {
		t.Fatalf("expected 1 action row, got %d", len(buttons))
	}
	if len(buttons[0].Components) != 3 {
		t.Fatalf("expected 3 buttons, got %d", len(buttons[0].Components))
	}
	prefixes := []string{"branchcreate:", "prcreate:", "mergecheck:"}
	for i, btn := range buttons[0].Components {
		if !strings.HasPrefix(btn.CustomID, prefixes[i]) {
			t.Errorf("button %d: expected prefix %s, got %s", i, prefixes[i], btn.CustomID)
		}
	}
}

func TestFormatSubagentProgressEmbed(t *testing.T) {
	tasks := []SubagentTask{
		{Name: "백엔드", Description: "API 구현", Status: "completed", Duration: "3.2s"},
		{Name: "테스트", Description: "테스트 작성", Status: "running"},
	}
	embed := FormatSubagentProgressEmbed(tasks)
	if embed.Color != ColorProgress {
		t.Errorf("expected progress color while running, got %#x", embed.Color)
	}
	if !strings.Contains(embed.Description, "백엔드") {
		t.Errorf("expected task names in description")
	}
}

func TestFormatSubagentProgressEmbed_AllDone(t *testing.T) {
	tasks := []SubagentTask{
		{Name: "A", Description: "task", Status: "completed"},
		{Name: "B", Description: "task", Status: "completed"},
	}
	embed := FormatSubagentProgressEmbed(tasks)
	if embed.Color != ColorSuccess {
		t.Errorf("expected success color when all done, got %#x", embed.Color)
	}
}

func TestFormatPilotDelegationEmbed(t *testing.T) {
	embed := FormatPilotDelegationEmbed("코드 분석", []string{"file.go", "test.go"})
	if embed.Title != "🧠 Pilot 분석 중" {
		t.Errorf("unexpected title: %s", embed.Title)
	}
	if embed.Color != ColorProgress {
		t.Errorf("expected progress color")
	}
}

func TestFormatSmartTestEmbed(t *testing.T) {
	embed := FormatSmartTestEmbed([]string{"./internal/foo/..."}, 5, 1, 0, "FAIL TestFoo")
	if embed.Color != ColorError {
		t.Errorf("expected error color for failed tests")
	}
	if !strings.Contains(embed.Title, "실패") {
		t.Errorf("expected failure in title")
	}

	embed2 := FormatSmartTestEmbed(nil, 10, 0, 2, "")
	if embed2.Color != ColorSuccess {
		t.Errorf("expected success color for passing tests")
	}
}

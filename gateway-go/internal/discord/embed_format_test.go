package discord

import (
	"strings"
	"testing"
)

func TestFormatToolResultEmbed_Success(t *testing.T) {
	e := FormatToolResultEmbed("read", "file contents here", false, 150)
	if e.Color != ColorSuccess {
		t.Errorf("expected green color %#x, got %#x", ColorSuccess, e.Color)
	}
	if !strings.Contains(e.Title, "read") {
		t.Errorf("expected title to contain tool name, got %q", e.Title)
	}
	if e.Footer == nil || !strings.Contains(e.Footer.Text, "150") {
		t.Error("expected footer with duration")
	}
}

func TestFormatToolResultEmbed_Error(t *testing.T) {
	e := FormatToolResultEmbed("exec", "command failed", true, 0)
	if e.Color != ColorError {
		t.Errorf("expected red color %#x, got %#x", ColorError, e.Color)
	}
	if !strings.Contains(e.Title, "❌") {
		t.Error("expected error emoji in title")
	}
}

func TestFormatGitDiffEmbed_Empty(t *testing.T) {
	e := FormatGitDiffEmbed("")
	if !strings.Contains(e.Description, "없음") {
		t.Errorf("expected '없음' for empty diff, got %q", e.Description)
	}
}

func TestFormatGitDiffEmbed_WithStats(t *testing.T) {
	stats := ` file.go     | 5 +++--
 main.go     | 2 +-
 3 files changed, 10 insertions(+), 2 deletions(-)`
	e := FormatGitDiffEmbed(stats)
	if e.Color != ColorInfo {
		t.Errorf("expected blue color, got %#x", e.Color)
	}
	if len(e.Fields) < 2 {
		t.Errorf("expected at least 2 file fields, got %d", len(e.Fields))
	}
}

func TestFormatGitDiffEmbed_ManyFiles(t *testing.T) {
	var lines []string
	for i := 0; i < 20; i++ {
		lines = append(lines, " file"+string(rune('a'+i))+".go | 1 +")
	}
	lines = append(lines, " 20 files changed, 20 insertions(+)")
	stats := strings.Join(lines, "\n")

	e := FormatGitDiffEmbed(stats)
	if len(e.Fields) > 15 {
		t.Errorf("expected capped fields at 15, got %d", len(e.Fields))
	}
}

func TestFormatTestResultsEmbed_Pass(t *testing.T) {
	e := FormatTestResultsEmbed(10, 0, 10, "all passed")
	if e.Color != ColorSuccess {
		t.Errorf("expected green, got %#x", e.Color)
	}
	if len(e.Fields) != 3 {
		t.Errorf("expected 3 fields (pass/fail/total), got %d", len(e.Fields))
	}
}

func TestFormatTestResultsEmbed_Fail(t *testing.T) {
	e := FormatTestResultsEmbed(8, 2, 10, "2 failures")
	if e.Color != ColorError {
		t.Errorf("expected red, got %#x", e.Color)
	}
}

func TestFormatErrorEmbed(t *testing.T) {
	e := FormatErrorEmbed("nil pointer", "main.go", 42)
	if e.Color != ColorError {
		t.Errorf("expected red, got %#x", e.Color)
	}
	if len(e.Fields) == 0 {
		t.Error("expected location field")
	}
	if !strings.Contains(e.Fields[0].Value, "main.go:42") {
		t.Errorf("expected file:line in field, got %q", e.Fields[0].Value)
	}
}

func TestFormatStatusEmbed(t *testing.T) {
	e := FormatStatusEmbed("main", "M file.go", "1 file changed", "abc123 initial commit")
	if e.Color != ColorInfo {
		t.Errorf("expected blue, got %#x", e.Color)
	}
	if len(e.Fields) != 4 {
		t.Errorf("expected 4 fields, got %d", len(e.Fields))
	}
}

func TestTruncate(t *testing.T) {
	short := "hello"
	if truncate(short, 10) != "hello" {
		t.Error("short string should not be truncated")
	}

	long := strings.Repeat("x", 100)
	result := truncate(long, 20)
	if len(result) != 20 {
		t.Errorf("expected length 20, got %d", len(result))
	}
	if !strings.HasSuffix(result, "...") {
		t.Error("expected ... suffix")
	}
}

func TestFormatProgressEmbed_InProgress(t *testing.T) {
	steps := []ProgressStep{
		{Name: "read file", Status: StepDone},
		{Name: "edit code", Status: StepRunning},
		{Name: "test", Status: StepPending},
	}
	e := FormatProgressEmbed(steps)
	if e.Color != ColorProgress {
		t.Errorf("expected orange, got %#x", e.Color)
	}
	if !strings.Contains(e.Description, "✅") {
		t.Error("expected checkmark for done step")
	}
	if !strings.Contains(e.Description, "🔄") {
		t.Error("expected spinner for running step")
	}
}

func TestFormatProgressEmbed_AllDone(t *testing.T) {
	steps := []ProgressStep{
		{Name: "step1", Status: StepDone},
		{Name: "step2", Status: StepDone},
	}
	e := FormatProgressEmbed(steps)
	if e.Color != ColorSuccess {
		t.Errorf("expected green when all done, got %#x", e.Color)
	}
}

func TestFormatProgressEmbed_ParallelGroup(t *testing.T) {
	steps := []ProgressStep{
		{Name: "tree", Status: StepDone, Group: 0},
		{Name: "grep", Status: StepDone, Group: 2},
		{Name: "find", Status: StepDone, Group: 2},
		{Name: "edit", Status: StepDone, Group: 3},
	}
	e := FormatProgressEmbed(steps)
	if e.Color != ColorSuccess {
		t.Errorf("expected green, got %#x", e.Color)
	}
	// Grouped steps (group 2) should have ┃ prefix.
	if !strings.Contains(e.Description, "┃ ✅ grep") {
		t.Error("expected parallel prefix for group-2 step 'grep'")
	}
	if !strings.Contains(e.Description, "┃ ✅ find") {
		t.Error("expected parallel prefix for group-2 step 'find'")
	}
	// Non-grouped / single-member group steps should not.
	if strings.Contains(e.Description, "┃ ✅ tree") {
		t.Error("group-0 step should not have parallel prefix")
	}
	if strings.Contains(e.Description, "┃ ✅ edit") {
		t.Error("single-member group step should not have parallel prefix")
	}
}

func TestParseDiffSummary(t *testing.T) {
	files, ins, del := parseDiffSummary("3 files changed, 10 insertions(+), 2 deletions(-)")
	if files != 3 || ins != 10 || del != 2 {
		t.Errorf("expected 3/10/2, got %d/%d/%d", files, ins, del)
	}
}

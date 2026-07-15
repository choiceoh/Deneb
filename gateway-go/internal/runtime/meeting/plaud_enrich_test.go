package meeting

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailanalysis"
)

func TestExtractEarliestDue(t *testing.T) {
	report := `## 요약
- x
## 액션 아이템
- 김부장: 견적 제출 (2026-08-01)
- 이과장: 현장 방문 2026-07-20
## 리스크·미해결
- 없음
`
	if got := extractEarliestDue(report); got != "2026-07-20" {
		t.Fatalf("due=%q", got)
	}
}

func TestRelatedProjectsOrFallbackUsesRank(t *testing.T) {
	cands := []mailanalysis.ProjectCandidate{
		{Path: "프로젝트/비금도-154kv/대표.md", Title: "비금도 154kV"},
		{Path: "프로젝트/당진-솔라빌리지/대표.md", Title: "당진 솔라빌리지"},
	}
	body, related := relatedProjectsOrFallback("## 요약\n- x\n관련프로젝트: 없음", cands, "비금 주간", "비금도 잔금")
	if len(related) != 1 || !strings.Contains(related[0], "비금도") {
		t.Fatalf("related=%v body=%q", related, body)
	}
}

func TestMatchCalendarEventByTimeAndTitle(t *testing.T) {
	start := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	f := plaudFile{ID: "abc", Name: "비금도 잔금 회의", StartAt: start, Duration: time.Hour}
	evs := []calendar.Event{
		{ID: "e1", Summary: "비금도 잔금 미팅", Start: start.Add(-10 * time.Minute), End: start.Add(50 * time.Minute)},
		{ID: "e2", Summary: "개인 점심", Start: start.Add(3 * time.Hour), End: start.Add(4 * time.Hour)},
	}
	got := matchCalendarEvent(f, evs)
	if got == nil || got.ID != "e1" {
		t.Fatalf("got=%v", got)
	}
}

func TestWriteTranscriptSpill(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "plaud-recordings-state.json")
	path := writeTranscriptSpill(state, "deadbeef", "화자1: 안녕하세요")
	if path == "" {
		t.Fatal("expected spill path")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "화자1: 안녕하세요" {
		t.Fatalf("spill=%q err=%v", data, err)
	}
}

func TestDefaultCorrectionPromptMatchesTemplate(t *testing.T) {
	root := findRepoRoot(t)
	tmpl := filepath.Join(root, "docs/reference/templates/topics/plaud-correction.md")
	data, err := os.ReadFile(tmpl)
	if err != nil {
		t.Fatal(err)
	}
	body := strings.TrimSpace(string(data))
	// Strip leading H1 title line from template for comparison to embedded default.
	lines := strings.Split(body, "\n")
	if strings.HasPrefix(lines[0], "# ") {
		body = strings.TrimSpace(strings.Join(lines[1:], "\n"))
	}
	def := strings.TrimSpace(defaultPlaudCorrectionPrompt)
	// Require shared core principles rather than byte-identical (template has file refs).
	for _, needle := range []string{"용어집", "교정 금지", "MW", "확신 없으면"} {
		if !strings.Contains(def, needle) || !strings.Contains(body, needle) {
			t.Fatalf("core principle %q missing from default or template", needle)
		}
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := wd
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "docs/reference/templates/topics/plaud-correction.md")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("repo root not found")
	return ""
}

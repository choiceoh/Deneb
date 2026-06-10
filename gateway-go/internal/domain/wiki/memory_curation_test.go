package wiki

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const memoryFixture = `# Memory

Auto-recorded learnings and decisions.

## 결정사항

- [0.9] 크론 에이전트를 메인 에이전트의 시한부 클론으로 통합.

## 선호도

- 한국어 응답 선호.

## 2026-04-07 00:39

옛날 학습 항목 — 위키로 증류되어야 한다.

## 2026-04-07 01:55

두 번째 옛 항목.

## %s

최근 항목 — keep window 안이라 유지되어야 한다.
`

// freshStamp returns a section stamp inside the keep window.
func freshStamp() string {
	return time.Now().Add(-24 * time.Hour).Format("2006-01-02 15:04")
}

func writeMemoryFixture(t *testing.T, dir string) string {
	t.Helper()
	content := strings.Replace(memoryFixture, "%s", freshStamp(), 1)
	path := filepath.Join(dir, memoryFileName)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return content
}

func TestParseMemorySections_ByteFaithful(t *testing.T) {
	content := strings.Replace(memoryFixture, "%s", freshStamp(), 1)
	preamble, sections := parseMemorySections(content)

	if !strings.HasPrefix(preamble, "# Memory") {
		t.Errorf("preamble lost: %q", preamble)
	}
	if len(sections) != 5 {
		t.Fatalf("want 5 sections, got %d", len(sections))
	}
	if sections[0].stamp != "" || sections[1].stamp != "" {
		t.Error("category sections must not be detected as timestamped")
	}
	if sections[2].stamp != "2026-04-07 00:39" {
		t.Errorf("stamp parse failed: %q", sections[2].stamp)
	}
	// Reassembly must reproduce the file byte-for-byte.
	var sb strings.Builder
	sb.WriteString(preamble)
	for _, s := range sections {
		sb.WriteString(s.raw)
	}
	if sb.String() != content {
		t.Error("reassembled content differs from original")
	}
}

func TestScanWorkspaceMemory_ConsumedThroughAndBudget(t *testing.T) {
	dir := t.TempDir()
	writeMemoryFixture(t, dir)
	wd := &WikiDreamer{workspaceDir: dir}

	scan := wd.scanWorkspaceMemory("")
	if scan == nil {
		t.Fatal("expected sections to scan")
	}
	if scan.Sections != 3 {
		t.Errorf("want 3 timestamped sections, got %d", scan.Sections)
	}
	if !strings.Contains(scan.Content, "옛날 학습 항목") {
		t.Error("scan content missing old section")
	}
	if scan.ConsumedThrough <= "2026-04-07 01:55" {
		t.Errorf("high-water must reach the fresh stamp, got %q", scan.ConsumedThrough)
	}

	// Already consumed through the old entries → only the fresh one remains.
	scan2 := wd.scanWorkspaceMemory("2026-04-07 01:55")
	if scan2 == nil || scan2.Sections != 1 {
		t.Fatalf("want 1 unconsumed section, got %+v", scan2)
	}
	// Fully consumed → nothing to scan.
	if got := wd.scanWorkspaceMemory(scan.ConsumedThrough); got != nil {
		t.Errorf("expected nil scan when fully consumed, got %+v", got)
	}
	// No workspace → disabled.
	if got := (&WikiDreamer{}).scanWorkspaceMemory(""); got != nil {
		t.Error("empty workspaceDir must disable scanning")
	}
}

func TestCurateWorkspaceMemory_DropsConsumedOldKeepsRest(t *testing.T) {
	dir := t.TempDir()
	original := writeMemoryFixture(t, dir)
	wd := &WikiDreamer{workspaceDir: dir}

	scan := wd.scanWorkspaceMemory("")
	if scan == nil {
		t.Fatal("scan failed")
	}
	dropped, err := wd.curateWorkspaceMemory(scan)
	if err != nil {
		t.Fatalf("curate: %v", err)
	}
	if dropped != 2 {
		t.Errorf("want 2 dropped (old+consumed), got %d", dropped)
	}

	after, err := os.ReadFile(filepath.Join(dir, memoryFileName))
	if err != nil {
		t.Fatal(err)
	}
	got := string(after)
	if strings.Contains(got, "옛날 학습 항목") || strings.Contains(got, "두 번째 옛 항목") {
		t.Error("consumed old sections must be dropped")
	}
	if !strings.Contains(got, "## 결정사항") || !strings.Contains(got, "## 선호도") {
		t.Error("category sections must be preserved")
	}
	if !strings.Contains(got, "최근 항목") {
		t.Error("recent section inside the keep window must be preserved")
	}
	bak, err := os.ReadFile(filepath.Join(dir, memoryFileName+".bak"))
	if err != nil || string(bak) != original {
		t.Error("backup must hold the pre-curation content")
	}
}

func TestCurateWorkspaceMemory_SkipsOnConcurrentChange(t *testing.T) {
	dir := t.TempDir()
	writeMemoryFixture(t, dir)
	wd := &WikiDreamer{workspaceDir: dir}
	scan := wd.scanWorkspaceMemory("")
	if scan == nil {
		t.Fatal("scan failed")
	}

	// The recorder appends mid-cycle: rewrite must be skipped to avoid
	// dropping the new append.
	path := filepath.Join(dir, memoryFileName)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("\n## " + freshStamp() + "\n\n사이클 중 추가된 항목.\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	dropped, err := wd.curateWorkspaceMemory(scan)
	if err != nil {
		t.Fatalf("curate: %v", err)
	}
	if dropped != 0 {
		t.Errorf("concurrent change must skip the rewrite, dropped=%d", dropped)
	}
	after, _ := os.ReadFile(path)
	if !strings.Contains(string(after), "사이클 중 추가된 항목") {
		t.Error("mid-cycle append was lost")
	}
}

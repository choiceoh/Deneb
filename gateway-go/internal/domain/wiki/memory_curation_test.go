package wiki

import (
	"fmt"
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

// writeOversizedMemory builds a MEMORY.md larger than memoryDiskMaxBytes: a
// preamble, two category sections, then many timestamped sections oldest→newest.
// Returns the path, the oldest stamp, and the newest stamp.
func writeOversizedMemory(t *testing.T, dir string) (path, oldest, newest string) {
	t.Helper()
	var sb strings.Builder
	sb.WriteString("# Memory\n\nAuto-recorded learnings and decisions.\n\n")
	sb.WriteString("## 결정사항\n\n- [0.9] 카테고리 섹션은 보존되어야 한다.\n\n")
	sb.WriteString("## 선호도\n\n- 한국어 응답 선호.\n\n")

	// Each timestamped section ~600 bytes; enough sections to blow past the cap.
	// Stamps walk forward one day at a time from a fixed base so every stamp is
	// unique, valid, and strictly ordered (no ambiguous duplicates).
	body := strings.Repeat("타임스탬프 섹션 본문 패딩 라인. ", 20)
	base := time.Date(2025, 1, 1, 9, 0, 0, 0, time.UTC)
	for day := 0; sb.Len() <= memoryDiskMaxBytes+20_000; day++ {
		stamp := base.AddDate(0, 0, day).Format("2006-01-02 15:04")
		if day == 0 {
			oldest = stamp
		}
		fmt.Fprintf(&sb, "## %s\n\n%s\n\n", stamp, body)
		newest = stamp
	}

	path = filepath.Join(dir, memoryFileName)
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return path, oldest, newest
}

func TestEnforceMemoryDiskCap_BoundsAndKeepsHeadTail(t *testing.T) {
	dir := t.TempDir()
	path, oldest, newest := writeOversizedMemory(t, dir)
	before := len(testReadFile(t, path))
	wd := &WikiDreamer{workspaceDir: dir}

	dropped, err := wd.enforceMemoryDiskCap()
	if err != nil {
		t.Fatalf("enforceMemoryDiskCap: %v", err)
	}
	if dropped == 0 {
		t.Fatal("expected oldest timestamped sections to be dropped")
	}

	after := testReadFile(t, path)
	if len(after) > memoryDiskMaxBytes {
		t.Errorf("file still over cap: %d > %d", len(after), memoryDiskMaxBytes)
	}
	if len(after) >= before {
		t.Errorf("file did not shrink: before=%d after=%d", before, len(after))
	}
	// Head: preamble + category sections preserved verbatim.
	if !strings.HasPrefix(after, "# Memory") {
		t.Error("preamble lost")
	}
	if !strings.Contains(after, "## 결정사항") || !strings.Contains(after, "## 선호도") {
		t.Error("category sections must be preserved")
	}
	// Tail: newest timestamped section preserved.
	if !strings.Contains(after, "## "+newest) {
		t.Errorf("newest timestamped section %q must be preserved", newest)
	}
	// Oldest timestamped section dropped.
	if strings.Contains(after, "## "+oldest) {
		t.Errorf("oldest timestamped section %q should have been dropped", oldest)
	}
	// Dropped content recoverable in the backup.
	bak := testReadFile(t, path+".bak")
	if !strings.Contains(bak, "## "+oldest) {
		t.Error("dropped sections must be written to MEMORY.md.bak")
	}
	// No orphaned temp file.
	if _, statErr := os.Stat(path + ".diskcap.tmp"); !os.IsNotExist(statErr) {
		t.Error("disk-cap left an orphaned .tmp file")
	}

	// The rewritten file must still parse cleanly and round-trip.
	preamble, sections := parseMemorySections(after)
	if !strings.HasPrefix(preamble, "# Memory") {
		t.Error("post-cap preamble unparsable")
	}
	if len(sections) == 0 {
		t.Error("post-cap file has no sections")
	}
}

func TestEnforceMemoryDiskCap_NoOpUnderCap(t *testing.T) {
	dir := t.TempDir()
	original := writeMemoryFixture(t, dir) // small fixture, well under the cap
	wd := &WikiDreamer{workspaceDir: dir}

	dropped, err := wd.enforceMemoryDiskCap()
	if err != nil {
		t.Fatalf("enforceMemoryDiskCap: %v", err)
	}
	if dropped != 0 {
		t.Errorf("under-cap file must not be modified, dropped=%d", dropped)
	}
	after := testReadFile(t, filepath.Join(dir, memoryFileName))
	if after != original {
		t.Error("under-cap file was rewritten")
	}
	// No backup should be created for a no-op.
	if _, statErr := os.Stat(filepath.Join(dir, memoryFileName+".bak")); !os.IsNotExist(statErr) {
		t.Error("no-op must not create a .bak file")
	}
}

func TestEnforceMemoryDiskCap_DisabledAndMissing(t *testing.T) {
	// Empty workspace dir disables the cap.
	if dropped, err := (&WikiDreamer{}).enforceMemoryDiskCap(); err != nil || dropped != 0 {
		t.Errorf("empty workspaceDir must be a no-op, dropped=%d err=%v", dropped, err)
	}
	// Missing file is not an error.
	dir := t.TempDir()
	wd := &WikiDreamer{workspaceDir: dir}
	if dropped, err := wd.enforceMemoryDiskCap(); err != nil || dropped != 0 {
		t.Errorf("absent MEMORY.md must be a no-op, dropped=%d err=%v", dropped, err)
	}
}

func testReadFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

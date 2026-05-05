package wiki

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestWikiDreamerScanDiariesUsesOffsets(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	wd := NewWikiDreamer(store, nil, "", Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := os.MkdirAll(store.DiaryDir(), 0o755); err != nil {
		t.Fatalf("mkdir diary: %v", err)
	}
	diaryPath := filepath.Join(store.DiaryDir(), "diary-2026-05-05.md")
	if err := os.WriteFile(diaryPath, []byte("\n## 10:00\n\nfirst\n"), 0o644); err != nil {
		t.Fatalf("write diary: %v", err)
	}

	scan1, err := wd.scanDiaries(context.Background())
	if err != nil {
		t.Fatalf("scan1: %v", err)
	}
	if scan1 == nil || !strings.Contains(scan1.Content, "first") {
		t.Fatalf("scan1 content = %q, want first entry", scanContent(scan1))
	}
	if err := wd.saveDiaryProcessState(scan1.State); err != nil {
		t.Fatalf("save state: %v", err)
	}

	f, err := os.OpenFile(diaryPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open diary append: %v", err)
	}
	if _, err := f.WriteString("\n## 11:00\n\nsecond\n"); err != nil {
		_ = f.Close()
		t.Fatalf("append diary: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close diary: %v", err)
	}

	scan2, err := wd.scanDiaries(context.Background())
	if err != nil {
		t.Fatalf("scan2: %v", err)
	}
	if scan2 == nil || !strings.Contains(scan2.Content, "second") {
		t.Fatalf("scan2 content = %q, want second entry", scanContent(scan2))
	}
	if strings.Contains(scan2.Content, "first") {
		t.Fatalf("scan2 replayed already processed entry: %q", scan2.Content)
	}
}

func TestWikiDreamerScanDiariesDoesNotSkipLegacyCutoffDay(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	store.Index().LastProcessed = "2026-05-05"
	wd := NewWikiDreamer(store, nil, "", Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := os.MkdirAll(store.DiaryDir(), 0o755); err != nil {
		t.Fatalf("mkdir diary: %v", err)
	}
	diaryPath := filepath.Join(store.DiaryDir(), "diary-2026-05-05.md")
	if err := os.WriteFile(diaryPath, []byte("\n## 20:00\n\nsame-day entry\n"), 0o644); err != nil {
		t.Fatalf("write diary: %v", err)
	}

	scan, err := wd.scanDiaries(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if scan == nil || !strings.Contains(scan.Content, "same-day entry") {
		t.Fatalf("scan content = %q, want same-day entry", scanContent(scan))
	}
}

func TestProcessedDiaryCapsulesAreCappedAndFormatted(t *testing.T) {
	var capsules []processedDiaryCapsule
	for i := 0; i < processedCapsuleLimit+3; i++ {
		capsules = appendProcessedDiaryCapsule(capsules, processedDiaryCapsule{
			At:        "2026-05-05T00:00:00Z",
			DiaryDate: "2026-05-" + twoDigit(i+1),
			Proposed:  1,
			Created:   i % 2,
			Updated:   1,
			Paths:     []string{"프로젝트/deneb.md", "프로젝트/deneb.md"},
		})
	}
	if len(capsules) != processedCapsuleLimit {
		t.Fatalf("capsule count = %d, want %d", len(capsules), processedCapsuleLimit)
	}
	formatted := formatProcessedDiaryCapsules(capsules)
	if strings.Contains(formatted, "2026-05-01") {
		t.Fatalf("expected oldest capsules to be capped, got %q", formatted)
	}
	if !strings.Contains(formatted, "proposed=1") || !strings.Contains(formatted, "프로젝트/deneb.md") {
		t.Fatalf("expected compact processed history, got %q", formatted)
	}
	if strings.Count(formatted, "프로젝트/deneb.md") != processedCapsuleLimit {
		t.Fatalf("expected duplicated paths to be deduped per capsule, got %q", formatted)
	}
}

func TestDreamProposalReportWritesPreview(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	wd := NewWikiDreamer(store, nil, "", Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	scan := &diaryScanResult{Content: "diary content", LatestDate: "2026-05-05"}
	report := buildDreamProposalReport(scan, []wikiUpdate{{
		Action:     "create",
		Path:       "프로젝트/deneb.md",
		Title:      "Deneb",
		Summary:    "기억 개선",
		Category:   "프로젝트",
		Type:       "concept",
		Confidence: "medium",
		Importance: 0.8,
		Related:    []string{"결정/memory.md", "결정/memory.md"},
		Content:    strings.Repeat("긴 내용 ", 120),
	}})
	report.Applied = dreamApplySummary{Created: 1}

	if err := wd.saveDreamProposalReport(report); err != nil {
		t.Fatalf("saveDreamProposalReport: %v", err)
	}
	data, err := os.ReadFile(wd.dreamProposalPath())
	if err != nil {
		t.Fatalf("read proposal: %v", err)
	}
	var got dreamProposalReport
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("proposal json: %v", err)
	}
	if got.LatestDiaryDate != "2026-05-05" || got.Applied.Created != 1 {
		t.Fatalf("unexpected report metadata: %+v", got)
	}
	if len(got.Proposed) != 1 || got.Proposed[0].Path != "프로젝트/deneb.md" {
		t.Fatalf("unexpected proposals: %+v", got.Proposed)
	}
	if len(got.Proposed[0].Related) != 1 {
		t.Fatalf("expected related paths to be deduped: %+v", got.Proposed[0].Related)
	}
	if len([]rune(got.Proposed[0].ContentHint)) > 323 {
		t.Fatalf("content hint too long: %d", len([]rune(got.Proposed[0].ContentHint)))
	}
}

func scanContent(scan *diaryScanResult) string {
	if scan == nil {
		return ""
	}
	return scan.Content
}

func twoDigit(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

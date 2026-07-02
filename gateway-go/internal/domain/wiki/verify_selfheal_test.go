package wiki

import (
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestDetectDuplicates_NormalizedTitle: punctuation/spacing title variants get
// a high-confidence merge Fix; genuinely different titles stay advisory.
func TestDetectDuplicates_NormalizedTitle(t *testing.T) {
	idx := NewIndex()
	idx.UpdateEntry("프로젝트/영산고-태양광/대표.md", &Page{Meta: Frontmatter{Title: "영산고 태양광", Importance: 0.7}})
	idx.UpdateEntry("프로젝트/영산고태양광/대표.md", &Page{Meta: Frontmatter{Title: "영산고-태양광", Importance: 0.5}})
	idx.UpdateEntry("프로젝트/부산8호/대표.md", &Page{Meta: Frontmatter{Title: "부산 8호 태양광", Importance: 0.5}})

	findings := detectDuplicates(idx)
	var normFix *VerifyFinding
	for i := range findings {
		if findings[i].Type == "duplicate" && findings[i].Fix != nil &&
			strings.Contains(findings[i].Detail, "정규화") {
			normFix = &findings[i]
		}
		if findings[i].Fix != nil &&
			(findings[i].PageA == "프로젝트/부산8호/대표.md" || findings[i].PageB == "프로젝트/부산8호/대표.md") {
			t.Errorf("distinct title must not get an auto-fix: %+v", findings[i])
		}
	}
	if normFix == nil {
		t.Fatalf("expected a normalized-title merge fix, findings: %+v", findings)
	}
	// Higher-importance page is the keeper (PageA).
	if normFix.PageA != "프로젝트/영산고-태양광/대표.md" {
		t.Errorf("keeper = %s, want the higher-importance page", normFix.PageA)
	}
}

// TestDetectStaleSuperseded_ArchiveFlow: a page superseded and untouched for
// over the threshold gets an archive Fix, and applying it flips Archived.
func TestDetectStaleSuperseded_ArchiveFlow(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	old := time.Now().AddDate(0, 0, -(staleSupersededAfterDays + 10)).Format("2006-01-02")
	recent := time.Now().AddDate(0, 0, -3).Format("2006-01-02")

	mustWrite(t, store, "업무/구식-포트정책.md", &Page{
		Meta: Frontmatter{Title: "포트 정책 (구)", Category: "업무", SupersededBy: "업무/포트정책.md", Updated: old},
		Body: "옛 사실.",
	})
	mustWrite(t, store, "업무/최근-대체.md", &Page{
		Meta: Frontmatter{Title: "최근 대체됨", Category: "업무", SupersededBy: "업무/포트정책.md", Updated: recent},
		Body: "아직 유예 기간.",
	})

	wd := NewWikiDreamer(store, nil, "", Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	findings := wd.detectStaleSuperseded()
	if len(findings) != 1 || findings[0].PageA != "업무/구식-포트정책.md" {
		t.Fatalf("findings = %+v, want exactly the long-superseded page", findings)
	}
	if findings[0].Fix == nil || findings[0].Fix.Kind != "archive" {
		t.Fatalf("expected an archive fix, got %+v", findings[0].Fix)
	}

	if applied := wd.applyVerifyFixes(findings); applied != 1 {
		t.Fatalf("applied = %d, want 1", applied)
	}
	got, err := store.ReadPage("업무/구식-포트정책.md")
	if err != nil {
		t.Fatalf("read archived page: %v", err)
	}
	if !got.Meta.Archived {
		t.Error("page should be archived")
	}
	// Idempotent: a second detection pass skips archived pages.
	if again := wd.detectStaleSuperseded(); len(again) != 0 {
		t.Errorf("archived page re-flagged: %+v", again)
	}
}

// TestDetectStaleMailAnalyses_RetentionFlow: mail-analysis pages older than
// the retention window get an archive fix; fresh ones and non-mail pages don't.
func TestDetectStaleMailAnalyses_RetentionFlow(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	old := time.Now().AddDate(0, 0, -(mailAnalysisArchiveAfterDays + 5)).Format("2006-01-02")
	fresh := time.Now().AddDate(0, 0, -5).Format("2006-01-02")

	mustWrite(t, store, "프로젝트/영산고/메일분석/19e8717314b5c914.md", &Page{
		Meta: Frontmatter{Title: "RE: 견적", Category: "프로젝트", Type: "log", Updated: old},
		Body: "> Message ID: `19e8717314b5c914`\n분석",
	})
	mustWrite(t, store, "프로젝트/메일분석/19e8717314b5c915.md", &Page{
		Meta: Frontmatter{Title: "FW: 계약", Category: "프로젝트", Type: "log", Updated: fresh},
		Body: "> Message ID: `19e8717314b5c915`\n분석",
	})
	mustWrite(t, store, "프로젝트/영산고/대표.md", &Page{
		Meta: Frontmatter{Title: "영산고", Category: "프로젝트", Updated: old},
		Body: "# 영산고", // old but NOT a mail page → untouched
	})

	wd := NewWikiDreamer(store, nil, "", Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	findings := wd.detectStaleMailAnalyses()
	if len(findings) != 1 || findings[0].PageA != "프로젝트/영산고/메일분석/19e8717314b5c914.md" {
		t.Fatalf("findings = %+v, want exactly the old mail page", findings)
	}
	if findings[0].Fix == nil || findings[0].Fix.Kind != "archive" {
		t.Fatalf("expected archive fix, got %+v", findings[0].Fix)
	}
	if applied := wd.applyVerifyFixes(findings); applied != 1 {
		t.Fatalf("applied = %d, want 1", applied)
	}
	got := testMustRead(t, store, "프로젝트/영산고/메일분석/19e8717314b5c914.md")
	if !got.Meta.Archived {
		t.Error("old mail page should be archived")
	}
	if again := wd.detectStaleMailAnalyses(); len(again) != 0 {
		t.Errorf("archived mail page re-flagged: %+v", again)
	}
}

func testMustRead(t *testing.T, s *Store, path string) *Page {
	t.Helper()
	p, err := s.ReadPage(path)
	if err != nil {
		t.Fatalf("ReadPage(%s): %v", path, err)
	}
	return p
}

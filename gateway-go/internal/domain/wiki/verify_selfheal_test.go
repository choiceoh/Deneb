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

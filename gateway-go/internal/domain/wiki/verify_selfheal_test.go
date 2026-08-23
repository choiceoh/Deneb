package wiki

import (
	"io"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// TestDetectDuplicates_NormalizedTitle: punctuation/spacing title variants get
// a high-confidence merge Fix; genuinely different titles stay advisory.
func TestDetectDuplicates_NormalizedTitle(t *testing.T) {
	idx := newIndex()
	idx.updateEntry("프로젝트/영산고-태양광/대표.md", &Page{Meta: Frontmatter{Title: "영산고 태양광", Importance: 0.7}})
	idx.updateEntry("프로젝트/영산고태양광/대표.md", &Page{Meta: Frontmatter{Title: "영산고-태양광", Importance: 0.5}})
	idx.updateEntry("프로젝트/부산8호/대표.md", &Page{Meta: Frontmatter{Title: "부산 8호 태양광", Importance: 0.5}})

	findings := detectDuplicates(idx.Entries)
	var normFix *verifyFinding
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

// TestDetectDuplicates_IsDeterministicAcrossRuns: the finding text must not
// depend on Go's randomized map iteration. Live 2026-07: the same pair rendered
// with its two members swapped from one dream cycle to the next, so one logical
// finding was reported as two distinct strings — 6,632 finding lines over 14
// days collapsing to 1,779 unique, top pairs appearing ~62 times in EACH
// direction. (The Korean near-name pairs that used to drive those counts are no
// longer reported at all — see the CJK test below — so this fixture uses the
// two finding kinds that remain.)
func TestDetectDuplicates_IsDeterministicAcrossRuns(t *testing.T) {
	build := func() map[string]IndexEntry {
		idx := newIndex()
		// Spelling variants that normalize equal (the auto-mergeable kind)…
		idx.updateEntry("프로젝트/영산고 태양광.md", &Page{Meta: Frontmatter{Title: "영산고 태양광"}})
		idx.updateEntry("프로젝트/영산고-태양광.md", &Page{Meta: Frontmatter{Title: "영산고-태양광"}})
		// …and latin titles a letter apart (the advisory kind).
		idx.updateEntry("프로젝트/solar-farm-a.md", &Page{Meta: Frontmatter{Title: "solar-farm-a"}})
		idx.updateEntry("프로젝트/solar-farm-b.md", &Page{Meta: Frontmatter{Title: "solar-farm-b"}})
		return idx.Entries
	}
	render := func(fs []verifyFinding) []string {
		out := make([]string, 0, len(fs))
		for _, f := range fs {
			out = append(out, f.Detail)
		}
		sort.Strings(out)
		return out
	}

	want := render(detectDuplicates(build()))
	if len(want) == 0 {
		t.Fatal("expected similar-title findings for the fixture")
	}
	// Fresh maps each round: map iteration order is re-randomized per map, so a
	// handful of rounds reliably catches an order-dependent rendering.
	for round := range 12 {
		got := render(detectDuplicates(build()))
		if len(got) != len(want) {
			t.Fatalf("round %d: finding count changed: %d vs %d\n%v\n%v", round, len(got), len(want), got, want)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("round %d: finding text is order-dependent:\n got %q\nwant %q", round, got[i], want[i])
			}
		}
	}
}

// TestDetectDuplicates_RejectsAutoMergeAcrossDifferentMessageIDsButMergesSameID:
// two 메일분석 pages sharing a subject but carrying different Message IDs are
// DISTINCT Gmail messages (메일 1통 = 1페이지) and must never get an auto-merge
// Fix — the 2026-06 mis-merge that folded 14 different-ID mails together on
// normalized-title equality. A genuine same-mail duplicate (same ID in two
// buckets) still merges.
func TestDetectDuplicates_RejectsAutoMergeAcrossDifferentMessageIDsButMergesSameID(t *testing.T) {
	const sameTitle = "Re: Re: [해밀고흥솔라팜] 모듈 제작 마스터 스케쥴 요청의 건"

	t.Run("different msgID is never auto-merged", func(t *testing.T) {
		idx := newIndex()
		idx.updateEntry("프로젝트/해밀고흥솔라팜-모듈/메일분석/19eaa3e4371576a3.md",
			&Page{Meta: Frontmatter{Title: sameTitle, Importance: 0.3}})
		idx.updateEntry("프로젝트/해밀고흥솔라팜-모듈/메일분석/19eaa3aa72de312b.md",
			&Page{Meta: Frontmatter{Title: sameTitle, Importance: 0.3}})
		for _, f := range detectDuplicates(idx.Entries) {
			if f.Fix != nil {
				t.Errorf("distinct mails (different Message IDs) got an auto-fix: %+v", f)
			}
		}
	})

	t.Run("same msgID across buckets still merges", func(t *testing.T) {
		idx := newIndex()
		idx.updateEntry("프로젝트/메일분석/19eaa3e4371576a3.md",
			&Page{Meta: Frontmatter{Title: sameTitle, Importance: 0.3}})
		idx.updateEntry("프로젝트/해밀고흥솔라팜-모듈/메일분석/19eaa3e4371576a3.md",
			&Page{Meta: Frontmatter{Title: sameTitle, Importance: 0.5}})
		merged := false
		for _, f := range detectDuplicates(idx.Entries) {
			if f.Fix != nil && f.Fix.Kind == "merge" {
				merged = true
			}
		}
		if !merged {
			t.Error("same-mail duplicate across buckets should still get a merge Fix")
		}
	})
}

func TestDetectDuplicates_SkipsSiblingProjectFuzzy(t *testing.T) {
	idx := newIndex()
	idx.updateEntry("프로젝트/pl2-kia-epc-001/대표.md", &Page{Meta: Frontmatter{Title: "solar-farm-a"}})
	idx.updateEntry("프로젝트/pl2-kia-epc-002/대표.md", &Page{Meta: Frontmatter{Title: "solar-farm-b"}})
	for _, f := range detectDuplicates(idx.Entries) {
		if f.Type == "duplicate" {
			t.Fatalf("sibling deals must not fuzzy-match: %+v", f)
		}
	}

	// Same project splintered into two folder spellings still merges.
	idx2 := newIndex()
	idx2.updateEntry("프로젝트/영산고-태양광/대표.md", &Page{Meta: Frontmatter{Title: "영산고 태양광", Importance: 0.7}})
	idx2.updateEntry("프로젝트/영산고태양광/대표.md", &Page{Meta: Frontmatter{Title: "영산고-태양광", Importance: 0.5}})
	found := false
	for _, f := range detectDuplicates(idx2.Entries) {
		if f.Fix != nil && f.Fix.Kind == "merge" {
			found = true
		}
	}
	if !found {
		t.Fatal("normalized-title twins of the same name must still merge")
	}
}

// TestDetectStaleSuperseded_ArchivesPastThresholdSkipsRecentAndIdempotent: a page
// superseded and untouched for over the threshold gets an archive Fix, and
// applying it flips Archived.
func TestDetectStaleSuperseded_ArchivesPastThresholdSkipsRecentAndIdempotent(t *testing.T) {
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

// TestDetectStaleMailAnalyses_ArchivesPastRetentionSkipsFreshAndNonMailIdempotent:
// mail-analysis pages older than the retention window get an archive fix;
// fresh ones and non-mail pages don't.
func TestDetectStaleMailAnalyses_ArchivesPastRetentionSkipsFreshAndNonMailIdempotent(t *testing.T) {
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

// Regression: the Levenshtein rule treated a one-syllable difference in a short
// Korean name or place as a near-duplicate. On the live wiki that produced 106
// similar-title warnings per dream cycle with essentially no true positive —
// 94 of them pairs of plainly different people — re-reported every night.
func TestDetectDuplicates_KoreanNamesOneSyllableApartAreNotDuplicates(t *testing.T) {
	t.Parallel()
	for _, pair := range [][2]string{
		{"김노영", "김유영"}, // 서로 다른 인물
		{"강동민", "강동화"},
		{"김갑수", "김덕수"},
		{"진도군", "완도군"}, // 서로 다른 지명
		{"서산시", "아산시"},
		{"울산", "부산"},
		{"기아-광주 진행 로그", "기아-광명 진행 로그"}, // 서로 다른 현장
	} {
		if isSimilar(pair[0], pair[1]) {
			t.Errorf("isSimilar(%q, %q) = true; a syllable apart in CJK is a different word", pair[0], pair[1])
		}
	}
	// The signal that DOES survive: spelling variants that normalize equal. That
	// path (normalizeTitleKey) is the one allowed to auto-merge.
	if normalizeTitleKey("영산고 태양광") != normalizeTitleKey("영산고-태양광") {
		t.Error("punctuation/spacing variants must still fold to one key")
	}
	// Latin text keeps the distance rule — one letter of a short word there is
	// ordinary spelling variance.
	if !isSimilar("Smith", "Smyth") {
		t.Error("isSimilar(Smith, Smyth) = false; the alphabet rule must stay")
	}
	if !isSimilar("solar-farm-a", "solar-farm-b") {
		t.Error("isSimilar on long latin ids regressed")
	}
}

func TestDetectStalePersonStubs_ArchivesOldUnusedKeepsRecalled(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	old := time.Now().AddDate(0, 0, -(personStubArchiveAfterDays + 5)).Format("2006-01-02")
	stub := NewPage("김스텁", "인물", nil)
	stub.Meta.Created = old
	stub.Meta.Updated = old
	stub.Meta.Summary = "주소록 기반 자동 생성"
	stub.Body = "# 김스텁\n\n## 변경 이력\n- 드림 사이클 반복 언급으로 자동 생성 (주소록 연동)\n"
	if err := store.WritePage("인물/김스텁.md", stub); err != nil {
		t.Fatal(err)
	}
	fresh := NewPage("최신스텁", "인물", nil)
	fresh.Meta.Summary = "주소록 기반 자동 생성"
	if err := store.WritePage("인물/최신스텁.md", fresh); err != nil {
		t.Fatal(err)
	}
	curated := NewPage("김실장", "인물", nil)
	curated.Meta.Created = old
	curated.Meta.Summary = "남도에코 실장"
	if err := store.WritePage("인물/김실장.md", curated); err != nil {
		t.Fatal(err)
	}

	wd := &WikiDreamer{store: store, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	findings := wd.detectStalePersonStubs()
	if len(findings) != 1 || findings[0].PageA != "인물/김스텁.md" || findings[0].Fix == nil || findings[0].Fix.Kind != "archive" {
		t.Fatalf("want one archive fix on the old unused stub, got %+v", findings)
	}
	if n := wd.applyVerifyFixes(findings); n != 1 {
		t.Fatalf("apply=%d, want 1", n)
	}
	got := testMustRead(t, store, "인물/김스텁.md")
	if !got.Meta.Archived {
		t.Fatal("old stub must be archived")
	}
}

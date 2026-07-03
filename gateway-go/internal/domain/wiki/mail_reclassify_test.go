package wiki

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func newReclassifyStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), ""))
	t.Cleanup(func() { store.Close() })

	for _, name := range []string{"기아-화성", "해남-희망에너지-epc"} {
		rep := NewPage(name, "프로젝트", nil)
		rep.Body = "# " + name
		if err := store.WritePage(RepPagePath(name), rep); err != nil {
			t.Fatal(err)
		}
	}
	return store
}

func writeUnlinkedMail(t *testing.T, store *Store, id, title string, related []string) {
	t.Helper()
	page := NewPage(title, "프로젝트", nil)
	page.Meta.Type = "log"
	page.Meta.Related = related
	page.Body = "> Message ID: `" + id + "`\n분석"
	if err := store.WritePage(MailAnalysisPagePath("", id), page); err != nil {
		t.Fatal(err)
	}
}

// TestReclassifyUnlinkedMailAnalyses: related-signal and unique-title-signal
// mails re-file into their project's 메일분석 slot (gaining the 대표 edge);
// ambiguous and signal-less mails stay put.
func TestReclassifyUnlinkedMailAnalyses(t *testing.T) {
	store := newReclassifyStore(t)
	now := time.Date(2026, 7, 3, 9, 0, 0, 0, time.UTC)

	writeUnlinkedMail(t, store, "m1", "RE: 견적 회신", []string{"프로젝트/기아-화성/대표.md"}) // signal 1
	writeUnlinkedMail(t, store, "m2", "해남 희망에너지 EPC 낙찰 통보", nil)                 // signal 2 (unique title)
	writeUnlinkedMail(t, store, "m3", "기아 화성 + 해남 희망에너지 EPC 비교", nil)            // ambiguous → stays
	writeUnlinkedMail(t, store, "m4", "뉴스레터", nil)                               // no signal → stays

	moved := store.ReclassifyUnlinkedMailAnalyses(now, 10)
	if len(moved) != 2 {
		t.Fatalf("moved = %+v, want exactly the two signal mails", moved)
	}

	m1 := testutil.Must(store.ReadPage(MailAnalysisPagePath("기아-화성", "m1")))
	hasRep := false
	for _, r := range m1.Meta.Related {
		if r == RepPagePath("기아-화성") {
			hasRep = true
		}
	}
	if !hasRep {
		t.Errorf("re-filed mail missing the 대표 edge: %v", m1.Meta.Related)
	}
	if _, err := store.ReadPage(MailAnalysisPagePath("해남-희망에너지-epc", "m2")); err != nil {
		t.Errorf("title-signal mail not re-filed: %v", err)
	}
	for _, stay := range []string{"m3", "m4"} {
		if _, err := store.ReadPage(MailAnalysisPagePath("", stay)); err != nil {
			t.Errorf("%s should have stayed in the unlinked bucket: %v", stay, err)
		}
	}

	// Idempotent: nothing left to move.
	if again := store.ReclassifyUnlinkedMailAnalyses(now, 10); len(again) != 0 {
		t.Errorf("second pass moved again: %+v", again)
	}
}

// TestReclassifyTarget_TwoDistinctRelatedProjectsIsAmbiguous (M23): a mail
// whose Related cites TWO different projects has no unambiguous home — the
// doctrine is 모호하면 잔류, and returning the first Related entry was arbitrary.
// Repeated citations of the SAME project stay a valid signal.
func TestReclassifyTarget_TwoDistinctRelatedProjectsIsAmbiguous(t *testing.T) {
	store := newReclassifyStore(t)
	projects := store.KnownProjects()

	ambiguous := &Page{Meta: Frontmatter{
		Title: "비교 검토",
		Related: []string{
			"프로젝트/기아-화성/대표.md",
			"프로젝트/해남-희망에너지-epc/대표.md",
		},
	}}
	if got := reclassifyTarget(ambiguous, projects); got != "" {
		t.Errorf("two distinct related projects must be ambiguous, got %q", got)
	}

	sameTwice := &Page{Meta: Frontmatter{
		Title: "견적 회신",
		Related: []string{
			"프로젝트/기아-화성/대표.md",
			`프로젝트\기아-화성\로그.md`, // same project, other slot + windows separators
		},
	}}
	if got := reclassifyTarget(sameTwice, projects); got != "기아-화성" {
		t.Errorf("agreeing related entries = %q, want 기아-화성", got)
	}

	// End-to-end: the ambiguous mail stays in the unlinked bucket.
	writeUnlinkedMail(t, store, "amb1", "비교 검토",
		[]string{"프로젝트/기아-화성/대표.md", "프로젝트/해남-희망에너지-epc/대표.md"})
	if moved := store.ReclassifyUnlinkedMailAnalyses(time.Now(), 10); len(moved) != 0 {
		t.Errorf("ambiguous mail was re-filed: %+v", moved)
	}
}

// TestReclassifyUnlinkedMailAnalyses_Cap: the per-call cap holds.
func TestReclassifyUnlinkedMailAnalyses_Cap(t *testing.T) {
	store := newReclassifyStore(t)
	writeUnlinkedMail(t, store, "c1", "기아 화성 문의 1", nil)
	writeUnlinkedMail(t, store, "c2", "기아 화성 문의 2", nil)
	writeUnlinkedMail(t, store, "c3", "기아 화성 문의 3", nil)

	if moved := store.ReclassifyUnlinkedMailAnalyses(time.Now(), 2); len(moved) != 2 {
		t.Fatalf("moved = %d, want cap 2", len(moved))
	}
}

// TestMatchProjectsInText: normalized containment, specificity order, the
// short-name guard, and closed-project exclusion.
func TestMatchProjectsInText(t *testing.T) {
	store := newReclassifyStore(t)

	got := store.MatchProjectsInText("기아 화성 근황 알려줘", 2)
	if len(got) != 1 || got[0].Path != RepPagePath("기아-화성") {
		t.Fatalf("match = %+v, want 기아-화성 대표", got)
	}

	// Most specific first when several match.
	both := store.MatchProjectsInText("기아 화성이랑 해남 희망에너지 EPC 어떻게 됐어", 2)
	if len(both) != 2 || both[0].Path != RepPagePath("해남-희망에너지-epc") {
		t.Fatalf("matches = %+v, want the longer key first", both)
	}

	if hits := store.MatchProjectsInText("아무 프로젝트도 없는 문장", 2); len(hits) != 0 {
		t.Errorf("unexpected matches: %+v", hits)
	}

	// Closed projects never anchor.
	if _, err := store.CloseProject("기아-화성", "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if hits := store.MatchProjectsInText("기아 화성 근황", 2); len(hits) != 0 {
		t.Errorf("closed project matched: %+v", hits)
	}
}

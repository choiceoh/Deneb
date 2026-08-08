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

// TestReclassifyUnlinkedMailAnalysesReturnsMoved: related-signal and
// unique-title-signal mails re-file into their project's 메일분석 slot
// (gaining the 대표 edge); ambiguous and signal-less mails stay put.
func TestReclassifyUnlinkedMailAnalysesReturnsMoved(t *testing.T) {
	store := newReclassifyStore(t)
	now := time.Date(2026, 7, 3, 9, 0, 0, 0, time.UTC)

	writeUnlinkedMail(t, store, "m1", "RE: 견적 회신", []string{"프로젝트/기아-화성/대표.md"}) // signal 1
	writeUnlinkedMail(t, store, "m2", "해남 희망에너지 EPC 낙찰 통보", nil)                 // signal 2 (unique title)
	writeUnlinkedMail(t, store, "m3", "기아 화성 + 해남 희망에너지 EPC 비교", nil)            // ambiguous → stays
	writeUnlinkedMail(t, store, "m4", "뉴스레터", nil)                               // no signal → stays

	moved, _ := store.ReclassifyUnlinkedMailAnalyses(now, 10)
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
	if again, _ := store.ReclassifyUnlinkedMailAnalyses(now, 10); len(again) != 0 {
		t.Errorf("second pass moved again: %+v", again)
	}
}

// TestReclassifyTarget_TwoDistinctRelatedProjectsReturnsEmpty (M23): a mail
// whose Related cites TWO different projects has no unambiguous home — the
// doctrine is 모호하면 잔류, and returning the first Related entry was arbitrary.
// Repeated citations of the SAME project stay a valid signal.
func TestReclassifyTarget_TwoDistinctRelatedProjectsReturnsEmpty(t *testing.T) {
	store := newReclassifyStore(t)
	projects := store.KnownProjects()

	ambiguous := &Page{Meta: Frontmatter{
		Title: "비교 검토",
		Related: []string{
			"프로젝트/기아-화성/대표.md",
			"프로젝트/해남-희망에너지-epc/대표.md",
		},
	}}
	if got, _ := reclassifyTarget(ambiguous, projects); got != "" {
		t.Errorf("two distinct related projects must be ambiguous, got %q", got)
	}

	sameTwice := &Page{Meta: Frontmatter{
		Title: "견적 회신",
		Related: []string{
			"프로젝트/기아-화성/대표.md",
			`프로젝트\기아-화성\로그.md`, // same project, other slot + windows separators
		},
	}}
	if got, _ := reclassifyTarget(sameTwice, projects); got != "기아-화성" {
		t.Errorf("agreeing related entries = %q, want 기아-화성", got)
	}

	// End-to-end: the ambiguous mail stays in the unlinked bucket.
	writeUnlinkedMail(t, store, "amb1", "비교 검토",
		[]string{"프로젝트/기아-화성/대표.md", "프로젝트/해남-희망에너지-epc/대표.md"})
	if moved, _ := store.ReclassifyUnlinkedMailAnalyses(time.Now(), 10); len(moved) != 0 {
		t.Errorf("ambiguous mail was re-filed: %+v", moved)
	}
}

// TestReclassifyUnlinkedMailAnalyses_CapEnforcesBoundary: the per-call cap holds.
func TestReclassifyUnlinkedMailAnalyses_CapEnforcesBoundary(t *testing.T) {
	store := newReclassifyStore(t)
	writeUnlinkedMail(t, store, "c1", "기아 화성 문의 1", nil)
	writeUnlinkedMail(t, store, "c2", "기아 화성 문의 2", nil)
	writeUnlinkedMail(t, store, "c3", "기아 화성 문의 3", nil)

	if moved, _ := store.ReclassifyUnlinkedMailAnalyses(time.Now(), 2); len(moved) != 2 {
		t.Fatalf("moved = %d, want cap 2", len(moved))
	}
}

// writeFiledMail plants an analysis mail INSIDE a project's 메일분석 slot with a
// sender-domain tag — the evidence the domain histogram counts.
func writeFiledMail(t *testing.T, store *Store, project, id, domain string) {
	t.Helper()
	page := NewPage("RE: "+id, "프로젝트", []string{domain})
	page.Meta.Type = "log"
	page.Body = "분석"
	if err := store.WritePage(MailAnalysisPagePath(project, id), page); err != nil {
		t.Fatal(err)
	}
}

// TestDomainSignalObserveThenArmed: the sunkean shape from the 2026-08-08
// measurement — a domain with ≥3 filed mails under exactly one project. In
// observe mode (default) the mail stays put and comes back as a proposal; with
// DENEB_MAIL_RECLASS_DOMAIN=1 it moves, stamped signal="domain".
func TestDomainSignalObserveThenArmed(t *testing.T) {
	store := newReclassifyStore(t)
	for i, id := range []string{"f1", "f2", "f3"} {
		_ = i
		writeFiledMail(t, store, "기아-화성", id, "sunkean.com")
	}
	writeUnlinkedMail(t, store, "u1", "견적 회신드립니다", nil) // no related/title signal
	// The unlinked mail carries the sender-domain tag like real analyzer output.
	_ = store.UpdatePage(MailAnalysisPagePath("", "u1"), func(cur *Page) (*Page, error) {
		cur.Meta.Tags = []string{"sunkean.com"}
		return cur, nil
	})

	// Observe mode: proposal surfaces, nothing moves.
	t.Setenv("DENEB_MAIL_RECLASS_DOMAIN", "")
	moved, proposals := store.ReclassifyUnlinkedMailAnalyses(time.Now(), 10)
	if len(moved) != 0 {
		t.Fatalf("observe mode must not move, got %+v", moved)
	}
	if len(proposals) != 1 || proposals[0].Project != "기아-화성" || proposals[0].Signal != "domain" {
		t.Fatalf("proposals = %+v, want one domain proposal for 기아-화성", proposals)
	}
	if _, err := store.ReadPage(MailAnalysisPagePath("", "u1")); err != nil {
		t.Fatalf("observe mode moved the page: %v", err)
	}

	// Armed: the same evidence now files the mail.
	t.Setenv("DENEB_MAIL_RECLASS_DOMAIN", "1")
	moved, proposals = store.ReclassifyUnlinkedMailAnalyses(time.Now(), 10)
	if len(proposals) != 0 {
		t.Errorf("armed runs must not emit proposals, got %+v", proposals)
	}
	if len(moved) != 1 || moved[0].Signal != "domain" || moved[0].Project != "기아-화성" {
		t.Fatalf("moved = %+v, want the domain-signal move", moved)
	}
	if _, err := store.ReadPage(MailAnalysisPagePath("기아-화성", "u1")); err != nil {
		t.Errorf("armed move missing at destination: %v", err)
	}
}

// TestDomainSignalGuards: every firing condition individually blocks —
// under-evidence (<3), split evidence (two projects), blocklisted domains
// (internal + freemail), and archived-project evidence exclusion.
func TestDomainSignalGuards(t *testing.T) {
	t.Setenv("DENEB_MAIL_RECLASS_DOMAIN", "1")

	t.Run("under_evidence_stays", func(t *testing.T) {
		store := newReclassifyStore(t)
		writeFiledMail(t, store, "기아-화성", "f1", "acme.co.kr")
		writeFiledMail(t, store, "기아-화성", "f2", "acme.co.kr") // only 2 < 3
		writeUnlinkedMail(t, store, "u1", "문의", nil)
		_ = store.UpdatePage(MailAnalysisPagePath("", "u1"), func(cur *Page) (*Page, error) {
			cur.Meta.Tags = []string{"acme.co.kr"}
			return cur, nil
		})
		if moved, props := store.ReclassifyUnlinkedMailAnalyses(time.Now(), 10); len(moved)+len(props) != 0 {
			t.Errorf("K=3 guard failed: moved=%+v props=%+v", moved, props)
		}
	})

	t.Run("split_evidence_stays", func(t *testing.T) {
		store := newReclassifyStore(t)
		for _, id := range []string{"f1", "f2", "f3"} {
			writeFiledMail(t, store, "기아-화성", id, "acme.co.kr")
		}
		writeFiledMail(t, store, "해남-희망에너지-epc", "f4", "acme.co.kr") // one competitor kills it
		writeUnlinkedMail(t, store, "u1", "문의", nil)
		_ = store.UpdatePage(MailAnalysisPagePath("", "u1"), func(cur *Page) (*Page, error) {
			cur.Meta.Tags = []string{"acme.co.kr"}
			return cur, nil
		})
		if moved, props := store.ReclassifyUnlinkedMailAnalyses(time.Now(), 10); len(moved)+len(props) != 0 {
			t.Errorf("unanimity guard failed: moved=%+v props=%+v", moved, props)
		}
	})

	t.Run("blocklisted_domains_stay", func(t *testing.T) {
		store := newReclassifyStore(t)
		for _, dom := range []string{"topsolar.kr", "naver.com"} {
			for _, id := range []string{"f1", "f2", "f3"} {
				writeFiledMail(t, store, "기아-화성", dom+"-"+id, dom)
			}
			mailID := "u-" + dom
			writeUnlinkedMail(t, store, mailID, "문의", nil)
			_ = store.UpdatePage(MailAnalysisPagePath("", mailID), func(cur *Page) (*Page, error) {
				cur.Meta.Tags = []string{dom}
				return cur, nil
			})
		}
		if moved, props := store.ReclassifyUnlinkedMailAnalyses(time.Now(), 10); len(moved)+len(props) != 0 {
			t.Errorf("blocklist guard failed: moved=%+v props=%+v", moved, props)
		}
	})

	t.Run("archived_project_evidence_excluded", func(t *testing.T) {
		store := newReclassifyStore(t)
		for _, id := range []string{"f1", "f2", "f3"} {
			writeFiledMail(t, store, "기아-화성", id, "acme.co.kr")
		}
		if _, err := store.CloseProject("기아-화성", "", time.Now()); err != nil {
			t.Fatal(err)
		}
		writeUnlinkedMail(t, store, "u1", "문의", nil)
		_ = store.UpdatePage(MailAnalysisPagePath("", "u1"), func(cur *Page) (*Page, error) {
			cur.Meta.Tags = []string{"acme.co.kr"}
			return cur, nil
		})
		if moved, props := store.ReclassifyUnlinkedMailAnalyses(time.Now(), 10); len(moved)+len(props) != 0 {
			t.Errorf("closed-project evidence must not fire: moved=%+v props=%+v", moved, props)
		}
	})
}

// newClientGroupStore builds two projects of one 거래처 plus an unrelated one —
// the fixture for client-key anchoring and exactly-one resolution.
func newClientGroupStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), ""))
	t.Cleanup(func() { store.Close() })

	for name, client := range map[string]string{
		"금호타이어-곡성-1단계": "금호타이어",
		"금호타이어-곡성-2단계": "금호타이어",
		"영산고":          "",
	} {
		rep := NewPage(name, "프로젝트", nil)
		rep.Meta.Client = client
		rep.Body = "# " + name
		if err := store.WritePage(RepPagePath(name), rep); err != nil {
			t.Fatal(err)
		}
	}
	return store
}

// TestMatchProjectsInText_ClientKeyReturnsAllProjects: a bare 거래처 mention
// anchors every project of that client (the 거래처-as-top-level recall
// behavior), while unrelated projects stay out.
func TestMatchProjectsInText_ClientKeyReturnsAllProjects(t *testing.T) {
	store := newClientGroupStore(t)

	got := store.MatchProjectsInText("금호타이어 요즘 어떻게 되고 있어?", 5)
	if len(got) != 2 {
		t.Fatalf("client mention matches = %+v, want the two 금호타이어 projects", got)
	}
	for _, ref := range got {
		if ref.Client != "금호타이어" {
			t.Errorf("matched a non-client project: %+v", ref)
		}
	}
}

// TestUniqueProjectInTextReturnsSpecificMatch: exactly-one resolution is
// specificity-aware — a bare client mention ties across the client's projects
// (no arbitrary pick), a specific title wins despite the sibling's client-key
// hit.
func TestUniqueProjectInTextReturnsSpecificMatch(t *testing.T) {
	store := newClientGroupStore(t)

	if ref, ok := store.UniqueProjectInText("금호타이어 회의"); ok {
		t.Errorf("bare client mention must tie, got %+v", ref)
	}
	ref, ok := store.UniqueProjectInText("금호타이어 곡성 1단계 자재 검토")
	if !ok || ref.Path != RepPagePath("금호타이어-곡성-1단계") {
		t.Fatalf("specific title = %+v (ok=%v), want 곡성-1단계", ref, ok)
	}
	if ref, ok := store.UniqueProjectInText("영산고 발전 근황"); !ok || ref.Path != RepPagePath("영산고") {
		t.Fatalf("plain name = %+v (ok=%v), want 영산고", ref, ok)
	}
	if _, ok := store.UniqueProjectInText("아무 관련 없는 문장"); ok {
		t.Error("no identity key in text must not resolve")
	}
}

// TestReclassifyTarget_ClientMentionReturnsEmpty: mail titled with only the
// 거래처 stays in the unlinked bucket (tie), while a project-specific title
// still files even though the sibling project also hits via the shared
// client key.
func TestReclassifyTarget_ClientMentionReturnsEmpty(t *testing.T) {
	store := newClientGroupStore(t)
	projects := store.KnownProjects()

	bare := &Page{Meta: Frontmatter{Title: "금호타이어 태양광 문의"}}
	if got, _ := reclassifyTarget(bare, projects); got != "" {
		t.Errorf("bare client title must stay put, got %q", got)
	}
	specific := &Page{Meta: Frontmatter{Title: "금호타이어 곡성 2단계 준공 서류"}}
	if got, _ := reclassifyTarget(specific, projects); got != "금호타이어-곡성-2단계" {
		t.Errorf("specific title = %q, want 금호타이어-곡성-2단계", got)
	}
}

// TestMatchProjectsInTextReturnsRankedMatches: normalized containment,
// specificity order, the short-name guard, and closed-project exclusion.
func TestMatchProjectsInTextReturnsRankedMatches(t *testing.T) {
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

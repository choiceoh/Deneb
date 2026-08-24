package wiki

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

func seedSourcePage(t *testing.T, store *Store, path, updated, body string) {
	t.Helper()
	page := NewPage(path, "프로젝트", nil)
	page.Meta.Updated = updated
	page.Body = body
	if err := store.WritePage(path, page); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// A source ref counts as corroboration only when the store can open the page
// and read the asserted value inside it. That is a report, not an authority
// (ADR-0005) — see the refusal cases below for what it deliberately withholds.
func TestVerifyFactSourceAcceptsOnlyAProvenValue(t *testing.T) {
	store, _, _ := newFactTestStore(t)
	seedSourcePage(t, store, "프로젝트/abc-견적.md", "2026-07-01",
		"# ABC 견적\n\n- 견적 금액: 1억 2,000만원\n- 담당: 최 차장\n")

	verified := store.VerifyFactSource("w:프로젝트/abc-견적", "1억 2,000만원")
	if !verified.Verified {
		t.Fatalf("a page that states the value must verify: %+v", verified)
	}
	if got := verified.BasisAt.Format("2006-01-02"); got != "2026-07-01" {
		t.Errorf("basis date = %q, want the page's own date", got)
	}
	if !strings.Contains(verified.Path, "abc-견적") {
		t.Errorf("resolved path = %q", verified.Path)
	}

	// A value the page does not state cannot be promoted by citing it.
	if absent := store.VerifyFactSource("w:프로젝트/abc-견적", "8,000만원"); absent.Verified {
		t.Errorf("a page that does not state the value must not verify: %+v", absent)
	}
}

func TestVerifyFactSourceRefusesUnusableRefs(t *testing.T) {
	store, _, _ := newFactTestStore(t)
	if _, err := store.UpsertFact(FactInput{
		Subject: "self", Key: "communication.language", Value: "한국어로 답변",
		Kind: FactKindPreference, Authority: FactAuthorityDirectUser,
		At: time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	_, active := store.ActiveFactSnapshot("self")
	if len(active) != 1 {
		t.Fatalf("seed facts = %d", len(active))
	}

	cases := []struct {
		name   string
		ref    string
		value  string
		reason string
	}{
		{"empty ref", "", "값", "empty source ref"},
		{"missing page", "w:프로젝트/없는페이지", "값", "not readable"},
		{"escaping path", "w:../../etc/passwd", "root", "not readable"},
		{
			// A fact citing the fact plane would corroborate itself.
			"fact reference", "@facts/" + active[0].ID + ".md", "한국어로 답변",
			"cannot vouch for a fact",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			evidence := store.VerifyFactSource(tc.ref, tc.value)
			if evidence.Verified {
				t.Fatalf("%q must not verify: %+v", tc.ref, evidence)
			}
			if !strings.Contains(evidence.Reason, tc.reason) {
				t.Fatalf("reason = %q, want it to mention %q", evidence.Reason, tc.reason)
			}
		})
	}
}

// The page date fed the withdrawn promotion. A dateless page still states what
// it states, so it must not be reported as a bad citation.
func TestVerifyFactSourceDoesNotRequireAPageDate(t *testing.T) {
	store, _, _ := newFactTestStore(t)
	seedSourcePage(t, store, "프로젝트/무날짜.md", "", "- 금액: 5,000만원\n")

	evidence := store.VerifyFactSource("w:프로젝트/무날짜", "5,000만원")
	if !evidence.Verified {
		t.Fatalf("a dateless page that states the value must verify: %+v", evidence)
	}
	if !evidence.BasisAt.IsZero() {
		t.Errorf("a dateless page must report no date, got %v", evidence.BasisAt)
	}
}

func TestVerifyFactSourcesReturnsTheProvingRef(t *testing.T) {
	store, _, _ := newFactTestStore(t)
	seedSourcePage(t, store, "프로젝트/계약.md", "2026-08-01", "- 납기: 2026-12-31\n")

	evidence, ok := store.VerifyFactSources(
		[]string{"w:프로젝트/없는페이지", "w:프로젝트/계약"}, "2026-12-31",
	)
	if !ok || !strings.Contains(evidence.Path, "계약") {
		t.Fatalf("verification should skip to the proving ref: %+v ok=%v", evidence, ok)
	}

	// With nothing proven the caller still learns why, so it can fix the ref.
	failed, ok := store.VerifyFactSources([]string{"w:프로젝트/계약"}, "2027-01-01")
	if ok || failed.Reason == "" {
		t.Fatalf("unproven sources must report a reason: %+v ok=%v", failed, ok)
	}
}

// A ref naming another knowledge layer is a valid citation this store cannot
// open. Reporting it as a bad ref would send the model to fix working input.
func TestVerifyFactSourceLeavesOtherLayersUnchecked(t *testing.T) {
	store, _, _ := newFactTestStore(t)
	seedSourcePage(t, store, "프로젝트/계약.md", "2026-08-01", "- 납기: 2026-12-31\n")

	files := store.VerifyFactSource("f:/계약/견적서.pdf", "2026-12-31")
	if files.Verified || files.Checked {
		t.Fatalf("a files ref must be left unchecked: %+v", files)
	}
	if !strings.Contains(files.Reason, "cannot open") {
		t.Errorf("reason = %q", files.Reason)
	}

	// A wiki ref alongside it still gets judged, and a checked refusal is the one
	// worth reporting back when nothing verifies.
	if evidence, ok := store.VerifyFactSources(
		[]string{"f:/계약/견적서.pdf", "w:프로젝트/계약"}, "2026-12-31"); !ok || !evidence.Checked {
		t.Fatalf("the wiki ref should still verify: %+v ok=%v", evidence, ok)
	}
	unproven, ok := store.VerifyFactSources(
		[]string{"f:/계약/견적서.pdf", "w:프로젝트/계약"}, "2027-01-01")
	if ok || !unproven.Checked {
		t.Fatalf("a checked refusal should outrank an unchecked one: %+v ok=%v", unproven, ok)
	}
}

// Structured pages state their facts in frontmatter. A page whose deadline
// lives in `due:` backs a deadline claim just as a prose line would.
func TestVerifyFactSourceReadsFrontmatterClaims(t *testing.T) {
	store, _, _ := newFactTestStore(t)
	page := NewPage("프로젝트/모듈-납품.md", "프로젝트", nil)
	page.Meta.Updated = "2026-08-01"
	page.Meta.Due = "2026-12-31"
	page.Meta.Status = "진행중"
	page.Body = "# 모듈 납품\n\n본문에는 날짜가 없다.\n"
	if err := store.WritePage("프로젝트/모듈-납품.md", page); err != nil {
		t.Fatal(err)
	}

	for _, value := range []string{"2026-12-31", "진행중"} {
		if evidence := store.VerifyFactSource("w:프로젝트/모듈-납품", value); !evidence.Verified {
			t.Errorf("frontmatter claim %q must verify: %+v", value, evidence)
		}
	}
	if absent := store.VerifyFactSource("w:프로젝트/모듈-납품", "2027-01-01"); absent.Verified {
		t.Errorf("a value in neither body nor frontmatter must not verify: %+v", absent)
	}
}

// Frontmatter fields are searched one at a time. Joining them would let two
// unrelated fields run together into a phrase neither one states.
func TestVerifyFactSourceDoesNotMatchAcrossFields(t *testing.T) {
	store, _, _ := newFactTestStore(t)
	page := NewPage("프로젝트/교차.md", "프로젝트", nil)
	page.Meta.Updated = "2026-08-01"
	page.Meta.Status = "진행중"
	page.Meta.Client = "ABC건설"
	page.Body = "# 교차\n"
	if err := store.WritePage("프로젝트/교차.md", page); err != nil {
		t.Fatal(err)
	}

	if spliced := store.VerifyFactSource("w:프로젝트/교차", "진행중 ABC건설"); spliced.Verified {
		t.Errorf("a phrase spliced from two fields must not verify: %+v", spliced)
	}
	if single := store.VerifyFactSource("w:프로젝트/교차", "ABC건설"); !single.Verified {
		t.Errorf("a value one field states must still verify: %+v", single)
	}
}

// The generated projection restates the fact plane, so citing it corroborates a
// claim with itself one page removed.
func TestVerifyFactSourceRefusesTheGeneratedProjection(t *testing.T) {
	store, _, _ := newFactTestStore(t)
	if _, err := store.UpsertFact(FactInput{
		Subject: "self", Key: "communication.language", Value: "한국어로 답변",
		Kind: FactKindPreference, Authority: FactAuthorityAgent,
		At: time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}

	evidence := store.VerifyFactSource("w:"+factProfilePagePath, "한국어로 답변")
	if evidence.Verified {
		t.Fatalf("the projection must not corroborate a fact: %+v", evidence)
	}
	if !strings.Contains(evidence.Reason, "projection") {
		t.Errorf("reason = %q", evidence.Reason)
	}
}

// The tool schema shows `file:/계약/2026-01.pdf` as a source_refs example, so
// both spellings of the files layer must be left unjudged rather than reported
// as broken — otherwise the tool tells the model its own documentation is wrong.
func TestVerifyFactSourceAcceptsBothFilesLayerSpellings(t *testing.T) {
	store, _, _ := newFactTestStore(t)
	for _, ref := range []string{"f:/계약/2026-01.pdf", "file:/계약/2026-01.pdf"} {
		evidence := store.VerifyFactSource(ref, "값")
		if evidence.Checked || evidence.Verified {
			t.Errorf("%q must be left unjudged: %+v", ref, evidence)
		}
		if !strings.Contains(evidence.Reason, "files layer") {
			t.Errorf("%q reason = %q", ref, evidence.Reason)
		}
	}
}

// Canonical frontmatter states facts in every shape it has — a slice of sites, a
// numeric capacity, a program slug — and a citation to one of them is good.
func TestVerifyFactSourceReadsEveryCanonicalMetaShape(t *testing.T) {
	store, _, _ := newFactTestStore(t)
	page := NewPage("프로젝트/군산.md", "프로젝트", []string{"태그값"})
	page.Meta.Updated = "2026-08-01"
	page.Meta.Sites = []string{"전북 군산시 옥구읍 수산리"}
	page.Meta.Program = "비금-130mw"
	page.Meta.Capacity = 130.9
	page.Body = "# 군산\n"
	if err := store.WritePage("프로젝트/군산.md", page); err != nil {
		t.Fatal(err)
	}

	for _, value := range []string{"전북 군산시 옥구읍 수산리", "비금-130mw", "130.9"} {
		if evidence := store.VerifyFactSource("w:프로젝트/군산", value); !evidence.Verified {
			t.Errorf("canonical meta %q must back a claim: %+v", value, evidence)
		}
	}
	// Tags are navigation, not a claim the page makes about its subject.
	if tagged := store.VerifyFactSource("w:프로젝트/군산", "태그값"); tagged.Verified {
		t.Errorf("a tag must not read as a stated fact: %+v", tagged)
	}
}

// A prefix naming no real layer is a bad citation, not an unjudged one: nothing
// could open it, so the caller must hear about it.
func TestVerifyFactSourceReportsUnknownLayers(t *testing.T) {
	store, _, _ := newFactTestStore(t)
	evidence := store.VerifyFactSource("x:계약서", "값")
	if evidence.Verified || !evidence.Checked {
		t.Fatalf("an unknown layer must be reported as a bad ref: %+v", evidence)
	}
	if !strings.Contains(evidence.Reason, "unknown layer") {
		t.Errorf("reason = %q", evidence.Reason)
	}
}

// A caller that ignores the schema's maxItems must not turn one tool call into
// unbounded disk reads: only the refs the store would keep are opened.
func TestVerifyFactSourcesBoundsTheRefsItReads(t *testing.T) {
	store, _, _ := newFactTestStore(t)
	seedSourcePage(t, store, "프로젝트/계약.md", "2026-08-01", "- 납기: 2026-12-31\n")

	flood := make([]string, 0, 200)
	for i := range 200 {
		flood = append(flood, "w:프로젝트/없는페이지-"+strconv.Itoa(i))
	}
	// The proving ref sits at the front, outside the tail the store keeps, so a
	// bounded verification cannot find it — that is the bound being enforced.
	if _, ok := store.VerifyFactSources(append([]string{"w:프로젝트/계약"}, flood...), "2026-12-31"); ok {
		t.Fatal("verification read past the refs the store would keep")
	}
	if _, ok := store.VerifyFactSources(append(flood, "w:프로젝트/계약"), "2026-12-31"); !ok {
		t.Fatal("a kept ref must still verify")
	}
}

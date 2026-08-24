package wiki

import (
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

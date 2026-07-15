package wikitool

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
)

func TestFormatContacts_UnifiedRefAndFormat(t *testing.T) {
	matches := []tooldeps.Contact{
		{Name: "박부장", Phones: []string{"010-1234-5678"}, Org: "탑솔라", Emails: []string{"park@topsolar.com"}},
		{Name: "", Phones: []string{"010-0000-0000"}},
	}
	out := formatContacts(matches, "박", "")

	if !strings.Contains(out, "🔍") || !strings.Contains(out, "2건") || !strings.Contains(out, "주소록") {
		t.Errorf("expected shared recall header, got: %q", out)
	}
	// Person carries a c:-namespaced ref, consistent with w:/h:/p:.
	if !strings.Contains(out, "`c:박부장`") {
		t.Errorf("expected c: person ref, got: %q", out)
	}
	if !strings.Contains(out, "010-1234-5678 · 탑솔라") {
		t.Errorf("expected phone·org meta, got: %q", out)
	}
	if !strings.Contains(out, "park@topsolar.com") {
		t.Errorf("expected email snippet, got: %q", out)
	}
	// Blank name falls back without dropping the ref.
	if !strings.Contains(out, "`c:(이름 없음)`") {
		t.Errorf("expected fallback ref for nameless contact, got: %q", out)
	}
	// Cross-link to the curated wiki person page.
	if !strings.Contains(out, "w:인물/") {
		t.Errorf("expected cross-link to wiki person page, got: %q", out)
	}
}

func TestFormatContacts_NoMatch(t *testing.T) {
	if out := formatContacts(nil, "없는사람", ""); !strings.Contains(out, "없음") {
		t.Errorf("expected no-match message, got: %q", out)
	}
}

// by_company enumerates everyone at an org, name-sorted, org match
// case-insensitive substring.
func TestContactsByOrgReturnsSortedRoster(t *testing.T) {
	all := []tooldeps.Contact{
		{Name: "이차장", Org: "현대차 구매팀"},
		{Name: "김부장", Org: "현대차"},
		{Name: "박대리", Org: "탑솔라"},
	}
	got := contactsByOrg(all, "현대차")
	if len(got) != 2 || got[0].Name != "김부장" || got[1].Name != "이차장" {
		t.Errorf("unexpected roster: %+v", got)
	}
	if len(contactsByOrg(all, "")) != 0 {
		t.Errorf("empty query must return nothing")
	}
}

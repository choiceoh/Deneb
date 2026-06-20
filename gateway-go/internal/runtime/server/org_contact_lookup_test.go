package server

// Tests for orgContactLookup — the miniapp.org.get member enrichment wiring that
// matches org-chart member names to the contacts store via wiki.NormalizePersonName.
//
// FAKE names/numbers only — never real contacts.

import (
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/contacts"
)

// newContactStore returns a temp-backed contacts store seeded with the given
// entries (fully replacing the snapshot, as a native sync would).
func newContactStore(t *testing.T, entries ...contacts.Contact) *contacts.Store {
	t.Helper()
	s, err := contacts.NewStore(filepath.Join(t.TempDir(), "contacts.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if _, err := s.ReplaceAll(entries); err != nil {
		t.Fatalf("ReplaceAll: %v", err)
	}
	return s
}

func TestOrgContactLookup_NilStoreReturnsNilFunc(t *testing.T) {
	if orgContactLookup(nil) != nil {
		t.Fatal("orgContactLookup(nil) should be nil so enrichment is skipped")
	}
}

func TestOrgContactLookup_ExactName(t *testing.T) {
	store := newContactStore(t, contacts.Contact{
		Name:   "김철수",
		Phones: []string{"010-1111-2222"},
		Emails: []string{"chulsoo@example.test"},
	})
	lookup := orgContactLookup(store)
	phones, emails := lookup("김철수")
	if len(phones) != 1 || phones[0] != "010-1111-2222" {
		t.Fatalf("phones = %v, want [010-1111-2222]", phones)
	}
	if len(emails) != 1 || emails[0] != "chulsoo@example.test" {
		t.Fatalf("emails = %v, want [chulsoo@example.test]", emails)
	}
}

func TestOrgContactLookup_NormalizesTitleAndAffiliation(t *testing.T) {
	store := newContactStore(t, contacts.Contact{
		Name:   "김철수",
		Phones: []string{"010-3333-4444"},
	})
	lookup := orgContactLookup(store)
	// Member names carry honorifics / affiliation parentheticals; all normalize
	// to "김철수" and match the bare contact.
	for _, member := range []string{"김철수 부장", "김철수대표님", "김철수(예시그룹)"} {
		phones, _ := lookup(member)
		if len(phones) != 1 || phones[0] != "010-3333-4444" {
			t.Fatalf("lookup(%q) phones = %v, want [010-3333-4444]", member, phones)
		}
	}
}

func TestOrgContactLookup_NoSubstringMismatch(t *testing.T) {
	// "이수" must NOT match the contact "이수민" — matching is exact on the
	// normalized key, not a substring.
	store := newContactStore(t, contacts.Contact{
		Name:   "이수민",
		Phones: []string{"010-5555-6666"},
	})
	lookup := orgContactLookup(store)
	if phones, emails := lookup("이수"); len(phones) != 0 || len(emails) != 0 {
		t.Fatalf("lookup(이수) = %v / %v, want empty (no substring match)", phones, emails)
	}
}

func TestOrgContactLookup_NoMatchEmpty(t *testing.T) {
	store := newContactStore(t, contacts.Contact{Name: "이영희", Phones: []string{"010-0000-0000"}})
	lookup := orgContactLookup(store)
	if phones, emails := lookup("박지성"); len(phones) != 0 || len(emails) != 0 {
		t.Fatalf("unmatched lookup = %v / %v, want empty", phones, emails)
	}
}

func TestOrgContactLookup_BlankNameEmpty(t *testing.T) {
	store := newContactStore(t, contacts.Contact{Name: "김철수", Phones: []string{"010-1111-2222"}})
	lookup := orgContactLookup(store)
	if phones, emails := lookup("   "); len(phones) != 0 || len(emails) != 0 {
		t.Fatalf("blank-name lookup = %v / %v, want empty", phones, emails)
	}
}

func TestOrgContactLookup_HomonymsUnionAndDedup(t *testing.T) {
	// Two contacts normalize to the same name ("김철수" and "김철수 부장"): their
	// phones/emails are unioned, and a value shared across both appears once.
	store := newContactStore(t,
		contacts.Contact{Name: "김철수", Phones: []string{"010-1111-2222"}, Emails: []string{"shared@example.test"}},
		contacts.Contact{Name: "김철수 부장", Phones: []string{"010-1111-2222", "010-7777-8888"}, Emails: []string{"shared@example.test"}},
	)
	lookup := orgContactLookup(store)
	phones, emails := lookup("김철수")
	if len(phones) != 2 {
		t.Fatalf("phones = %v, want 2 unioned/deduped", phones)
	}
	if !contains(phones, "010-1111-2222") || !contains(phones, "010-7777-8888") {
		t.Fatalf("phones = %v, want both numbers", phones)
	}
	if len(emails) != 1 || emails[0] != "shared@example.test" {
		t.Fatalf("emails = %v, want one deduped shared address", emails)
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

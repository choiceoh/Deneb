package knowledge

import (
	"fmt"
	"testing"

	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
)

// TestLoadWikiPeople_LoadsEveryPageAndCapsOnlyTheResponseTail: the row cap used
// to break the LOAD loop, so with 306 person pages the alphabetical tail was
// invisible to people.list matching and to person.dossier — a lookup for a
// person late in the alphabet found no wiki page even though it existed.
func TestLoadWikiPeople_LoadsEveryPageAndCapsOnlyTheResponseTail(t *testing.T) {
	pages := make(map[string]*wiki.Page, maxPeopleWikiRows+6)
	for i := range maxPeopleWikiRows + 5 {
		rel := fmt.Sprintf("인물/p%03d.md", i)
		pages[rel] = &wiki.Page{Meta: wiki.Frontmatter{
			Title: fmt.Sprintf("사람%03d", i), Category: "인물", Updated: "2026-06-01",
		}}
	}
	// The person the old cap hid: last alphabetically, reachable only by email.
	pages["인물/zzz.md"] = &wiki.Page{Meta: wiki.Frontmatter{
		Title: "황재훈", Category: "인물", Updated: "2026-06-02",
		Emails: []string{"hwang@partner.co"}, // frontmatter only — no 연락처 section
	}}
	store := peopleWikiStore(pages)

	people := loadWikiPeople(func() (MemorySearcher, error) { return store, nil })
	if len(people) != len(pages) {
		t.Fatalf("loaded %d people, want all %d", len(people), len(pages))
	}

	// Matching sees the tail: an email that lives only in frontmatter resolves.
	rows := mergeWikiPeople([]PersonRow{{Name: "unknown", Email: "hwang@partner.co"}}, people)
	if rows[0].WikiPath != "인물/zzz.md" {
		t.Errorf("frontmatter-only email did not match: %+v", rows[0])
	}

	// The response tail is still bounded.
	tail := mergeWikiPeople(nil, people)
	if len(tail) != maxPeopleWikiRows {
		t.Errorf("wiki-only tail = %d rows, want the %d cap", len(tail), maxPeopleWikiRows)
	}
}

func TestLoadWikiPeople_ExcludesArchivedAndEffectivelySupersededPages(t *testing.T) {
	pages := map[string]*wiki.Page{
		"인물/live.md": {
			Meta: wiki.Frontmatter{Title: "현행 인물", Category: "인물"},
		},
		"인물/archived.md": {
			Meta: wiki.Frontmatter{Title: "보관 인물", Category: "인물", Archived: true},
		},
		"인물/old.md": {
			Meta: wiki.Frontmatter{Title: "구 인물", Category: "인물", SupersededBy: "인물/new.md"},
		},
	}
	got := loadWikiPeople(func() (MemorySearcher, error) { return peopleWikiStore(pages), nil })
	if len(got) != 1 || got[0].path != "인물/live.md" {
		t.Fatalf("loadWikiPeople returned historical rows: %+v", got)
	}
}

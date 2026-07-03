package server

import (
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

func TestCounterpartyLookup(t *testing.T) {
	t.Run("nil store yields no boost and retries after TTL", func(t *testing.T) {
		var store *wiki.Store
		c := newCounterpartyLookup(func() *wiki.Store { return store })
		if c.Has("kim@acme.co.kr") {
			t.Fatal("no store → no boost")
		}
		// Store appears (late-bound session phase); force a rebuild by expiring.
		store = seedCounterpartyStore(t)
		c.builtAt = time.Now().Add(-2 * counterpartyTTL)
		if !c.Has("kim@acme.co.kr") {
			t.Fatal("active counterparty domain should match after the store binds")
		}
	})

	t.Run("matches by domain, rejects freemail and malformed", func(t *testing.T) {
		store := seedCounterpartyStore(t)
		c := newCounterpartyLookup(func() *wiki.Store { return store })
		if !c.Has("anyone@ACME.co.kr") {
			t.Error("domain match must be case-insensitive and per-domain, not per-address")
		}
		for _, miss := range []string{"kim@other.com", "kim@gmail.com", "no-at-sign", "trailing@"} {
			if c.Has(miss) {
				t.Errorf("Has(%q) = true, want false", miss)
			}
		}
	})
}

// seedCounterpartyStore builds a wiki store with one fresh project-linked mail
// analysis from acme.co.kr and one freemail one (must not count).
func seedCounterpartyStore(t *testing.T) *wiki.Store {
	t.Helper()
	store, err := wiki.NewStore(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	today := time.Now().UTC().Format("2006-01-02")
	for _, p := range []struct{ path, tag string }{
		{"프로젝트/영산고/메일분석/m1.md", "acme.co.kr"},
		{"프로젝트/영산고/메일분석/m2.md", "gmail.com"},
	} {
		if err := store.WritePage(p.path, &wiki.Page{
			Meta: wiki.Frontmatter{
				Title: p.path, Category: "프로젝트", Type: "log", Confidence: "medium",
				Tags: []string{p.tag}, Created: today, Updated: today,
			},
			Body: "> From: x <x@" + p.tag + ">\n\n분석",
		}); err != nil {
			t.Fatal(err)
		}
	}
	return store
}

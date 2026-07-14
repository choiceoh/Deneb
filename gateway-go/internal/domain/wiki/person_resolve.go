// person_resolve.go — resolve external display names (org chart members, agent
// mentions) to their 인물 wiki page paths. Any caller that holds only a person's
// name — the org chart editor linking a member to their knowledge page, the org
// recall source surfacing who a query is about — uses this to bridge into the
// wiki. The bridge deliberately lives in the wiki package (not org/): wiki owns
// person-page paths, while the contacts domain supplies the shared name key.

package wiki

import (
	"strings"

	contactdomain "github.com/choiceoh/deneb/gateway-go/internal/domain/contacts"
)

// ResolvePersonPaths maps each display name to its 인물 page relPath, for the
// names that have a page. Matching uses contacts.NormalizePersonName — the same
// normalization contact enrichment uses — so "오선택 전무", "오선택(탑솔라)" and
// "오선택" all resolve to the one 인물/오선택… page. A name with no page is simply
// absent from the result (callers treat a missing key as "no wiki page yet").
//
// One disk scan (listPeopleByName) resolves the whole batch, so callers with
// many names — the org GET handler resolving every member — pass them all at
// once rather than calling per name. Within the current wiki one normalized name
// maps to exactly one page, so 동명이인 that were merged onto a shared page
// resolve to that shared page; distinguishing them needs more than a name.
func (s *Store) ResolvePersonPaths(names []string) map[string]string {
	if len(names) == 0 {
		return nil
	}
	people, err := s.listPeopleByName()
	if err != nil || len(people) == 0 {
		return nil
	}
	out := make(map[string]string, len(names))
	for _, name := range names {
		key := contactdomain.NormalizePersonName(name)
		if len([]rune(key)) < 2 {
			continue
		}
		if p, ok := people[key]; ok {
			out[name] = p.path
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// resolvePeopleByEmail maps each email address to the 인물 page that declares it
// in frontmatter `emails:` — the robust identity join name matching cannot do.
// An email is unique to one identity, so 동명이인 (김성훈@marsh vs 김성훈@bohae)
// resolve to different pages, and mail senders / org members / contacts all land
// on the ONE page that owns their address. Keys are lowercased; an address no
// page claims is absent. One disk scan resolves the whole batch.
func (s *Store) resolvePeopleByEmail(emails []string) map[string]string {
	if len(emails) == 0 {
		return nil
	}
	idx := s.indexPeopleByEmail()
	if len(idx) == 0 {
		return nil
	}
	out := make(map[string]string, len(emails))
	for _, e := range emails {
		key := strings.ToLower(strings.TrimSpace(e))
		if key == "" {
			continue
		}
		if p, ok := idx[key]; ok {
			out[key] = p
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ResolvePersonByEmail resolves a single address to its 인물 page relPath, or ""
// when no page claims it. Thin single-address wrapper over resolvePeopleByEmail.
func (s *Store) ResolvePersonByEmail(email string) string {
	m := s.resolvePeopleByEmail([]string{email})
	return m[strings.ToLower(strings.TrimSpace(email))]
}

// indexPeopleByEmail builds email(lowercased) → 인물 page relPath from every 인물
// page's frontmatter `emails:`. First writer wins on the rare chance two pages
// claim one address (a data error worth surfacing elsewhere, not here). nil when
// no page declares any address.
func (s *Store) indexPeopleByEmail() map[string]string {
	relPaths, err := s.ListPages("인물")
	if err != nil {
		return nil
	}
	out := make(map[string]string, len(relPaths))
	for _, path := range relPaths {
		page, rerr := s.ReadPage(path)
		if rerr != nil || page == nil {
			continue
		}
		for _, e := range page.Meta.Emails {
			key := strings.ToLower(strings.TrimSpace(e))
			if key == "" {
				continue
			}
			if _, exists := out[key]; !exists {
				out[key] = path
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

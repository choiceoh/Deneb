// person_emails.go — backfill 인물 page frontmatter `emails:` from the address
// book so the 인물 page becomes the email-keyed identity record. Once a page
// declares its addresses, mail senders / org members / contacts all resolve to
// it by email (ResolvePersonByEmail) instead of by the name that conflates
// 동명이인. A page whose name matches people at DIFFERENT companies (동명이인) is
// left for a human to split — guessing its address would re-create the very
// conflation this is meant to end.

package wiki

import "strings"

// (freemailDomains — consumer mail hosts whose domain says nothing about the
// employer — is defined in counterparties.go and reused here: a same-name contact
// at one is that person's personal address, not a distinct company that would
// signal a homonym.)

// PersonEmailResult reports an emails backfill: pages whose identity addresses
// were written, and pages skipped because the name maps to homonyms.
type PersonEmailResult struct {
	Updated   []string // person-page titles whose emails: was filled/changed
	Ambiguous []string // titles skipped: name matches 동명이인 (distinct company domains)
}

// EnrichPersonEmails writes each 인물 page's identity email(s) into its frontmatter
// from the address book. A page whose name resolves to a single identity gets its
// addresses; a page whose name matches homonyms at distinct companies is skipped
// and flagged (a human splits it — see the person-homonym-conflation memory).
// Best-effort: one unwritable page never aborts the rest.
func (s *Store) EnrichPersonEmails(book []Contact) (PersonEmailResult, error) {
	var res PersonEmailResult
	if len(book) == 0 {
		return res, nil
	}
	byName := make(map[string][]Contact)
	for _, c := range book {
		key := NormalizePersonName(c.Name)
		if len([]rune(key)) < 2 {
			continue
		}
		byName[key] = append(byName[key], c)
	}
	people, err := s.listPeopleByName()
	if err != nil {
		return res, err
	}
	for key, pp := range people {
		emails, ambiguous := identityEmails(byName[key])
		if ambiguous {
			res.Ambiguous = append(res.Ambiguous, pp.title)
			continue
		}
		if len(emails) == 0 {
			continue
		}
		changed, werr := s.setPersonEmails(pp.path, emails)
		if werr == nil && changed {
			res.Updated = append(res.Updated, pp.title)
		}
	}
	return res, nil
}

// identityEmails collapses the contacts that share a name into that page's
// identity addresses. One identity — a single contact, or several that are the
// same person (same company, or only a personal-mail domain differs) — yields the
// union of their emails. Contacts at two or more DISTINCT company (non-freemail)
// domains are 동명이인: no single identity, so ambiguous=true and no addresses.
func identityEmails(cs []Contact) (emails []string, ambiguous bool) {
	if len(cs) == 0 {
		return nil, false
	}
	companyDomains := make(map[string]bool)
	var all []string
	for _, c := range cs {
		for _, e := range c.Emails {
			e = strings.ToLower(strings.TrimSpace(e))
			if e == "" {
				continue
			}
			all = append(all, e)
			if at := strings.LastIndex(e, "@"); at >= 0 && at+1 < len(e) {
				dom := e[at+1:]
				if _, free := freemailDomains[dom]; !free {
					companyDomains[dom] = true
				}
			}
		}
	}
	if len(companyDomains) >= 2 {
		return nil, true // homonym: distinct employers under one name
	}
	return dedupeLowerStrings(all), false
}

// setPersonEmails sets the page's frontmatter emails when they differ from what is
// already there, returning whether it wrote. A no-op (unchanged set) skips the
// write via UpdatePage's nil-next contract.
func (s *Store) setPersonEmails(relPath string, emails []string) (bool, error) {
	want := dedupeLowerStrings(emails)
	var changed bool
	err := s.UpdatePage(relPath, func(page *Page) (*Page, error) {
		if page == nil || sameStringSet(page.Meta.Emails, want) {
			return nil, nil
		}
		page.Meta.Emails = want
		changed = true
		return page, nil
	})
	return changed, err
}

// dedupeLowerStrings lowercases, trims, drops blanks, and dedupes preserving
// first-seen order.
func dedupeLowerStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// sameStringSet reports whether a and b hold the same set of values (order-
// independent), used to skip a no-op frontmatter write.
func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := make(map[string]bool, len(a))
	for _, s := range a {
		m[strings.ToLower(strings.TrimSpace(s))] = true
	}
	for _, s := range b {
		if !m[strings.ToLower(strings.TrimSpace(s))] {
			return false
		}
	}
	return true
}

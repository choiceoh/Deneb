// person_emails.go — backfill 인물 page frontmatter `emails:` from the address
// book so the 인물 page becomes the email-keyed identity record. Once a page
// declares its addresses, mail senders / org members / contacts all resolve to
// it by email (ResolvePersonByEmail) instead of by the name that conflates
// 동명이인. A page whose name matches people at DIFFERENT companies (동명이인) is
// left for a human to split — guessing its address would re-create the very
// conflation this is meant to end.

package wiki

import (
	"strings"
	"unicode"
)

// EnrichEmployeePages creates an 인물 page for every address-book contact whose
// email is at one of ourDomains (the operator's own company) — our staff, who
// should be first-class identities even when the wiki has never mentioned them.
// External people stay curated by mention (the dreamer), so this never floods the
// category with the whole phone book. Reuses EnrichPeople(createMissing): existing
// pages are enriched in place (not duplicated), and 직급 variants of one name
// ("고건", "고건 주임") collapse via NormalizePersonName. Measured: +220 staff stubs
// held R@8 at 99% (a bare page text-matches only its own name), so surfacing them
// needs no retrieval gate. Names that don't start with a letter (junk contact
// rows like "#이성기…") are skipped.
func (s *Store) EnrichEmployeePages(book []Contact, ourDomains []string) (PeopleEnrichResult, error) {
	if len(book) == 0 || len(ourDomains) == 0 {
		return PeopleEnrichResult{}, nil
	}
	dom := make(map[string]bool, len(ourDomains))
	for _, d := range ourDomains {
		if d = strings.ToLower(strings.TrimSpace(d)); d != "" {
			dom[d] = true
		}
	}
	seen := make(map[string]bool)
	var names []string
	for _, c := range book {
		name := strings.TrimSpace(c.Name)
		if len([]rune(name)) < 2 || seen[name] {
			continue
		}
		if r := []rune(name)[0]; !unicode.IsLetter(r) {
			continue // junk row (leading '#', punctuation, …)
		}
		for _, e := range c.Emails {
			at := strings.LastIndex(e, "@")
			if at < 0 || at+1 >= len(e) {
				continue
			}
			if dom[strings.ToLower(strings.TrimSpace(e[at+1:]))] {
				seen[name] = true
				names = append(names, personPageName(name)) // clean title, drop 직급/junk
				break
			}
		}
	}
	if len(names) == 0 {
		return PeopleEnrichResult{}, nil
	}
	return s.EnrichPeople(names, book, true)
}

// EnrichDealMentionedPages gives an 인물 page to each EXTERNAL contact (has a
// company email, none at ourDomains) whose name is actually named in a 프로젝트
// (project/deal) page — the counterparties involved in our deals, i.e. our
// "important external" people. The whole-phone-book flood is avoided three ways:
// only company-email contacts, only those a 프로젝트 page names, and a ≥3-rune name
// (a 2-rune name matches too much prose). Reuses EnrichPeople(createMissing), so
// existing pages are enriched not duplicated; our own staff are EnrichEmployeePages'
// job and everyone else stays mention-curated by the dreamer. A name shared by
// 동명이인 (최종원 = SK vs LONGi) still resolves to one page; its identity email is
// left for EnrichPersonEmails to flag, not guessed here.
func (s *Store) EnrichDealMentionedPages(book []Contact, ourDomains []string) (PeopleEnrichResult, error) {
	if len(book) == 0 {
		return PeopleEnrichResult{}, nil
	}
	our := domainSet(ourDomains)
	paths, err := s.ListPages("프로젝트")
	if err != nil {
		return PeopleEnrichResult{}, err
	}
	var sb strings.Builder
	for _, p := range paths {
		if page, e := s.ReadPage(p); e == nil && page != nil {
			sb.WriteString(page.Body)
			sb.WriteByte('\n')
		}
	}
	if sb.Len() == 0 {
		return PeopleEnrichResult{}, nil
	}
	deals := strings.ToLower(sb.String())

	seen := make(map[string]bool)
	var names []string
	for _, c := range book {
		name := strings.TrimSpace(c.Name)
		if len([]rune(name)) < 2 || seen[name] {
			continue
		}
		if r := []rune(name)[0]; !unicode.IsLetter(r) {
			continue // junk row
		}
		if !hasCompanyEmailOutside(c, our) {
			continue // no external company address → not a deal counterparty
		}
		clean := personPageName(name) // "박준영 팀장(한화)" → "박준영"
		if len([]rune(clean)) < 3 {
			continue // a 2-rune name matches too much prose
		}
		if strings.Contains(deals, strings.ToLower(clean)) {
			seen[name] = true
			names = append(names, clean)
		}
	}
	if len(names) == 0 {
		return PeopleEnrichResult{}, nil
	}
	return s.EnrichPeople(names, book, true)
}

// personPageName returns a clean 인물 page title for a contact name: the leading
// run of Korean syllables, dropping the trailing 직급/회사/junk the address book
// staples on ("박준영 팀장(한화시스템)" → "박준영"). A name with no 2–4 syllable
// Korean prefix (a foreign name, a one-syllable handle) is kept as-is so it is
// not silently mangled.
func personPageName(name string) string {
	var b []rune
	for _, r := range strings.TrimSpace(name) {
		if r >= 0xAC00 && r <= 0xD7A3 { // Hangul syllable block
			b = append(b, r)
			continue
		}
		break
	}
	if len(b) >= 2 && len(b) <= 4 {
		return string(b)
	}
	return strings.TrimSpace(name)
}

// hasCompanyEmailOutside reports whether the contact has a non-freemail email at
// a domain NOT in ourDomains — an external company address.
func hasCompanyEmailOutside(c Contact, ourDomains map[string]bool) bool {
	for _, e := range c.Emails {
		at := strings.LastIndex(e, "@")
		if at < 0 || at+1 >= len(e) {
			continue
		}
		d := strings.ToLower(strings.TrimSpace(e[at+1:]))
		if d == "" || ourDomains[d] {
			continue
		}
		if _, free := freemailDomains[d]; !free {
			return true
		}
	}
	return false
}

// domainSet lowercases a domain slice into a lookup set.
func domainSet(domains []string) map[string]bool {
	out := make(map[string]bool, len(domains))
	for _, d := range domains {
		if d = strings.ToLower(strings.TrimSpace(d)); d != "" {
			out[d] = true
		}
	}
	return out
}

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
		// 2+ employers under one name: either 동명이인 (different people) or 이직 (one
		// person who moved companies). People keep their mobile across a job move, so
		// a phone shared between the entries means ONE identity — union both addresses
		// (전 직장 + 현 직장). Distinct phones across distinct companies mean different
		// people → leave for a human to split, don't guess an address.
		if contactsSharePhone(cs) {
			return dedupeLowerStrings(all), false // 이직 / same person, keep both
		}
		return nil, true // 동명이인
	}
	return dedupeLowerStrings(all), false
}

// contactsSharePhone reports whether two or more of these same-name contacts carry
// a common phone number — the signal that they are one person (a 이직 job move
// keeps the mobile), not homonyms. Digits-only compare; numbers under 9 digits are
// ignored as too short to be a reliable identity key.
func contactsSharePhone(cs []Contact) bool {
	owners := make(map[string]int)
	for _, c := range cs {
		seen := make(map[string]bool)
		for _, p := range c.Phones {
			n := digitsOnly(p)
			if len(n) < 9 || seen[n] {
				continue
			}
			seen[n] = true
			owners[n]++
			if owners[n] >= 2 {
				return true
			}
		}
	}
	return false
}

// digitsOnly strips a phone string down to its digits.
func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
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

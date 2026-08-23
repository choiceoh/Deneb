package wiki

import (
	"encoding/json"
	"fmt"
	"strings"

	contactdomain "github.com/choiceoh/deneb/gateway-go/internal/domain/contacts"
	"github.com/choiceoh/deneb/gateway-go/pkg/dentime"
	"github.com/choiceoh/deneb/gateway-go/pkg/textutil"
)

// contactsPayload is the wire shape EnrichContacts parses: {"contacts": [...]}.
type contactsPayload struct {
	Contacts []contactdomain.Contact `json:"contacts"`
}

// ContactEnrichResult summarizes one EnrichContacts run.
type ContactEnrichResult struct {
	Total   int      // address-book entries received
	Matched int      // entries whose name matched an existing wiki person
	Updated int      // wiki pages actually changed (idempotent re-syncs excluded)
	Names   []string // titles of enriched people, in page order
}

// EnrichContacts merges a shared address book into EXISTING 사람 (people) wiki
// pages — it never creates a page. For each contact whose name matches a person
// already in the wiki, it writes the phone/email/org into a "## 연락처" section.
//
// The deliberate non-goal is a contacts dump: a phone holds hundreds of numbers,
// almost none of them work-relevant, and flooding the wiki with them would both
// drown the curated pages and pollute the ASR hotword bias (which draws from
// page titles + tags). By enriching only people the user already keeps a wiki
// page for, the address book instead *strengthens* the existing set — the agent
// can answer "whose number is this?" and meeting-prep lookups, and the contact
// detail is searchable — without changing what the wiki is about.
//
// Re-syncing is idempotent: an unchanged "## 연락처" section is left as-is so the
// page's Updated date and the search index don't churn.
func (s *Store) EnrichContacts(contactsJSON []byte) (ContactEnrichResult, error) {
	var payload contactsPayload
	if err := json.Unmarshal(contactsJSON, &payload); err != nil {
		// Tolerate a bare top-level array too ([...] instead of {"contacts":[...]}).
		if err2 := json.Unmarshal(contactsJSON, &payload.Contacts); err2 != nil {
			return ContactEnrichResult{}, fmt.Errorf("wiki: parse contacts: %w", err)
		}
	}
	res := ContactEnrichResult{Total: len(payload.Contacts)}
	if len(payload.Contacts) == 0 {
		return res, nil
	}

	merged := mergeContactsByName(payload.Contacts)
	if len(merged) == 0 {
		return res, nil
	}

	// A missing 인물/ directory (no people yet) or an unreadable tree yields no
	// candidates — nothing to enrich, but the save path already succeeded, so this
	// is not an error for the caller.
	people, _ := s.listPeopleByName()
	if len(people) == 0 {
		return res, nil
	}

	for key, p := range people {
		c, ok := merged[key]
		if !ok {
			continue // no address-book entry for this wiki person
		}
		res.Matched++
		changed, err := s.enrichPersonPage(p.path, c)
		if err != nil {
			// Best-effort per page: one unreadable/unwritable page shouldn't abort
			// the whole sync. The miss simply isn't counted as Updated.
			continue
		}
		if changed {
			res.Updated++
			res.Names = append(res.Names, p.title)
		}
	}
	return res, nil
}

// PeopleEnrichResult summarizes a write-time EnrichPeople run.
type PeopleEnrichResult struct {
	Created []string // person-page titles newly created from the address book
	Updated []string // existing person-page titles whose 연락처 was filled/changed
}

// EnrichPeople is the wiki-WRITE-time counterpart to EnrichContacts (which runs
// on address-book sync). Given display names a freshly written page is about —
// the page's own title when it is an 인물 page, plus any inline [[wiki-link]]
// targets — it fills each matching person's "## 연락처" section from the address
// book so curated pages and the device contacts stay in lockstep without waiting
// for the next sync.
//
// createMissing splits the two triggers the caller wires:
//   - false: only enrich an 인물 page that already exists (e.g. the page just
//     written is itself the person). Never fabricate a page.
//   - true: for an explicit [[link]] to someone in the address book, create a
//     minimal 인물/<name> page when absent. This narrowly relaxes EnrichContacts'
//     "never create" rule — but only for a name the author explicitly linked,
//     which is exactly the work-relevance signal the no-dump doctrine wants.
//
// Best-effort per name: one unreadable/unwritable page never aborts the rest.
func (s *Store) EnrichPeople(names []string, book []contactdomain.Contact, createMissing bool) (PeopleEnrichResult, error) {
	var res PeopleEnrichResult
	if len(names) == 0 || len(book) == 0 {
		return res, nil
	}
	merged := mergeContactsByName(book)
	if len(merged) == 0 {
		return res, nil
	}
	// 인물/ may be absent (nil map) — that's fine, we can still create if allowed.
	people, _ := s.listPeopleByName()
	if people == nil {
		people = map[string]personPage{}
	}

	seen := make(map[string]bool, len(names))
	for _, name := range names {
		key := contactdomain.NormalizePersonName(name)
		if len([]rune(key)) < 2 || seen[key] {
			continue
		}
		seen[key] = true
		c, ok := merged[key]
		if !ok {
			continue // not in the address book — nothing to record
		}
		if p, ok := people[key]; ok {
			if changed, err := s.enrichPersonPage(p.path, c); err == nil && changed {
				res.Updated = append(res.Updated, p.title)
			}
			continue
		}
		if !createMissing {
			continue
		}
		title := strings.TrimSpace(name)
		path, created, changed, err := s.createPersonPage(title, c)
		if err != nil {
			continue
		}
		// Record under the normalized key so a later duplicate link in the same
		// run enriches rather than re-creates.
		people[key] = personPage{path: path, title: title}
		switch {
		case created:
			res.Created = append(res.Created, title)
		case changed:
			res.Updated = append(res.Updated, title)
		}
	}
	return res, nil
}

// personPage is a candidate 인물 page (disk path + display title).
type personPage struct{ path, title string }

// mergeContactsByName collapses address-book entries that share a normalized
// name into one record, uniting all numbers/emails for a person saved under
// several entries. 1-char/empty names are dropped as too ambiguous to match.
func mergeContactsByName(book []contactdomain.Contact) map[string]*contactdomain.Contact {
	byName := make(map[string][]contactdomain.Contact, len(book))
	order := make([]string, 0, len(book))
	for i := range book {
		c := book[i]
		key := contactdomain.NormalizePersonName(c.Name)
		if len([]rune(key)) < 2 {
			continue
		}
		if _, seen := byName[key]; !seen {
			order = append(order, key)
		}
		byName[key] = append(byName[key], c)
	}
	merged := make(map[string]*contactdomain.Contact, len(byName))
	for _, key := range order {
		group := byName[key]
		// Same guard the frontmatter identity backfill uses (identityEmails):
		// entries under one name spanning two employers with no shared phone are
		// 동명이인, not one person. Merging them wrote both companies' phones and
		// addresses into a single 인물 page — 인물/김성환.md ended up carrying
		// topsolar.kr and bmenergy.co.kr side by side, which is how a call gets
		// routed to the wrong person. Keep only the first entry's own details and
		// let the operator split (never auto-split: 2026-07-28 over-merge).
		if _, ambiguous := identityEmails(group); ambiguous {
			cp := group[0]
			cp.Phones = append([]string(nil), group[0].Phones...)
			cp.Emails = append([]string(nil), group[0].Emails...)
			merged[key] = &cp
			continue
		}
		cp := group[0]
		cp.Phones = append([]string(nil), group[0].Phones...)
		cp.Emails = append([]string(nil), group[0].Emails...)
		for _, c := range group[1:] {
			cp.Phones = append(cp.Phones, c.Phones...)
			cp.Emails = append(cp.Emails, c.Emails...)
			if strings.TrimSpace(cp.Org) == "" {
				cp.Org = c.Org
			}
		}
		merged[key] = &cp
	}
	return merged
}

// listPeopleByName indexes the existing 인물 pages by normalized title. Pages are
// listed straight off disk via the 인물/ directory rather than the in-memory
// index, which can be stale or miss the category for older pages (it's rebuilt
// on startup) and silently dropped every candidate; ListPages + ReadPage is
// authoritative. ReadPage/WritePage take the store lock themselves, so callers
// hold no lock while iterating.
func (s *Store) listPeopleByName() (map[string]personPage, error) {
	relPaths, err := s.ListPages("인물")
	if err != nil {
		return nil, err
	}
	people := make(map[string]personPage, len(relPaths))
	for _, path := range relPaths {
		page, err := s.ReadPage(path)
		if err != nil {
			continue // unreadable page — skip, don't abort
		}
		// Defensive: only treat actual people. A stray non-person .md under 인물/
		// shouldn't be matched as a contact.
		if page.Meta.Category != "" && page.Meta.Category != "인물" {
			continue
		}
		title := strings.TrimSpace(page.Meta.Title)
		if title == "" {
			continue
		}
		key := contactdomain.NormalizePersonName(title)
		if len([]rune(key)) < 2 {
			continue
		}
		if _, exists := people[key]; !exists {
			people[key] = personPage{path: path, title: title}
		}
	}
	return people, nil
}

// createPersonPage creates a minimal 인물/<slug> page seeded with the contact's
// "## 연락처" section. Returns the page path, whether it was newly created, and
// whether content changed. A pre-existing page at the slug path is enriched in
// place (created=false) rather than overwritten, so a slug collision never
// clobbers curated content.
func (s *Store) createPersonPage(title string, c *contactdomain.Contact) (path string, created, changed bool, err error) {
	slug := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(title)), " ", "-")
	if slug == "" {
		return "", false, false, fmt.Errorf("wiki: empty person title")
	}
	relPath := "인물/" + slug + ".md"
	// One atomic read-modify-write so a concurrent enrich/write of the same person
	// page can't be clobbered. The existing-page branch enriches in place inline
	// rather than calling enrichPersonPage — that would re-enter UpdatePage and
	// deadlock on the (non-reentrant) writeMu we hold inside this closure.
	err = s.UpdatePage(relPath, func(existing *Page) (*Page, error) {
		if existing != nil {
			section := renderContactSection(c)
			if section == "" {
				return nil, nil // nothing worth writing
			}
			newBody := upsertSection(existing.Body, contactSectionHeading, section)
			if strings.TrimSpace(newBody) == strings.TrimSpace(existing.Body) {
				return nil, nil // identical section → no-op
			}
			existing.Body = newBody
			existing.Meta.Updated = dentime.Now().Format("2006-01-02")
			changed = true
			return existing, nil
		}
		page := NewPage(title, "인물", nil)
		page.Meta.Type = "entity"
		page.Meta.Confidence = "low"
		page.Meta.Importance = personStubImportance
		if org := strings.TrimSpace(c.Org); org != "" {
			page.Meta.Summary = org + " 소속"
		}
		page.Body = renderPersonTemplate(title, c)
		created, changed = true, true
		return page, nil
	})
	if err != nil {
		return "", false, false, err
	}
	return relPath, created, changed, nil
}

// enrichPersonPage writes a contact's phone/email/org into relPath's "## 연락처"
// section. Returns whether the page content actually changed (an identical
// section is a no-op so re-syncing doesn't bump the Updated date).
//
// The read-modify-write runs under UpdatePage so a concurrent writer of the same
// person page can't clobber the enrichment (or vice versa).
func (s *Store) enrichPersonPage(relPath string, c *contactdomain.Contact) (bool, error) {
	section := renderContactSection(c)
	if section == "" {
		return false, nil // nothing worth writing (no phone/email/org)
	}
	changed := false
	err := s.UpdatePage(relPath, func(page *Page) (*Page, error) {
		if page == nil {
			return nil, fmt.Errorf("wiki: enrich person: %s not found", relPath)
		}
		newBody := upsertSection(page.Body, contactSectionHeading, section)
		if strings.TrimSpace(newBody) == strings.TrimSpace(page.Body) {
			return nil, nil // identical section → no-op
		}
		page.Body = newBody
		page.Meta.Updated = dentime.Now().Format("2006-01-02")
		changed = true
		return page, nil
	})
	return changed, err
}

const contactSectionHeading = "연락처"

// The standard 인물 page form. Every person page — an auto-created stub or a
// hand/dream-enriched page — shares these body sections in this order so the
// shape is predictable and the dreamer/agent knows where each fact goes:
//
//	## 소속 · 직책   회사·부서·직급·직책·겸직
//	## 담당 · 관계   담당 업무 + 관련 프로젝트/거래/인물
//	## 연락처        전화·이메일 (contactSectionHeading)
//	## 비고          특징·메모        (added when there's content)
//	## 변경 이력     자동 감사 로그   (added by enrichment)
//
// A stub seeds the first three; 비고/변경 이력 arrive with content. The identity
// email lives in frontmatter (emails:, EnrichPersonEmails), not the body.
const (
	affiliationSectionHeading = "소속 · 직책"
	roleSectionHeading        = "담당 · 관계"
	notesSectionHeading       = "비고"
)

// personStubImportance is the default weight for an auto-created person stub —
// mid-scale, so a stub neither outranks curated pages nor is demoted out of view
// before the dreamer refines it.
const personStubImportance = 0.50

// renderPersonTemplate seeds a new 인물 page body with the standard form (see the
// section constants above). 소속 is filled from the contact's org and 연락처 from
// its numbers; 담당 · 관계 is a placeholder the dreamer / mail analysis fills in.
func renderPersonTemplate(title string, c *contactdomain.Contact) string {
	var b strings.Builder
	b.WriteString("# " + title + "\n\n")

	b.WriteString("## " + affiliationSectionHeading + "\n\n")
	if org := strings.TrimSpace(c.Org); org != "" {
		b.WriteString("- **소속**: " + org + "\n")
	} else {
		b.WriteString("- **소속**: —\n")
	}
	b.WriteString("- **직급 · 직책**: —\n\n")

	// Keep the 담당·관계 section (the standard form is the point) but seed it with a
	// NEUTRAL placeholder, not a business-term sentence. The dreamer/mail analysis
	// fills it. An earlier "_담당 업무·관련 프로젝트…를 채웁니다_" placeholder stamped
	// project/deal vocabulary on every person page and diluted those queries
	// (measured −2 P@1); "_(미기재)_" carries no such tokens (measured no regression).
	b.WriteString("## " + roleSectionHeading + "\n\n_(미기재)_\n\n")

	if section := renderContactSection(c); section != "" {
		b.WriteString("## " + contactSectionHeading + "\n\n" + section + "\n")
	}
	return b.String()
}

// renderContactSection formats a contact's details as the body of the "## 연락처"
// section. Returns "" when there's nothing to record. The provenance line is a
// fixed string (no date) so an unchanged contact renders byte-identically and
// the idempotent re-sync check holds.
func renderContactSection(c *contactdomain.Contact) string {
	phones := textutil.DedupeStrings(c.Phones)
	emails := textutil.DedupeStrings(c.Emails)
	org := strings.TrimSpace(c.Org)

	var b strings.Builder
	if len(phones) > 0 {
		b.WriteString("- 전화: " + strings.Join(phones, ", ") + "\n")
	}
	if len(emails) > 0 {
		b.WriteString("- 이메일: " + strings.Join(emails, ", ") + "\n")
	}
	if org != "" {
		b.WriteString("- 회사: " + org + "\n")
	}
	body := strings.TrimRight(b.String(), "\n")
	if body == "" {
		return ""
	}
	return body + "\n\n_주소록에서 동기화됨_"
}

// upsertSection replaces the body's "## <heading>" section content with
// newContent, or appends the section when it's absent. Other sections keep their
// order and content.
func upsertSection(body, heading, newContent string) string {
	preamble, sections := (&Page{Body: body}).SplitByH2()

	var b strings.Builder
	if strings.TrimSpace(preamble) != "" {
		b.WriteString(strings.TrimRight(preamble, "\n"))
		b.WriteString("\n\n")
	}
	replaced := false
	for _, sec := range sections {
		content := sec.Content
		if strings.EqualFold(strings.TrimSpace(sec.Heading), heading) {
			content = newContent
			replaced = true
		}
		b.WriteString("## " + sec.Heading + "\n\n")
		b.WriteString(strings.TrimRight(content, "\n"))
		b.WriteString("\n\n")
	}
	if !replaced {
		b.WriteString("## " + heading + "\n\n")
		b.WriteString(strings.TrimRight(newContent, "\n"))
		b.WriteString("\n\n")
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

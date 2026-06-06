package wiki

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/pkg/dentime"
)

// Contact is one shared address-book entry from the native client.
type Contact struct {
	Name   string   `json:"name"`
	Phones []string `json:"phones"`
	Emails []string `json:"emails"`
	Org    string   `json:"org"`
}

// contactsPayload is the wire shape EnrichContacts parses: {"contacts": [...]}.
type contactsPayload struct {
	Contacts []Contact `json:"contacts"`
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
func (s *Store) EnrichPeople(names []string, book []Contact, createMissing bool) (PeopleEnrichResult, error) {
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
		key := normalizePersonName(name)
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
func mergeContactsByName(book []Contact) map[string]*Contact {
	merged := make(map[string]*Contact, len(book))
	for i := range book {
		c := book[i]
		key := normalizePersonName(c.Name)
		if len([]rune(key)) < 2 {
			continue
		}
		if m, ok := merged[key]; ok {
			m.Phones = append(m.Phones, c.Phones...)
			m.Emails = append(m.Emails, c.Emails...)
			if strings.TrimSpace(m.Org) == "" {
				m.Org = c.Org
			}
			continue
		}
		cp := c
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
		key := normalizePersonName(title)
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
func (s *Store) createPersonPage(title string, c *Contact) (path string, created, changed bool, err error) {
	slug := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(title)), " ", "-")
	if slug == "" {
		return "", false, false, fmt.Errorf("wiki: empty person title")
	}
	relPath := "인물/" + slug + ".md"
	if existing, _ := s.ReadPage(relPath); existing != nil {
		ch, err := s.enrichPersonPage(relPath, c)
		return relPath, false, ch, err
	}
	page := NewPage(title, "인물", nil)
	body := fmt.Sprintf("# %s\n\n## 요약\n\n_위키 링크에서 자동 생성됨_\n", title)
	if section := renderContactSection(c); section != "" {
		body += "\n## " + contactSectionHeading + "\n\n" + section + "\n"
	}
	page.Body = body
	if err := s.WritePage(relPath, page); err != nil {
		return "", false, false, err
	}
	return relPath, true, true, nil
}

// enrichPersonPage writes a contact's phone/email/org into relPath's "## 연락처"
// section. Returns whether the page content actually changed (an identical
// section is a no-op so re-syncing doesn't bump the Updated date).
func (s *Store) enrichPersonPage(relPath string, c *Contact) (bool, error) {
	page, err := s.ReadPage(relPath)
	if err != nil {
		return false, err
	}
	section := renderContactSection(c)
	if section == "" {
		return false, nil // nothing worth writing (no phone/email/org)
	}
	newBody := upsertSection(page.Body, contactSectionHeading, section)
	if strings.TrimSpace(newBody) == strings.TrimSpace(page.Body) {
		return false, nil
	}
	page.Body = newBody
	page.Meta.Updated = dentime.Now().Format("2006-01-02")
	if err := s.WritePage(relPath, page); err != nil {
		return false, err
	}
	return true, nil
}

const contactSectionHeading = "연락처"

// renderContactSection formats a contact's details as the body of the "## 연락처"
// section. Returns "" when there's nothing to record. The provenance line is a
// fixed string (no date) so an unchanged contact renders byte-identically and
// the idempotent re-sync check holds.
func renderContactSection(c *Contact) string {
	phones := dedupeStrings(c.Phones)
	emails := dedupeStrings(c.Emails)
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

// personTitleSuffixes are Korean honorific/role tokens stripped from the tail of
// a name when matching an address-book entry to a wiki person ("김민준 부장" and
// "김민준대표님" both normalize to "김민준"). Ordered longest-first so a compound
// title ("대표이사") is removed whole before its parts.
var personTitleSuffixes = []string{
	"대표이사",
	"부사장", "본부장", "부회장",
	"부장", "차장", "과장", "대리", "사원", "주임", "팀장", "실장",
	"이사", "상무", "전무", "사장", "회장", "선임", "책임", "수석", "대표",
	"님", "씨", "군", "양",
}

// normalizePersonName reduces a display name to a stable match key: it drops any
// parenthetical/affiliation suffix, removes whitespace, peels trailing honorific
// tokens (while never shrinking below 2 runes, so "김부장" doesn't collapse to a
// bare surname), and lowercases ASCII. Matching is exact on this key — no
// substring matching, which would mis-pair "이수" with "이수민".
func normalizePersonName(s string) string {
	t := strings.TrimSpace(s)
	if t == "" {
		return ""
	}
	// Cut at the first affiliation/role separator: "김민준(탑솔라)" -> "김민준".
	for _, sep := range []string{"(", "（", "[", "<", "/", ",", "·"} {
		if i := strings.Index(t, sep); i >= 0 {
			t = t[:i]
		}
	}
	t = strings.ReplaceAll(t, " ", "")
	t = strings.TrimSpace(t)
	// Peel trailing honorific/role tokens, keeping at least 2 runes.
	for {
		stripped := false
		for _, suf := range personTitleSuffixes {
			if !strings.HasSuffix(t, suf) {
				continue
			}
			cand := strings.TrimSuffix(t, suf)
			if len([]rune(cand)) < 2 {
				continue
			}
			t = cand
			stripped = true
			break
		}
		if !stripped {
			break
		}
	}
	return strings.ToLower(t)
}

// dedupeStrings trims, drops blanks, and removes duplicates while preserving
// first-seen order.
func dedupeStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

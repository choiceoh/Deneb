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

	// Merge address-book entries that share a normalized name into one record, so
	// a person saved under several entries gets all their numbers/emails united.
	merged := make(map[string]*Contact, len(payload.Contacts))
	for i := range payload.Contacts {
		c := payload.Contacts[i]
		key := normalizePersonName(c.Name)
		if len([]rune(key)) < 2 {
			continue // 1-char / empty names are too ambiguous to match a person
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
	if len(merged) == 0 {
		return res, nil
	}

	// Candidate people: the 사람 category pages. Snapshot (path, title) under the
	// read lock, then release it before enriching — WritePage takes the write lock
	// itself, so holding RLock across the loop would deadlock.
	type person struct{ path, title string }
	s.mu.RLock()
	people := make([]person, 0, len(s.index.Entries))
	for path, e := range s.index.Entries {
		if e.Category == "사람" {
			people = append(people, person{path: path, title: e.Title})
		}
	}
	s.mu.RUnlock()

	for _, p := range people {
		pk := normalizePersonName(p.title)
		if len([]rune(pk)) < 2 {
			continue
		}
		c, ok := merged[pk]
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

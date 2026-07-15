package wiki

import (
	"fmt"
	"strings"

	contactdomain "github.com/choiceoh/deneb/gateway-go/internal/domain/contacts"
	"github.com/choiceoh/deneb/gateway-go/pkg/dentime"
)

// GroupwarePersonCard is the safe Amaranth HR card emitted as DENEB_PEOPLE_JSON
// (no resident-id / address). Used to enrich or create 인물 wiki pages.
type GroupwarePersonCard struct {
	EmpCd     string `json:"empCd"`
	Name      string `json:"name"`
	Dept      string `json:"dept"`
	Div       string `json:"div"`
	Title     string `json:"title"`
	Honorific string `json:"honorific"`
	Mobile    string `json:"mobile"`
	OfficeTel string `json:"officeTel"`
	Birth     string `json:"birth"`
	Status    string `json:"status"`
}

// GroupwarePersonLink summarizes one card's wiki linkage for the agent reply.
type GroupwarePersonLink struct {
	Name   string // display name
	Path   string // 인물/….md (empty when skipped)
	Action string // created | updated | unchanged | skipped
}

// GroupwarePeopleEnrichResult is the EnrichGroupwarePeople return value.
type GroupwarePeopleEnrichResult struct {
	Links []GroupwarePersonLink
}

// EnrichGroupwarePeople upserts ## 소속 · 직책 / ## 연락처 / ## 비고 on matching
// 인물 pages, or creates a stub when none exists. Only the cards passed in are
// touched (caller must already limit detail fetches). Best-effort per card.
func (s *Store) EnrichGroupwarePeople(cards []GroupwarePersonCard) (GroupwarePeopleEnrichResult, error) {
	var res GroupwarePeopleEnrichResult
	if s == nil || len(cards) == 0 {
		return res, nil
	}
	people, _ := s.listPeopleByName()
	if people == nil {
		people = map[string]personPage{}
	}
	seen := map[string]bool{}
	for _, card := range cards {
		name := strings.TrimSpace(card.Name)
		key := contactdomain.NormalizePersonName(name)
		if len([]rune(key)) < 2 || seen[key] {
			res.Links = append(res.Links, GroupwarePersonLink{Name: name, Action: "skipped"})
			continue
		}
		seen[key] = true
		if p, ok := people[key]; ok {
			changed, err := s.enrichPersonFromGroupware(p.path, card)
			if err != nil {
				res.Links = append(res.Links, GroupwarePersonLink{Name: p.title, Path: p.path, Action: "skipped"})
				continue
			}
			action := "unchanged"
			if changed {
				action = "updated"
			}
			res.Links = append(res.Links, GroupwarePersonLink{Name: p.title, Path: p.path, Action: action})
			continue
		}
		path, created, changed, err := s.createPersonFromGroupware(name, card)
		if err != nil {
			res.Links = append(res.Links, GroupwarePersonLink{Name: name, Action: "skipped"})
			continue
		}
		people[key] = personPage{path: path, title: name}
		action := "unchanged"
		switch {
		case created:
			action = "created"
		case changed:
			action = "updated"
		}
		res.Links = append(res.Links, GroupwarePersonLink{Name: name, Path: path, Action: action})
	}
	return res, nil
}

func (s *Store) enrichPersonFromGroupware(relPath string, card GroupwarePersonCard) (bool, error) {
	changed := false
	err := s.UpdatePage(relPath, func(page *Page) (*Page, error) {
		if page == nil {
			return nil, fmt.Errorf("wiki: enrich groupware person: %s not found", relPath)
		}
		body := applyGroupwareSections(page.Body, card)
		if strings.TrimSpace(body) == strings.TrimSpace(page.Body) {
			return nil, nil
		}
		page.Body = body
		page.Meta.Updated = dentime.Now().Format("2006-01-02")
		if sum := groupwareOrgLine(card); sum != "" && strings.TrimSpace(page.Meta.Summary) == "" {
			page.Meta.Summary = sum + " 소속"
		}
		changed = true
		return page, nil
	})
	return changed, err
}

func (s *Store) createPersonFromGroupware(title string, card GroupwarePersonCard) (path string, created, changed bool, err error) {
	c := groupwareCardToContact(card)
	// Prefer Hangul-friendly slug via personSlug when available (Korean names).
	slug := personSlug(title)
	if slug == "" {
		slug = strings.ReplaceAll(strings.ToLower(strings.TrimSpace(title)), " ", "-")
	}
	if slug == "" {
		return "", false, false, fmt.Errorf("wiki: empty person title")
	}
	relPath := "인물/" + slug + ".md"
	err = s.UpdatePage(relPath, func(existing *Page) (*Page, error) {
		if existing != nil {
			body := applyGroupwareSections(existing.Body, card)
			if strings.TrimSpace(body) == strings.TrimSpace(existing.Body) {
				return nil, nil
			}
			existing.Body = body
			existing.Meta.Updated = dentime.Now().Format("2006-01-02")
			changed = true
			return existing, nil
		}
		page := NewPage(title, "인물", nil)
		page.Meta.Type = "entity"
		page.Meta.Confidence = "low"
		page.Meta.Importance = personStubImportance
		if sum := groupwareOrgLine(card); sum != "" {
			page.Meta.Summary = sum + " 소속"
		}
		page.Body = renderPersonTemplate(title, c)
		page.Body = applyGroupwareSections(page.Body, card)
		created, changed = true, true
		return page, nil
	})
	if err != nil {
		return "", false, false, err
	}
	return relPath, created, changed, nil
}

func groupwareCardToContact(card GroupwarePersonCard) *contactdomain.Contact {
	phones := make([]string, 0, 2)
	if m := strings.TrimSpace(card.Mobile); m != "" {
		phones = append(phones, m)
	}
	if o := strings.TrimSpace(card.OfficeTel); o != "" {
		phones = append(phones, o)
	}
	return &contactdomain.Contact{
		Name:   strings.TrimSpace(card.Name),
		Phones: phones,
		Org:    groupwareOrgLine(card),
	}
}

func groupwareOrgLine(card GroupwarePersonCard) string {
	div := strings.TrimSpace(card.Div)
	dept := strings.TrimSpace(card.Dept)
	switch {
	case div != "" && dept != "" && div != dept:
		return div + " · " + dept
	case dept != "":
		return dept
	case div != "":
		return div
	default:
		return ""
	}
}

func groupwareTitle(card GroupwarePersonCard) string {
	if t := strings.TrimSpace(card.Title); t != "" {
		return t
	}
	return strings.TrimSpace(card.Honorific)
}

func renderGroupwareAffiliation(card GroupwarePersonCard) string {
	org := groupwareOrgLine(card)
	title := groupwareTitle(card)
	var b strings.Builder
	if org != "" {
		b.WriteString("- **소속**: " + org + "\n")
	} else {
		b.WriteString("- **소속**: —\n")
	}
	if title != "" {
		b.WriteString("- **직급 · 직책**: " + title + "\n")
	} else {
		b.WriteString("- **직급 · 직책**: —\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderGroupwareContact(card GroupwarePersonCard) string {
	c := groupwareCardToContact(card)
	section := renderContactSection(c)
	if section == "" {
		return ""
	}
	// Swap address-book provenance for Amaranth.
	section = strings.Replace(section, "_주소록에서 동기화됨_", "_아마란스 인사정보_", 1)
	if !strings.Contains(section, "_아마란스 인사정보_") {
		section = strings.TrimRight(section, "\n") + "\n\n_아마란스 인사정보_"
	}
	return section
}

func renderGroupwareNotes(card GroupwarePersonCard) string {
	var b strings.Builder
	if birth := strings.TrimSpace(card.Birth); birth != "" {
		b.WriteString("- 생년월일: " + birth + "\n")
	}
	if emp := strings.TrimSpace(card.EmpCd); emp != "" {
		b.WriteString("- 사원코드: " + emp + "\n")
	}
	if st := strings.TrimSpace(card.Status); st != "" {
		b.WriteString("- 재직상태: " + st + "\n")
	}
	body := strings.TrimRight(b.String(), "\n")
	if body == "" {
		return ""
	}
	return body + "\n\n_아마란스 인사정보_"
}

func applyGroupwareSections(body string, card GroupwarePersonCard) string {
	body = upsertSection(body, affiliationSectionHeading, renderGroupwareAffiliation(card))
	if contact := renderGroupwareContact(card); contact != "" {
		body = upsertSection(body, contactSectionHeading, contact)
	}
	if notes := renderGroupwareNotes(card); notes != "" {
		body = upsertSection(body, notesSectionHeading, notes)
	}
	return body
}

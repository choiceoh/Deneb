// person_seed.go — mention-driven 인물 page creation.
//
// The contacts mirror holds thousands of people; the wiki 인물 category holds
// a handful. The gap matters: sender-context cards, ASR hotwords, and the
// knowledge graph all key off wiki person pages. Asking the synthesis LLM to
// notice "this person keeps coming up" is unreliable — so each dream cycle
// does it deterministically: contacts mentioned repeatedly in the cycle's
// input get a stub 인물 page seeded from the address book, which later cycles
// then enrich like any other page.
package wiki

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// PersonSeed is the address-book projection the dreamer needs. Wired from the
// contacts store by the gateway; the wiki package stays decoupled from it.
type PersonSeed struct {
	Name   string
	Org    string
	Phones []string
	Emails []string
}

const (
	// personSeedMinMentions: a single mention is noise; repetition is signal.
	personSeedMinMentions = 2
	// personSeedMinNameRunes guards against false positives — 2-rune Korean
	// given names ("민준") collide with ordinary words far too often.
	personSeedMinNameRunes = 3
	// personSeedMaxPerCycle bounds page creation per dream cycle.
	personSeedMaxPerCycle = 3
)

// SetPersonDirectory wires the address-book snapshot provider. nil disables
// person seeding.
func (wd *WikiDreamer) SetPersonDirectory(fn func() []PersonSeed) {
	wd.personDirectory = fn
}

// seedPersonPages creates stub 인물 pages for contacts mentioned at least
// personSeedMinMentions times in the cycle input and absent from the wiki.
// Returns how many pages were created.
func (wd *WikiDreamer) seedPersonPages(_ context.Context, input string) int {
	if wd.personDirectory == nil || strings.TrimSpace(input) == "" {
		return 0
	}
	people := wd.personDirectory()
	if len(people) == 0 {
		return 0
	}

	idx := wd.store.Index()
	existingTitle := make(map[string]bool, len(idx.Entries))
	for _, e := range idx.Entries {
		existingTitle[strings.TrimSpace(e.Title)] = true
	}

	created := 0
	for _, p := range people {
		if created >= personSeedMaxPerCycle {
			break
		}
		name := strings.TrimSpace(p.Name)
		if utf8.RuneCountInString(name) < personSeedMinNameRunes {
			continue
		}
		if strings.Count(input, name) < personSeedMinMentions {
			continue
		}
		// Already in the wiki (by exact title, or title with org suffix etc.)?
		if existingTitle[name] || wd.personPageExists(name) {
			continue
		}

		page := NewPage(name, "인물", nil)
		page.Meta.ID = personSlug(name)
		page.Meta.Type = "entity"
		page.Meta.Confidence = "medium"
		page.Meta.Importance = 0.5
		if p.Org != "" {
			page.Meta.Summary = p.Org + " — 주소록 기반 자동 생성"
		} else {
			page.Meta.Summary = "주소록 기반 자동 생성"
		}
		var sb strings.Builder
		fmt.Fprintf(&sb, "# %s\n\n## 연락처\n", name)
		if p.Org != "" {
			fmt.Fprintf(&sb, "- 소속: %s\n", p.Org)
		}
		for _, ph := range p.Phones {
			fmt.Fprintf(&sb, "- 전화: %s\n", ph)
		}
		for _, em := range p.Emails {
			fmt.Fprintf(&sb, "- 이메일: %s\n", em)
		}
		fmt.Fprintf(&sb, "\n## 변경 이력\n- %s: 드림 사이클 반복 언급으로 자동 생성 (주소록 연동)\n",
			time.Now().Format("2006-01-02"))
		page.Body = sb.String()

		relPath := "인물/" + personSlug(name) + ".md"
		if err := wd.store.WritePage(relPath, page); err != nil {
			wd.logger.Warn("wiki-dream: person seed failed", "name", name, "error", err)
			continue
		}
		wd.logger.Info("wiki-dream: person page seeded", "name", name, "path", relPath)
		created++
	}
	return created
}

// personPageExists reports whether a 인물 page already covers this name
// (title search catches "<name> 부장" style titles the exact map misses).
func (wd *WikiDreamer) personPageExists(name string) bool {
	for _, e := range wd.store.Index().Entries {
		if e.Category == "인물" && strings.Contains(e.Title, name) {
			return true
		}
	}
	return false
}

// personSlug builds a stable file slug from a (Korean) name.
func personSlug(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r == ' ' || r == '\t':
			b.WriteRune('-')
		default:
			b.WriteRune(r)
		}
	}
	return strings.ToLower(b.String())
}

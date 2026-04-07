// Knowledge enrichment: tier-1 wiki auto-injection for system prompt.
package knowledge

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/wiki"
)

// Tier-1 auto-injection limits.
const (
	tier1MaxPages     = 10    // max pages to inject
	tier1MaxBodyRunes = 1000  // truncate each page body
	tier1MaxTotalChar = 20000 // total budget for tier-1 section
)

// FormatTier1 builds a "## 핵심 지식" section from high-importance wiki pages.
// Returns "" if no tier-1 pages exist.
func FormatTier1(store *wiki.Store, minImportance float64) string {
	if store == nil {
		return ""
	}

	pages := store.Tier1Pages(minImportance)
	if len(pages) == 0 {
		return ""
	}
	if len(pages) > tier1MaxPages {
		pages = pages[:tier1MaxPages]
	}

	var sb strings.Builder
	sb.WriteString("## 핵심 지식 (자동 주입)\n\n")

	for _, r := range pages {
		body := truncateRunes(r.Page.Body, tier1MaxBodyRunes)
		header := fmt.Sprintf("### %s (%s)\n", r.Page.Meta.Title, r.Path)
		if r.Page.Meta.Summary != "" {
			header += fmt.Sprintf("_%s_\n", r.Page.Meta.Summary)
		}
		entry := header + body + "\n\n"

		if sb.Len()+len(entry) > tier1MaxTotalChar {
			break
		}
		sb.WriteString(entry)
	}

	return sb.String()
}

// truncateRunes truncates s to at most maxRunes runes, appending "..." if truncated.
func truncateRunes(s string, maxRunes int) string {
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxRunes]) + "..."
}

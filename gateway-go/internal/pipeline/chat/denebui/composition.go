package denebui

import "strings"

// Composition advisories: soft signals that a schema-VALID card ignored the
// authoring contract's composition conventions (system prompt "조형 관례" —
// lead conclusion, per-topic card splits, no prose dumps). Logged by
// reportDenebUICardHealth, never gated: the heuristics flag only gross misses
// so the journal shows whether the contract actually shapes production cards
// (the observable half of evolving the prompt further).
const (
	// AdvisoryProseDump — a card that is essentially paragraphs: several plain
	// text nodes and not a single structural node (stat/table/list/chart/…).
	AdvisoryProseDump = "prose_dump"
	// AdvisoryNoSectionHeader — a multi-card answer where no card opens with
	// the icon+caption section-header row the contract teaches.
	AdvisoryNoSectionHeader = "no_section_header"
)

// proseDumpThreshold is the number of plain text children (style body/unset)
// in one card that reads as a prose dump when no structural sibling exists.
const proseDumpThreshold = 4

// CompositionAdvisories parses an HTML-format fence body and returns the
// composition advisories it triggers (nil when clean, unparseable, or legacy
// JSON — the contract targets HTML authoring only).
func CompositionAdvisories(body string) []string {
	body = strings.TrimSpace(body)
	if !IsHTMLBody(body) {
		return nil
	}
	root, _ := ParseHTML(body)
	if root == nil {
		return nil
	}
	cards := collectCards(root, nil)
	if len(cards) == 0 {
		return nil
	}
	var advisories []string
	for _, c := range cards {
		if cardIsProseDump(c) {
			advisories = append(advisories, AdvisoryProseDump)
			break
		}
	}
	if len(cards) >= 2 {
		headers := 0
		for _, c := range cards {
			if cardHasSectionHeader(c) {
				headers++
			}
		}
		if headers == 0 {
			advisories = append(advisories, AdvisoryNoSectionHeader)
		}
	}
	return advisories
}

func collectCards(v any, into []map[string]any) []map[string]any {
	switch n := v.(type) {
	case []any:
		for _, e := range n {
			into = collectCards(e, into)
		}
	case map[string]any:
		if n["type"] == "card" {
			into = append(into, n)
		}
		into = collectCards(n["children"], into)
		if tabs, ok := n["tabs"].([]any); ok {
			for _, t := range tabs {
				if tm, ok := t.(map[string]any); ok {
					into = collectCards(tm["children"], into)
				}
			}
		}
	}
	return into
}

// cardIsProseDump: many plain text children, zero structural children.
func cardIsProseDump(card map[string]any) bool {
	children, _ := card["children"].([]any)
	plainText := 0
	for _, c := range children {
		m, ok := c.(map[string]any)
		if !ok {
			continue
		}
		switch m["type"] {
		case "text":
			style, _ := m["style"].(string)
			if style == "" || style == "body" {
				plainText++
			}
		case "stat", "table", "list", "chart", "progress", "alert", "badge",
			"row", "markdown", "quote", "image", "code", "accordion", "tabs":
			return false
		}
	}
	return plainText >= proseDumpThreshold
}

// cardHasSectionHeader: first child is the contract's icon+caption row.
func cardHasSectionHeader(card map[string]any) bool {
	children, _ := card["children"].([]any)
	if len(children) == 0 {
		return false
	}
	row, ok := children[0].(map[string]any)
	if !ok || row["type"] != "row" {
		return false
	}
	rowKids, _ := row["children"].([]any)
	if len(rowKids) != 2 {
		return false
	}
	icon, ok := rowKids[0].(map[string]any)
	if !ok || icon["type"] != "icon" {
		return false
	}
	text, ok := rowKids[1].(map[string]any)
	if !ok || text["type"] != "text" {
		return false
	}
	style, _ := text["style"].(string)
	return style == "caption"
}

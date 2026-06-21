package org

import "strings"

// nodeTypeKR maps a node type to its Korean label, mirroring the native app's
// 그룹/회사/본부·실/팀 labels so the text the assistant reads matches what the
// operator sees on screen.
func nodeTypeKR(t string) string {
	switch t {
	case NodeTypeGroup:
		return "그룹"
	case NodeTypeCompany:
		return "회사"
	case NodeTypeDivision:
		return "본부/실"
	case NodeTypeTeam:
		return "팀"
	default:
		return ""
	}
}

// RenderText renders the chart as an indented tree (그룹 → 회사 → 본부/실 → 팀 → 사람),
// the same hierarchy the operator maintains in the native app's org-chart screen,
// so the assistant references the exact org the user sees. Members show their
// 직급/직책 in parentheses. Deterministic (input/slice order, no map iteration) so
// the rendered text is byte-stable across turns when org.json is unchanged — the
// prefix cache holds. An empty tree renders "".
func (t OrgTree) RenderText() string {
	if len(t.Nodes) == 0 {
		return ""
	}
	var b strings.Builder
	var walk func(nodes []OrgNode, depth int)
	walk = func(nodes []OrgNode, depth int) {
		for _, n := range nodes {
			indent := strings.Repeat("  ", depth)
			b.WriteString(indent + "- " + strings.TrimSpace(n.Name))
			if lbl := nodeTypeKR(n.Type); lbl != "" {
				b.WriteString(" (" + lbl + ")")
			}
			b.WriteByte('\n')
			for _, m := range n.Members {
				meta := make([]string, 0, 2)
				if r := strings.TrimSpace(m.Rank); r != "" {
					meta = append(meta, r)
				}
				if p := strings.TrimSpace(m.Position); p != "" {
					meta = append(meta, p)
				}
				b.WriteString(indent + "  · " + strings.TrimSpace(m.Name))
				if len(meta) > 0 {
					b.WriteString(" (" + strings.Join(meta, "·") + ")")
				}
				b.WriteByte('\n')
			}
			walk(t.Children(n.ID), depth+1)
		}
	}
	walk(t.Roots(), 0)
	return b.String()
}

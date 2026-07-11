// recall_org.go — the org chart (조직도) as a recall source. The chart holds who
// sits in which 계열사/실/팀; when a turn names a person or a division, the agent
// should get that structure alongside the wiki — and, for a named person, a
// straight link into their 인물 knowledge page. This closes the gap where the org
// chart lived only behind its own `org` tool, invisible to unified recall.
//
// Matching is name-in-message (the anchor style the wiki source uses for
// projects/counterparties), not substring-of-query: recall queries are phrases,
// but the entities we link on are short proper names, so we look for the member
// or node name appearing in the raw message. Rows are bounded by recallOrgQuota
// so a broad message ("팀 전체") cannot let the org source crowd out wiki/diary.

package recall

import (
	"context"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/org"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

// recallOrgQuota caps org rows per turn (mirrors recallFileQuota: a source with
// its own score band must not monopolize the merged tail-injection window).
const recallOrgQuota = 3

// recallOrgSourcePrior anchors org rows just under the wiki prior (0.80): org
// context is curated and high-trust, but a person's own wiki page — when the
// same turn also matched it — should edge out the bare org row.
const recallOrgSourcePrior = 0.79

// recallOrgEvidence surfaces org-chart context for people/divisions named in the
// message. For each matched member it links to their 인물 page (resolved via the
// wiki store) and notes their 부서 path + 직급/직책; for a matched division/company
// node it notes the node's members. load is org.Load (nil disables the source);
// store resolves member names to wiki paths (nil skips the person link but still
// surfaces org context). Graceful-empty on any error, like every recall source.
func recallOrgEvidence(ctx context.Context, load func() (org.OrgTree, error), store *wiki.Store, message string) []recallEvidence {
	if load == nil {
		return nil
	}
	msg := strings.TrimSpace(message)
	if len([]rune(msg)) < 2 {
		return nil
	}
	tree, err := load()
	if err != nil || len(tree.Nodes) == 0 {
		return nil
	}

	byID := make(map[string]org.OrgNode, len(tree.Nodes))
	for _, n := range tree.Nodes {
		byID[n.ID] = n
	}
	// deptPath walks ParentID to the root, joining node names with " · " so a row
	// reads "탑솔라 · 기획조정실". Bounded by node count to defend against a cyclic
	// chart (Validate rejects cycles on save, but recall must never hang on a
	// hand-edited file that slipped through).
	deptPath := func(n org.OrgNode) string {
		parts := []string{n.Name}
		seen := map[string]bool{n.ID: true}
		cur := n
		for i := 0; i < len(tree.Nodes); i++ {
			p, ok := byID[cur.ParentID]
			if !ok || seen[p.ID] {
				break
			}
			parts = append([]string{p.Name}, parts...)
			seen[p.ID] = true
			cur = p
		}
		return strings.Join(parts, " · ")
	}

	type memberHit struct {
		name string
		dept string
		note string
	}
	var members []memberHit
	var nodes []org.OrgNode
	seenMember := map[string]bool{}
	seenNode := map[string]bool{}

	for _, n := range tree.Nodes {
		if ctx.Err() != nil {
			break
		}
		// Node named in the message → surface its members. Guard 2-rune node names
		// (e.g. a bare "실") from false-matching common text.
		if name := strings.TrimSpace(n.Name); len([]rune(name)) >= 2 && strings.Contains(msg, name) {
			if !seenNode[n.ID] {
				seenNode[n.ID] = true
				nodes = append(nodes, n)
			}
		}
		for _, m := range n.Members {
			name := strings.TrimSpace(m.Name)
			// Proper names are ≥2 runes; requiring 3 for the containment test keeps a
			// common 2-char string from matching mid-word. Most Korean full names are
			// 3 runes, so this loses almost nothing while cutting false positives.
			if len([]rune(name)) < 3 || !strings.Contains(msg, name) {
				continue
			}
			if seenMember[name] {
				continue
			}
			seenMember[name] = true
			members = append(members, memberHit{name: name, dept: deptPath(n), note: memberNote(m)})
		}
	}

	if len(members) == 0 && len(nodes) == 0 {
		return nil
	}

	// Resolve every matched member's 인물 page in one batch (one disk scan).
	var names []string
	for _, mh := range members {
		names = append(names, mh.name)
	}
	var personPaths map[string]string
	if store != nil {
		personPaths = store.ResolvePersonPaths(names)
	}

	var evidence []recallEvidence
	for _, mh := range members {
		if len(evidence) >= recallOrgQuota {
			break
		}
		src := personPaths[mh.name]
		if src == "" {
			src = "조직도: " + mh.name
		}
		note := mh.dept
		if mh.note != "" {
			note += " · " + mh.note
		}
		evidence = append(evidence, recallEvidence{
			Kind:   "org",
			Source: src,
			Note:   note,
			Score:  recallOrgSourcePrior,
		})
	}
	for _, n := range nodes {
		if len(evidence) >= recallOrgQuota {
			break
		}
		names := make([]string, 0, len(n.Members))
		for _, m := range n.Members {
			if t := memberTitleShort(m); t != "" {
				names = append(names, m.Name+t)
			} else {
				names = append(names, m.Name)
			}
		}
		note := "구성원 " + fmt.Sprint(len(n.Members)) + "명"
		if len(names) > 0 {
			note += ": " + strings.Join(names, ", ")
		}
		evidence = append(evidence, recallEvidence{
			Kind:   "org",
			Source: "조직도: " + deptPath(n),
			Note:   note,
			Score:  recallOrgSourcePrior - 0.01, // a division row sits just below a person row
		})
	}
	return evidence
}

// memberNote renders a member's 직급/직책 for a recall row: "전무 · 기획조정실장",
// dropping empty parts. Empty when the member has neither.
func memberNote(m org.Member) string {
	var parts []string
	if r := strings.TrimSpace(m.Rank); r != "" {
		parts = append(parts, r)
	}
	if p := strings.TrimSpace(m.Position); p != "" {
		parts = append(parts, p)
	}
	return strings.Join(parts, " · ")
}

// memberTitleShort renders a compact parenthetical title for a division roster,
// e.g. " (전무)" — position preferred, else rank. Empty when the member has
// neither.
func memberTitleShort(m org.Member) string {
	if p := strings.TrimSpace(m.Position); p != "" {
		return " (" + p + ")"
	}
	if r := strings.TrimSpace(m.Rank); r != "" {
		return " (" + r + ")"
	}
	return ""
}

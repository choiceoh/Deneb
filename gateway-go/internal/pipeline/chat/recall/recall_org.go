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
	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
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
	query, ok := prepareOrgRecallQuery(load, message)
	if !ok {
		return nil
	}
	matches := matchOrgRecallEntities(ctx, query)
	if matches.empty() {
		return nil
	}
	candidates := scoreOrgRecallMatches(matches)
	personPaths := resolveOrgPersonPaths(store, matches.members)
	return outputOrgRecallEvidence(candidates, personPaths)
}

// orgRecallQuery is the source snapshot paired with the normalized message
// that selected it. Source loading happens once before entity matching starts.
type orgRecallQuery struct {
	message string
	tree    org.OrgTree
}

func prepareOrgRecallQuery(load func() (org.OrgTree, error), message string) (orgRecallQuery, bool) {
	if load == nil {
		return orgRecallQuery{}, false
	}
	message = strings.TrimSpace(message)
	if len([]rune(message)) < 2 {
		return orgRecallQuery{}, false
	}
	tree, err := load()
	if err != nil || len(tree.Nodes) == 0 {
		return orgRecallQuery{}, false
	}
	return orgRecallQuery{message: message, tree: tree}, true
}

type orgTreeIndex struct {
	byID     map[string]org.OrgNode
	maxDepth int
}

func newOrgTreeIndex(tree org.OrgTree) orgTreeIndex {
	byID := make(map[string]org.OrgNode, len(tree.Nodes))
	for _, node := range tree.Nodes {
		byID[node.ID] = node
	}
	return orgTreeIndex{byID: byID, maxDepth: len(tree.Nodes)}
}

// departmentPath walks ParentID to the root, joining node names with " · ".
// The node-count bound prevents a hand-edited cyclic chart from hanging recall.
func (index orgTreeIndex) departmentPath(node org.OrgNode) string {
	parts := []string{node.Name}
	seen := map[string]bool{node.ID: true}
	current := node
	for i := 0; i < index.maxDepth; i++ {
		parent, ok := index.byID[current.ParentID]
		if !ok || seen[parent.ID] {
			break
		}
		parts = append([]string{parent.Name}, parts...)
		seen[parent.ID] = true
		current = parent
	}
	return strings.Join(parts, " · ")
}

type orgMemberMatch struct {
	name       string
	department string
	role       string
}

type orgNodeMatch struct {
	node       org.OrgNode
	department string
}

type orgRecallMatches struct {
	members []orgMemberMatch
	nodes   []orgNodeMatch
}

func (matches orgRecallMatches) empty() bool {
	return len(matches.members) == 0 && len(matches.nodes) == 0
}

func matchOrgRecallEntities(ctx context.Context, query orgRecallQuery) orgRecallMatches {
	index := newOrgTreeIndex(query.tree)
	matches := orgRecallMatches{}
	seenMember := map[string]bool{}
	seenNode := map[string]bool{}

	for _, node := range query.tree.Nodes {
		if ctx.Err() != nil {
			break
		}
		if matchesOrgNode(query.message, node.Name) && !seenNode[node.ID] {
			seenNode[node.ID] = true
			matches.nodes = append(matches.nodes, orgNodeMatch{
				node:       node,
				department: index.departmentPath(node),
			})
		}
		for _, member := range node.Members {
			name, matched := matchOrgMemberName(query.message, member.Name)
			if !matched || seenMember[name] {
				continue
			}
			seenMember[name] = true
			matches.members = append(matches.members, orgMemberMatch{
				name:       name,
				department: index.departmentPath(node),
				role:       memberNote(member),
			})
		}
	}
	return matches
}

func matchesOrgNode(message, rawName string) bool {
	name := strings.TrimSpace(rawName)
	return len([]rune(name)) >= 2 && strings.Contains(message, name)
}

func matchOrgMemberName(message, rawName string) (string, bool) {
	name := strings.TrimSpace(rawName)
	// Requiring 3 runes keeps common 2-rune strings from matching mid-word.
	if len([]rune(name)) < 3 || !strings.Contains(message, name) {
		return "", false
	}
	return name, true
}

func resolveOrgPersonPaths(store *wiki.Store, members []orgMemberMatch) map[string]string {
	if store == nil || len(members) == 0 {
		return nil
	}
	names := make([]string, 0, len(members))
	for _, member := range members {
		names = append(names, member.name)
	}
	return store.ResolvePersonPaths(names)
}

type orgRecallCandidateKind uint8

const (
	orgRecallMemberCandidate orgRecallCandidateKind = iota
	orgRecallNodeCandidate
)

type orgRecallCandidate struct {
	kind   orgRecallCandidateKind
	score  float64
	member orgMemberMatch
	node   orgNodeMatch
}

// scoreOrgRecallMatches establishes source-local rank: members retain chart
// order at the member prior, followed by nodes one score step lower.
func scoreOrgRecallMatches(matches orgRecallMatches) []orgRecallCandidate {
	candidates := make([]orgRecallCandidate, 0, len(matches.members)+len(matches.nodes))
	for _, member := range matches.members {
		candidates = append(candidates, orgRecallCandidate{
			kind:   orgRecallMemberCandidate,
			score:  recallOrgSourcePrior,
			member: member,
		})
	}
	for _, node := range matches.nodes {
		candidates = append(candidates, orgRecallCandidate{
			kind:  orgRecallNodeCandidate,
			score: recallOrgSourcePrior - 0.01,
			node:  node,
		})
	}
	return candidates
}

func outputOrgRecallEvidence(candidates []orgRecallCandidate, personPaths map[string]string) []recallEvidence {
	if len(candidates) == 0 {
		return nil
	}
	limit := len(candidates)
	if limit > recallOrgQuota {
		limit = recallOrgQuota
	}
	evidence := make([]recallEvidence, 0, limit)
	for _, candidate := range candidates[:limit] {
		switch candidate.kind {
		case orgRecallMemberCandidate:
			evidence = append(evidence, outputOrgMemberEvidence(candidate, personPaths))
		case orgRecallNodeCandidate:
			evidence = append(evidence, outputOrgNodeEvidence(candidate))
		}
	}
	return evidence
}

func outputOrgMemberEvidence(candidate orgRecallCandidate, personPaths map[string]string) recallEvidence {
	member := candidate.member
	source := personPaths[member.name]
	if source == "" {
		source = "조직도: " + member.name
	}
	note := member.department
	if member.role != "" {
		note += " · " + member.role
	}
	return recallEvidence{
		Kind: "org",
		// A member row exists because the person's full name (3+ runes) appears
		// literally in the message and that name is in the curated chart. That
		// is a lookup, not a ranked guess.
		Confidence: "high",
		Source:     source,
		Note:       note,
		Score:      candidate.score,
	}
}

func outputOrgNodeEvidence(candidate orgRecallCandidate) recallEvidence {
	node := candidate.node
	names := make([]string, 0, len(node.node.Members))
	for _, member := range node.node.Members {
		if title := memberTitleShort(member); title != "" {
			names = append(names, member.Name+title)
		} else {
			names = append(names, member.Name)
		}
	}
	note := "구성원 " + fmt.Sprint(len(node.node.Members)) + "명"
	if len(names) > 0 {
		note += ": " + strings.Join(names, ", ")
	}
	return recallEvidence{
		Kind: "org",
		// A department row needs only a 2-rune name to appear in the message,
		// which incidental text can satisfy — one step below member rows, the
		// same step the score anchors already encode.
		Confidence: "medium",
		Source:     "조직도: " + node.department,
		Note:       note,
		Score:      candidate.score,
	}
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

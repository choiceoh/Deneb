// Package org models the operator's group org chart (조직도) and makes it the
// MASTER source for the dashboard's part classification. The operator is a
// solar-group executive who maintains a tree of group → company → division →
// team; some of those nodes are tagged as a dashboard *lane* (파트). From that
// one editable tree the classification ruleset is *derived* (see DeriveRules):
// a lane node's members become person→lane rules, its keywords become
// keyword→lane rules, its companies become company→lane rules. Edit the chart
// once and the "파트별 업무 현황" grouping follows — no separate rules file to
// keep in sync.
//
// Relationship to internal/domain/classification:
//   - classification owns the *matching engine* (Rules + Classify) and the
//     legacy flat-map rules file. It knows nothing about org.
//   - org owns the *tree* and derives a classification.Rules from it. The
//     dependency is one-way (org → classification), so no import cycle.
//   - LoadRules (loader.go) is the single entry the dashboard uses: org.json
//     first, else the legacy classification rules file — full backward compat.
//
// ★ Privacy: the real chart (names of people/companies) is operator data and
// MUST NOT be committed. The tree is loaded at runtime from {stateDir}/org.json
// (see loader.go). The repo ships only org.example.json with FAKE names, and
// every test uses invented names.
package org

import (
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/classification"
)

// Node type values. The tree is intentionally shallow and these are advisory
// labels for the native chart (icon / indent semantics) — validation does not
// enforce that a team only nests under a division, since real org charts bend
// those rules (a company can sit directly under the group, a person-led "팀"
// can hang off a company). The string set is fixed so the client can switch on
// it without a magic-string drift.
const (
	NodeTypeGroup    = "group"    // the top-level holding/group entity
	NodeTypeCompany  = "company"  // a company under the group (e.g. 남도에코)
	NodeTypeDivision = "division" // a division/실 inside a company (e.g. 기획조정실)
	NodeTypeTeam     = "team"     // a team inside a division (e.g. 1팀)
)

// validNodeTypes is the accepted Type set; an unknown type fails validation so
// a typo in a hand-edited file surfaces instead of rendering a blank icon.
var validNodeTypes = map[string]bool{
	NodeTypeGroup:    true,
	NodeTypeCompany:  true,
	NodeTypeDivision: true,
	NodeTypeTeam:     true,
}

// Korean orgs separate two orthogonal axes that a single "title" field would
// conflate: 직급 (Rank) is the seniority grade in the corporate ladder, and 직책
// (Position) is the functional role a person holds. One person carries both —
// e.g. 전무(rank) who is also 실장(position). We model them as two independent
// enums; a member may set either, both, or neither (blank = unspecified).

// Rank (직급) values, ordered HIGHEST seniority → lowest. RankOrder uses this
// slice's index as the rank's seniority (0 = most senior), so the declaration
// order here IS the authority order — do not reorder casually.
const (
	RankChairman      = "회장"  // chairman
	RankPresident     = "사장"  // president / CEO
	RankVicePresident = "부사장" // vice president
	RankExecVP        = "전무"  // executive vice president (전무이사)
	RankSeniorVP      = "상무"  // senior vice president (상무이사)
	RankDirector      = "이사"  // director
	RankGeneralMgr    = "부장"  // general manager / department head grade
	RankDeputyGenMgr  = "차장"  // deputy general manager
	RankManager       = "과장"  // manager (section chief)
	RankAsstManager   = "대리"  // assistant manager
	RankSupervisor    = "주임"  // supervisor / senior staff
	RankStaff         = "사원"  // staff (entry grade)
)

// rankOrder lists ranks from most senior (index 0) to least. It is the single
// source for both the valid-rank set and RankOrder's seniority comparison.
var rankOrder = []string{
	RankChairman,
	RankPresident,
	RankVicePresident,
	RankExecVP,
	RankSeniorVP,
	RankDirector,
	RankGeneralMgr,
	RankDeputyGenMgr,
	RankManager,
	RankAsstManager,
	RankSupervisor,
	RankStaff,
}

// validRanks is the accepted Rank set (built from rankOrder). The empty string
// is NOT in this map but is accepted by validation (rank is optional).
var validRanks = func() map[string]bool {
	m := make(map[string]bool, len(rankOrder))
	for _, r := range rankOrder {
		m[r] = true
	}
	return m
}()

// Position (직책) values, listed broadest scope → narrowest. Position is a
// functional role, not a strict ladder, so there is no seniority comparison for
// it (unlike Rank); the set is fixed only so the client can switch on it and a
// typo fails validation.
const (
	PositionChairman = "회장"  // chairman (also a position when it denotes the role)
	PositionCEO      = "대표"  // representative director / CEO role
	PositionDivHead  = "본부장" // division head (본부)
	PositionOfficeHd = "실장"  // office head (실)
	PositionTeamLead = "팀장"  // team leader (팀)
	PositionTeamMem  = "팀원"  // team member
)

// validPositions is the accepted Position set. The empty string is NOT in this
// map but is accepted by validation (position is optional).
var validPositions = map[string]bool{
	PositionChairman: true,
	PositionCEO:      true,
	PositionDivHead:  true,
	PositionOfficeHd: true,
	PositionTeamLead: true,
	PositionTeamMem:  true,
}

// leaderPositions are the positions that make a member the LEADER of their node
// (부서장). A node's "head" is derived as its member(s) holding one of these,
// replacing the old standalone Head field (see OrgNode.Heads).
var leaderPositions = map[string]bool{
	PositionDivHead:  true, // 본부장 leads a 본부
	PositionOfficeHd: true, // 실장 leads a 실
	PositionTeamLead: true, // 팀장 leads a 팀
}

// Member is one person in a node (담당자), holding only the person's intrinsic
// attributes: name plus a 직급 (Rank, seniority grade) and 직책 (Position,
// functional role), which are independent so each is its own optional field.
// Affiliation (계열사/본부/실/팀) is deliberately NOT a member field — belonging
// is expressed entirely by the tree (a person sits under the company/division/
// team node they belong to, and 겸직 is the same person appearing under multiple
// nodes), so a Member.Affiliation would just duplicate the tree. Only Name feeds
// classification (Rank/Position are display/sort/filter metadata — see
// DeriveRules).
type Member struct {
	// Name is the person's display name; the only field used for classification
	// (normalized into a person→lane rule on a lane node).
	Name string `json:"name"`
	// Rank is the 직급 (seniority grade), one of the Rank* constants or "".
	Rank string `json:"rank,omitempty"`
	// Position is the 직책 (functional role), one of the Position* constants or
	// "". A leader position (본부장/실장/팀장) marks this member as the node head.
	Position string `json:"position,omitempty"`
}

// RankOrder returns a sortable seniority index for a 직급 string: 0 for the most
// senior rank (회장), increasing toward junior ranks, and a large sentinel for an
// unknown/blank rank (so unspecified ranks sort LAST in an ascending sort).
// Provided for future member sorting; classification does not use it.
func RankOrder(rank string) int {
	for i, r := range rankOrder {
		if r == rank {
			return i
		}
	}
	return len(rankOrder) + 1 // unknown/blank → sorts after every known rank
}

// OrgNode is one box in the org chart. The tree is expressed as a flat node
// list joined by ParentID (not nested children) so it round-trips cleanly
// through JSON and the native editor can move a subtree by rewriting one
// ParentID. A node with an empty ParentID is a root.
//
// A node becomes a dashboard *part* when Lane is non-empty: that Lane string is
// the stable lane key, the node Name is the lane's display label, and the
// node's Members / Keywords / Companies seed the classification rules for that
// lane (see DeriveRules). Nodes without a Lane are pure structure (a parent
// grouping that is not itself a dashboard column).
type OrgNode struct {
	// ID is the stable node identifier (client-generated, e.g. a slug or uuid).
	// Unique within the tree; referenced by children's ParentID.
	ID string `json:"id"`
	// Name is the human label shown in the chart and, for a lane node, the
	// dashboard column title.
	Name string `json:"name"`
	// Type is one of the NodeType* constants (group|company|division|team).
	Type string `json:"type"`
	// ParentID is the ID of this node's parent; "" marks a root node.
	ParentID string `json:"parentId,omitempty"`
	// Lane, when non-empty, promotes this node to a dashboard part. The value
	// is the lane key (stable id used in the dashboard response). Must be
	// unique across the tree.
	Lane string `json:"lane,omitempty"`
	// Members are the people who belong to this node (담당자), each carrying an
	// optional 직급 (Rank) and 직책 (Position). A person's affiliation is the node
	// itself (and 겸직 = the same person listed under several nodes), so there is
	// no per-member affiliation. For a lane node a member's Name becomes a
	// person→lane classification rule (strong signal); Rank/Position are
	// display/sort/filter metadata only. The node's leader (부서장) is no longer a
	// separate field — it is the member(s) whose Position is a leader role
	// (본부장/실장/팀장), surfaced via Heads().
	Members []Member `json:"members,omitempty"`
	// Keywords are domain terms that route an item to this node's lane (weak
	// signal). Only meaningful on a lane node.
	Keywords []string `json:"keywords,omitempty"`
	// Companies are counterparty/거래처 names handled by this node (medium
	// signal). Only meaningful on a lane node.
	Companies []string `json:"companies,omitempty"`
}

// OrgTree is the whole chart: a flat list of nodes joined by ParentID. The zero
// value (no nodes) is a valid empty tree that derives an empty ruleset (the
// dashboard then falls back to legacy rules — see LoadRules).
type OrgTree struct {
	Nodes []OrgNode `json:"nodes"`
}

// Roots returns the nodes with no parent, in input order. A well-formed chart
// usually has exactly one root (the group), but multiple roots are allowed
// (e.g. two sibling companies with no modeled parent).
func (t OrgTree) Roots() []OrgNode {
	var roots []OrgNode
	for _, n := range t.Nodes {
		if strings.TrimSpace(n.ParentID) == "" {
			roots = append(roots, n)
		}
	}
	return roots
}

// Children returns the direct children of the node with the given id, in input
// order. Returns nil for a leaf or an unknown id.
func (t OrgTree) Children(id string) []OrgNode {
	var kids []OrgNode
	for _, n := range t.Nodes {
		if n.ParentID == id {
			kids = append(kids, n)
		}
	}
	return kids
}

// LaneNodes returns the nodes that are tagged as a dashboard part (Lane != ""),
// in input order. Input order is the dashboard column order, so the operator
// controls part ordering by ordering the node list (the native editor writes
// nodes in chart order).
func (t OrgTree) LaneNodes() []OrgNode {
	var out []OrgNode
	for _, n := range t.Nodes {
		if strings.TrimSpace(n.Lane) != "" {
			out = append(out, n)
		}
	}
	return out
}

// Heads returns the node's leader(s) (부서장): the members whose Position is a
// leader role (본부장/실장/팀장), in member order. This replaces the old
// standalone Head field — the leader is now just a member with a leading
// Position, so it round-trips through the same edit and can never drift from the
// member list. Usually one head; returns nil when the node has no leader member.
func (n OrgNode) Heads() []Member {
	var heads []Member
	for _, m := range n.Members {
		if leaderPositions[strings.TrimSpace(m.Position)] {
			heads = append(heads, m)
		}
	}
	return heads
}

// Validate checks the tree is well-formed and safe to persist/derive from:
//
//   - every node has a non-empty ID, and IDs are unique;
//   - every node has a non-empty Name and a known Type;
//   - every member has a non-empty Name, and any non-blank Rank/Position is in
//     the known set (a blank Rank or Position is allowed — it means unspecified);
//   - every non-root ParentID references an existing node;
//   - there are no cycles (a node cannot be its own ancestor);
//   - lane keys are unique across the tree (two parts can't share a key).
//
// It returns the first problem found (so a bad save is rejected with a clear
// message rather than corrupting the chart or the derived dashboard). An empty
// tree validates (it's the legitimate "no chart yet" state).
func (t OrgTree) Validate() error {
	byID := make(map[string]int, len(t.Nodes))
	for _, n := range t.Nodes {
		id := strings.TrimSpace(n.ID)
		if id == "" {
			return fmt.Errorf("org: node with empty id (name=%q)", n.Name)
		}
		if _, dup := byID[id]; dup {
			return fmt.Errorf("org: duplicate node id %q", id)
		}
		byID[id] = 1
		if strings.TrimSpace(n.Name) == "" {
			return fmt.Errorf("org: node %q has empty name", id)
		}
		if !validNodeTypes[n.Type] {
			return fmt.Errorf("org: node %q has invalid type %q (want group|company|division|team)", id, n.Type)
		}
		for _, m := range n.Members {
			if strings.TrimSpace(m.Name) == "" {
				return fmt.Errorf("org: node %q has a member with empty name", id)
			}
			if r := strings.TrimSpace(m.Rank); r != "" && !validRanks[r] {
				return fmt.Errorf("org: node %q member %q has invalid rank %q", id, m.Name, r)
			}
			if pos := strings.TrimSpace(m.Position); pos != "" && !validPositions[pos] {
				return fmt.Errorf("org: node %q member %q has invalid position %q", id, m.Name, pos)
			}
		}
	}

	// Parent existence + lane uniqueness.
	seenLane := make(map[string]string) // lane key → owning node id
	for _, n := range t.Nodes {
		if p := strings.TrimSpace(n.ParentID); p != "" {
			if _, ok := byID[p]; !ok {
				return fmt.Errorf("org: node %q references missing parent %q", n.ID, p)
			}
			if p == n.ID {
				return fmt.Errorf("org: node %q is its own parent", n.ID)
			}
		}
		if lane := strings.TrimSpace(n.Lane); lane != "" {
			if owner, dup := seenLane[lane]; dup {
				return fmt.Errorf("org: lane key %q used by both %q and %q", lane, owner, n.ID)
			}
			seenLane[lane] = n.ID
		}
	}

	return t.checkNoCycles(byID)
}

// checkNoCycles walks each node's parent chain; if following parents revisits a
// node already on the current path, the tree has a cycle. byID is the validated
// id set (every ParentID is known by the time this runs). Bounded by node count
// per start node, so worst case O(n^2) — fine for a hand-sized org chart.
func (t OrgTree) checkNoCycles(byID map[string]int) error {
	parentOf := make(map[string]string, len(t.Nodes))
	for _, n := range t.Nodes {
		parentOf[n.ID] = strings.TrimSpace(n.ParentID)
	}
	for start := range byID {
		seen := map[string]bool{start: true}
		cur := parentOf[start]
		for cur != "" {
			if seen[cur] {
				return fmt.Errorf("org: cycle detected at node %q", cur)
			}
			seen[cur] = true
			cur = parentOf[cur]
		}
	}
	return nil
}

// DeriveRules projects the tree into a classification.Rules. For every
// lane-tagged node it folds:
//
//	Members   → PersonToLane   (each member's Name, normalized with the same wiki
//	                            helper the classifier uses, so a display name in
//	                            the chart matches a bare attendee/sender name;
//	                            the member's Rank/Position are ignored here)
//	Keywords  → KeywordToLane  (lowercased, matched as substrings)
//	Companies → CompanyToLane  (lowercased + space-stripped)
//
// The in-code domain keyword defaults (classification.DefaultKeywordRules) are
// used as a BASE so a fresh chart that hasn't enumerated keywords still routes
// generic domain terms; a node's own keywords merge on top (and can override a
// default that collides). People/company maps start empty — those are operator
// data and only the chart supplies them. The returned Rules feeds
// classification.Classify unchanged.
//
// Because the lane keys here come from the chart (not the fixed
// classification.Lane constants), the derived rules can reference any lane the
// operator defined. classification.Classify treats Lane as an opaque string, so
// arbitrary lane keys classify fine; only the legacy flat-file loader validates
// against the fixed constant set.
func (t OrgTree) DeriveRules() classification.Rules {
	rules := classification.Rules{
		PersonToLane:  map[string]classification.Lane{},
		CompanyToLane: map[string]classification.Lane{},
		KeywordToLane: map[string]classification.Lane{},
	}
	// Seed with the generic in-code keyword defaults so unconfigured charts
	// still classify domain terms. A lane node's own keywords override on
	// collision (applied below, after the seed).
	for kw, lane := range classification.DefaultKeywordRules() {
		rules.KeywordToLane[kw] = lane
	}

	for _, n := range t.LaneNodes() {
		lane := classification.Lane(strings.TrimSpace(n.Lane))
		for _, m := range n.Members {
			// Only the member's Name routes classification; Rank/Position are
			// display/sort metadata and never affect lane assignment.
			key := classification.NormalizePersonName(m.Name)
			if len([]rune(key)) < 2 {
				continue // too short/ambiguous to be a reliable person key
			}
			rules.PersonToLane[key] = lane
		}
		for _, c := range n.Companies {
			key := classification.NormalizeCompany(c)
			if key == "" {
				continue
			}
			rules.CompanyToLane[key] = lane
		}
		for _, kw := range n.Keywords {
			key := strings.ToLower(strings.TrimSpace(kw))
			if key == "" {
				continue
			}
			rules.KeywordToLane[key] = lane
		}
	}
	return rules
}

// LaneDef is a derived dashboard lane: the chart-defined key plus its display
// name (the lane node's Name). DeriveLanes returns these in chart order so the
// dashboard can render columns from the org chart instead of the hardcoded
// classification.AllLanes set.
type LaneDef struct {
	Key  string
	Name string
}

// DeriveLanes returns the dashboard lane definitions from the lane-tagged
// nodes, in chart (input) order. Empty when no node is lane-tagged (the
// dashboard then keeps its legacy hardcoded lanes — see LoadRules/HasLanes).
func (t OrgTree) DeriveLanes() []LaneDef {
	nodes := t.LaneNodes()
	out := make([]LaneDef, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, LaneDef{Key: strings.TrimSpace(n.Lane), Name: strings.TrimSpace(n.Name)})
	}
	return out
}

// HasLanes reports whether the tree defines at least one dashboard part. Used
// by the dashboard to decide between org-derived lanes and the legacy
// hardcoded lane set.
func (t OrgTree) HasLanes() bool {
	for _, n := range t.Nodes {
		if strings.TrimSpace(n.Lane) != "" {
			return true
		}
	}
	return false
}

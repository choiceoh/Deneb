package org

// FAKE data only — every name/company below is invented. The real chart lives
// in the operator's {stateDir}/org.json, never in the repo (privacy invariant
// shared with the classification package).

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/classification"
)

// fakeTree is a small, valid chart used across the derivation/helper tests:
// a group root → division → two lane teams, plus a lane company sibling. Members
// carry fake 직급/직책 to exercise the Rank/Position fields; only Name matters to
// classification. Affiliation is the tree itself (a person belongs to the node
// they sit under), so members have no affiliation field — 홍길동 appearing under
// both g and plan is the 겸직 case, expressed structurally.
func fakeTree() OrgTree {
	return OrgTree{Nodes: []OrgNode{
		{
			ID: "g", Name: "예시그룹", Type: NodeTypeGroup,
			Members: []Member{{Name: "홍길동", Rank: RankChairman, Position: PositionChairman}},
		},
		{
			ID: "plan", Name: "기획조정실", Type: NodeTypeDivision, ParentID: "g",
			Members: []Member{{Name: "홍길동", Rank: RankExecVP, Position: PositionOfficeHd}},
		},
		{
			ID: "t1", Name: "1팀", Type: NodeTypeTeam, ParentID: "plan", Lane: "team1",
			Members:  []Member{{Name: "김철수", Rank: RankExecVP, Position: PositionTeamLead}, {Name: "박영수 부장", Rank: RankGeneralMgr, Position: PositionTeamMem}},
			Keywords: []string{"인허가"}, Companies: []string{"사아건설"},
		},
		{
			ID: "t2", Name: "2팀", Type: NodeTypeTeam, ParentID: "plan", Lane: "team2",
			Members: []Member{{Name: "이몽룡", Position: PositionTeamLead}}, Keywords: []string{"루프탑"},
		},
		{
			ID: "nd", Name: "남도에코", Type: NodeTypeCompany, ParentID: "g", Lane: "namdo",
			Members: []Member{{Name: "성춘향", Rank: RankSeniorVP, Position: PositionCEO}}, Companies: []string{"가나에너지"},
		},
	}}
}

func TestTreeHelpers(t *testing.T) {
	tree := fakeTree()

	if roots := tree.Roots(); len(roots) != 1 || roots[0].ID != "g" {
		t.Fatalf("Roots = %+v, want single root g", roots)
	}
	if kids := tree.Children("g"); len(kids) != 2 {
		t.Fatalf("Children(g) = %d, want 2 (plan, nd)", len(kids))
	}
	if kids := tree.Children("plan"); len(kids) != 2 {
		t.Fatalf("Children(plan) = %d, want 2 (t1, t2)", len(kids))
	}
	if kids := tree.Children("t1"); kids != nil {
		t.Fatalf("Children(t1) = %+v, want nil (leaf)", kids)
	}
	laneNodes := tree.LaneNodes()
	if len(laneNodes) != 3 {
		t.Fatalf("LaneNodes = %d, want 3 (t1,t2,nd)", len(laneNodes))
	}
	if !tree.HasLanes() {
		t.Fatal("HasLanes = false, want true")
	}
}

func TestValidate_OK(t *testing.T) {
	if err := fakeTree().Validate(); err != nil {
		t.Fatalf("valid tree rejected: %v", err)
	}
}

func TestValidate_EmptyTreeOK(t *testing.T) {
	// The "no chart yet" state must validate.
	if err := (OrgTree{}).Validate(); err != nil {
		t.Fatalf("empty tree rejected: %v", err)
	}
	if (OrgTree{}).HasLanes() {
		t.Fatal("empty tree HasLanes = true, want false")
	}
}

func TestValidate_MissingParent(t *testing.T) {
	tree := OrgTree{Nodes: []OrgNode{
		{ID: "a", Name: "A", Type: NodeTypeGroup},
		{ID: "b", Name: "B", Type: NodeTypeTeam, ParentID: "ghost"},
	}}
	if err := tree.Validate(); err == nil {
		t.Fatal("missing parent: expected error, got nil")
	}
}

func TestValidate_Cycle(t *testing.T) {
	// a → b → a (each names the other as parent): a cycle with no root.
	tree := OrgTree{Nodes: []OrgNode{
		{ID: "a", Name: "A", Type: NodeTypeDivision, ParentID: "b"},
		{ID: "b", Name: "B", Type: NodeTypeTeam, ParentID: "a"},
	}}
	if err := tree.Validate(); err == nil {
		t.Fatal("cycle: expected error, got nil")
	}
}

func TestValidate_SelfParent(t *testing.T) {
	tree := OrgTree{Nodes: []OrgNode{
		{ID: "a", Name: "A", Type: NodeTypeGroup, ParentID: "a"},
	}}
	if err := tree.Validate(); err == nil {
		t.Fatal("self-parent: expected error, got nil")
	}
}

func TestValidate_DuplicateID(t *testing.T) {
	tree := OrgTree{Nodes: []OrgNode{
		{ID: "a", Name: "A", Type: NodeTypeGroup},
		{ID: "a", Name: "A2", Type: NodeTypeTeam},
	}}
	if err := tree.Validate(); err == nil {
		t.Fatal("duplicate id: expected error, got nil")
	}
}

func TestValidate_DuplicateLane(t *testing.T) {
	// Two nodes claiming the same lane key would collide the dashboard column.
	tree := OrgTree{Nodes: []OrgNode{
		{ID: "a", Name: "A", Type: NodeTypeTeam, Lane: "x"},
		{ID: "b", Name: "B", Type: NodeTypeTeam, Lane: "x"},
	}}
	if err := tree.Validate(); err == nil {
		t.Fatal("duplicate lane: expected error, got nil")
	}
}

func TestValidate_BadTypeAndEmptyFields(t *testing.T) {
	cases := []struct {
		name string
		node OrgNode
	}{
		{"empty id", OrgNode{ID: "", Name: "A", Type: NodeTypeGroup}},
		{"empty name", OrgNode{ID: "a", Name: "", Type: NodeTypeGroup}},
		{"bad type", OrgNode{ID: "a", Name: "A", Type: "platoon"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := (OrgTree{Nodes: []OrgNode{c.node}}).Validate(); err == nil {
				t.Fatalf("%s: expected error, got nil", c.name)
			}
		})
	}
}

func TestDeriveRules(t *testing.T) {
	rules := fakeTree().DeriveRules()

	// Members → PersonToLane (member Name only; Rank/Position ignored), normalized
	// (honorific peeled): "박영수 부장" → "박영수".
	if rules.PersonToLane["김철수"] != classification.Lane("team1") {
		t.Errorf("김철수 → %q, want team1", rules.PersonToLane["김철수"])
	}
	if rules.PersonToLane["박영수"] != classification.Lane("team1") {
		t.Errorf("박영수 (honorific peeled) → %q, want team1", rules.PersonToLane["박영수"])
	}
	if rules.PersonToLane["이몽룡"] != classification.Lane("team2") {
		t.Errorf("이몽룡 → %q, want team2", rules.PersonToLane["이몽룡"])
	}
	if rules.PersonToLane["성춘향"] != classification.Lane("namdo") {
		t.Errorf("성춘향 → %q, want namdo", rules.PersonToLane["성춘향"])
	}
	// Companies → CompanyToLane.
	if rules.CompanyToLane["사아건설"] != classification.Lane("team1") {
		t.Errorf("사아건설 → %q, want team1", rules.CompanyToLane["사아건설"])
	}
	if rules.CompanyToLane["가나에너지"] != classification.Lane("namdo") {
		t.Errorf("가나에너지 → %q, want namdo", rules.CompanyToLane["가나에너지"])
	}
	// Keywords → KeywordToLane (node-defined).
	if rules.KeywordToLane["루프탑"] != classification.Lane("team2") {
		t.Errorf("루프탑 → %q, want team2", rules.KeywordToLane["루프탑"])
	}
	// ★ Chart mode does NOT seed the in-code keyword defaults: a default keyword
	// no node enumerated (e.g. 케이블) must be absent, so items can't be routed to
	// a constant lane the chart's columns don't contain (the silent-drop guard).
	if lane, ok := rules.KeywordToLane["케이블"]; ok {
		t.Errorf("default keyword 케이블 leaked → %q; chart-mode DeriveRules must not seed defaults", lane)
	}

	// End-to-end: the derived rules classify via the unchanged engine.
	if lane, conf := rules.Classify(classification.Signals{People: []string{"이몽룡 팀장"}}); lane != classification.Lane("team2") || conf != classification.ConfStrong {
		t.Errorf("classify person: got (%q,%d), want (team2, ConfStrong)", lane, conf)
	}
	if lane, _ := rules.Classify(classification.Signals{Text: "루프탑 점검"}); lane != classification.Lane("team2") {
		t.Errorf("classify keyword: got %q, want team2", lane)
	}
}

func TestDeriveRules_NoDefaultKeywordSeedWhenLanes(t *testing.T) {
	// ★ Regression guard (HIGH): when the chart defines lanes (keyed by node id,
	// like the native editor), DeriveRules must NOT seed the in-code keyword
	// defaults (keyed to the fixed team1/… constants). Seeding them routes items
	// to constant lanes the chart's columns don't contain, and groupByLane then
	// drops those items silently. So a default keyword the chart didn't enumerate
	// must be absent from the derived rules.
	tree := OrgTree{Nodes: []OrgNode{
		{ID: "n100", Name: "영업", Type: NodeTypeTeam, Lane: "n100", Keywords: []string{"제안"}},
	}}
	rules := tree.DeriveRules()
	// The node's own keyword is present, routed to the node-id lane.
	if rules.KeywordToLane["제안"] != classification.Lane("n100") {
		t.Errorf("제안 → %q, want n100 (node keyword)", rules.KeywordToLane["제안"])
	}
	// A default domain keyword the chart did NOT enumerate must not leak in (it
	// would carry a constant lane like team1 that has no column → silent drop).
	if lane, ok := rules.KeywordToLane["인허가"]; ok {
		t.Errorf("default keyword 인허가 leaked → %q; chart-mode DeriveRules must not seed defaults", lane)
	}
}

func TestDeriveRules_MoonlightingPicksMinLane(t *testing.T) {
	// MED: a person/keyword/company that appears under TWO lane nodes (겸직 or a
	// shared term) must resolve to the lexicographically smallest lane, matching
	// the engine's pickLane tie-break — not collapse to whichever node came last
	// in input order. The min lane ("team_a") is listed FIRST here on purpose: a
	// naive last-writer-wins would overwrite it with "team_z" (processed last) and
	// disagree with the engine, so this ordering makes the test discriminating.
	tree := OrgTree{Nodes: []OrgNode{
		{
			ID: "a", Name: "A", Type: NodeTypeTeam, Lane: "team_a",
			Members:  []Member{{Name: "홍길동"}},
			Keywords: []string{"공용키워드"}, Companies: []string{"공용상사"},
		},
		{
			ID: "z", Name: "Z", Type: NodeTypeTeam, Lane: "team_z",
			Members:  []Member{{Name: "홍길동"}},
			Keywords: []string{"공용키워드"}, Companies: []string{"공용상사"},
		},
	}}
	rules := tree.DeriveRules()
	if got := rules.PersonToLane["홍길동"]; got != classification.Lane("team_a") {
		t.Errorf("겸직 person 홍길동 → %q, want team_a (min lane)", got)
	}
	if got := rules.KeywordToLane["공용키워드"]; got != classification.Lane("team_a") {
		t.Errorf("shared keyword → %q, want team_a (min lane)", got)
	}
	if got := rules.CompanyToLane["공용상사"]; got != classification.Lane("team_a") {
		t.Errorf("shared company → %q, want team_a (min lane)", got)
	}
	// And the derived map must agree with the engine's own resolution.
	if lane, _ := rules.Classify(classification.Signals{People: []string{"홍길동"}}); lane != classification.Lane("team_a") {
		t.Errorf("engine Classify 홍길동 → %q, want team_a (agrees with DeriveRules)", lane)
	}
}

func TestValidate_RejectsReservedUnclassifiedLane(t *testing.T) {
	// LOW: "unclassified" is the dashboard's reserved holding-lane key. A chart
	// node claiming it would render a duplicate 미분류 column, so Validate rejects.
	tree := OrgTree{Nodes: []OrgNode{
		{ID: "a", Name: "A", Type: NodeTypeTeam, Lane: string(classification.LaneUnclassified)},
	}}
	if err := tree.Validate(); err == nil {
		t.Fatal("reserved lane key 'unclassified': expected error, got nil")
	}
}

func TestDeriveRules_ShortNameSkipped(t *testing.T) {
	// A 1-rune member is too ambiguous to be a person key and must be dropped.
	tree := OrgTree{Nodes: []OrgNode{
		{ID: "x", Name: "X", Type: NodeTypeTeam, Lane: "x", Members: []Member{{Name: "김"}}},
	}}
	rules := tree.DeriveRules()
	if _, ok := rules.PersonToLane["김"]; ok {
		t.Error("1-rune member should be skipped")
	}
}

func TestDeriveLanes_OrderAndNames(t *testing.T) {
	defs := fakeTree().DeriveLanes()
	// Chart order: t1, t2, nd (input order of lane nodes).
	wantKeys := []string{"team1", "team2", "namdo"}
	if len(defs) != len(wantKeys) {
		t.Fatalf("DeriveLanes = %d, want %d", len(defs), len(wantKeys))
	}
	for i, k := range wantKeys {
		if defs[i].Key != k {
			t.Fatalf("lane[%d].Key = %q, want %q", i, defs[i].Key, k)
		}
	}
	// Display name comes from the node Name.
	if defs[2].Name != "남도에코" {
		t.Errorf("namdo lane name = %q, want 남도에코", defs[2].Name)
	}
}

func TestValidate_MemberRankPosition(t *testing.T) {
	cases := []struct {
		name    string
		member  Member
		wantErr bool
	}{
		{"valid rank+position", Member{Name: "홍길동", Rank: RankExecVP, Position: PositionOfficeHd}, false},
		{"blank rank and position ok", Member{Name: "김철수"}, false},
		{"blank rank, set position ok", Member{Name: "이몽룡", Position: PositionTeamMem}, false},
		{"set rank, blank position ok", Member{Name: "성춘향", Rank: RankManager}, false},
		{"empty member name rejected", Member{Name: "", Rank: RankStaff}, true},
		{"unknown rank rejected", Member{Name: "변학도", Rank: "초대리"}, true},
		{"unknown position rejected", Member{Name: "변학도", Position: "조장"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tree := OrgTree{Nodes: []OrgNode{
				{ID: "x", Name: "X", Type: NodeTypeTeam, Members: []Member{c.member}},
			}}
			err := tree.Validate()
			if c.wantErr && err == nil {
				t.Fatalf("%s: expected error, got nil", c.name)
			}
			if !c.wantErr && err != nil {
				t.Fatalf("%s: unexpected error: %v", c.name, err)
			}
		})
	}
}

func TestRankOrder_Seniority(t *testing.T) {
	// 회장 is most senior (0) and ranks strictly increase toward junior grades.
	if RankOrder(RankChairman) != 0 {
		t.Errorf("RankChairman order = %d, want 0 (most senior)", RankOrder(RankChairman))
	}
	if !(RankOrder(RankChairman) < RankOrder(RankExecVP) && RankOrder(RankExecVP) < RankOrder(RankStaff)) {
		t.Errorf("rank order not ascending senior→junior: 회장=%d 전무=%d 사원=%d",
			RankOrder(RankChairman), RankOrder(RankExecVP), RankOrder(RankStaff))
	}
	// An unknown/blank rank sorts AFTER every known rank (largest order value).
	if RankOrder("") <= RankOrder(RankStaff) {
		t.Errorf("blank rank order = %d, want > 사원 order %d (sorts last)", RankOrder(""), RankOrder(RankStaff))
	}
	if RankOrder("없는직급") <= RankOrder(RankStaff) {
		t.Errorf("unknown rank should sort after the most junior known rank")
	}
}

func TestHeads_DerivedFromLeaderPosition(t *testing.T) {
	node := OrgNode{ID: "t", Name: "1팀", Type: NodeTypeTeam, Members: []Member{
		{Name: "김철수", Rank: RankExecVP, Position: PositionTeamLead}, // leader
		{Name: "이몽룡", Rank: RankManager, Position: PositionTeamMem}, // not a leader
	}}
	heads := node.Heads()
	if len(heads) != 1 || heads[0].Name != "김철수" {
		t.Fatalf("Heads = %+v, want single head 김철수 (팀장)", heads)
	}

	// 본부장 and 실장 also count as leaders; a node with no leader member yields nil.
	if h := (OrgNode{Members: []Member{{Name: "A", Position: PositionDivHead}}}).Heads(); len(h) != 1 {
		t.Errorf("본부장 should be a head, got %+v", h)
	}
	if h := (OrgNode{Members: []Member{{Name: "A", Position: PositionTeamMem}}}).Heads(); h != nil {
		t.Errorf("no leader position → Heads should be nil, got %+v", h)
	}
}

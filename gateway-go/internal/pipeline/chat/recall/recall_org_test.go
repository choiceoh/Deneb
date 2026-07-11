package recall

import (
	"context"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/org"
)

// fakeOrg is a tiny two-level chart: 탑솔라 → 기획조정실(오선택 전무) → 모듈팀(차남두 부장).
func fakeOrg() org.OrgTree {
	return org.OrgTree{Nodes: []org.OrgNode{
		{ID: "root", Name: "탑솔라", Type: "company"},
		{ID: "plan", Name: "기획조정실", Type: "division", ParentID: "root", Members: []org.Member{
			{Name: "오선택", Rank: "전무", Position: "기획조정실장"},
		}},
		{ID: "mod", Name: "모듈팀", Type: "team", ParentID: "plan", Members: []org.Member{
			{Name: "차남두", Rank: "부장"},
			{Name: "김성훈", Position: "이사"},
		}},
	}}
}

func loadFake(t org.OrgTree) func() (org.OrgTree, error) {
	return func() (org.OrgTree, error) { return t, nil }
}

func TestRecallOrgEvidence_MemberMatchWithDeptPath(t *testing.T) {
	ev := recallOrgEvidence(context.Background(), loadFake(fakeOrg()), nil, "오선택 전무가 요즘 뭐 챙기지?")
	if len(ev) != 1 {
		t.Fatalf("want 1 org row, got %d: %+v", len(ev), ev)
	}
	if ev[0].Kind != "org" {
		t.Errorf("Kind = %q, want org", ev[0].Kind)
	}
	// nil store → no person link, so Source is the org fallback label.
	if !strings.Contains(ev[0].Source, "오선택") {
		t.Errorf("Source = %q, want it to name 오선택", ev[0].Source)
	}
	// Note carries the full 부서 path + 직급/직책.
	for _, want := range []string{"탑솔라", "기획조정실", "전무", "기획조정실장"} {
		if !strings.Contains(ev[0].Note, want) {
			t.Errorf("Note = %q, missing %q", ev[0].Note, want)
		}
	}
}

func TestRecallOrgEvidence_NodeMatchListsMembers(t *testing.T) {
	ev := recallOrgEvidence(context.Background(), loadFake(fakeOrg()), nil, "모듈팀은 누구누구야?")
	if len(ev) == 0 {
		t.Fatal("want a division row for 모듈팀, got none")
	}
	// The 모듈팀 node row should list its members (차남두, 김성훈).
	var found bool
	for _, e := range ev {
		if strings.Contains(e.Source, "모듈팀") {
			found = true
			for _, name := range []string{"차남두", "김성훈"} {
				if !strings.Contains(e.Note, name) {
					t.Errorf("모듈팀 row Note = %q, missing member %q", e.Note, name)
				}
			}
		}
	}
	if !found {
		t.Errorf("no 모듈팀 division row in %+v", ev)
	}
}

func TestRecallOrgEvidence_NoMatchIsEmpty(t *testing.T) {
	if ev := recallOrgEvidence(context.Background(), loadFake(fakeOrg()), nil, "오늘 날씨 어때?"); ev != nil {
		t.Errorf("want nil for an unrelated message, got %+v", ev)
	}
}

func TestRecallOrgEvidence_QuotaBounds(t *testing.T) {
	// A message naming many members must not exceed the quota.
	msg := "오선택 차남두 김성훈 회의"
	ev := recallOrgEvidence(context.Background(), loadFake(fakeOrg()), nil, msg)
	if len(ev) > recallOrgQuota {
		t.Errorf("got %d rows, exceeds quota %d", len(ev), recallOrgQuota)
	}
}

func TestRecallOrgEvidence_NilLoadOrEmpty(t *testing.T) {
	if ev := recallOrgEvidence(context.Background(), nil, nil, "오선택"); ev != nil {
		t.Errorf("nil load must yield nil, got %+v", ev)
	}
	empty := func() (org.OrgTree, error) { return org.OrgTree{}, nil }
	if ev := recallOrgEvidence(context.Background(), empty, nil, "오선택"); ev != nil {
		t.Errorf("empty tree must yield nil, got %+v", ev)
	}
}

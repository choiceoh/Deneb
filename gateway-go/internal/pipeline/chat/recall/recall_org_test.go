package recall

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/org"
	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
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
	got := recallOrgEvidence(context.Background(), loadFake(fakeOrg()), nil, "오선택 전무가 요즘 뭐 챙기지?")
	want := []recallEvidence{{
		Kind:       "org",
		Confidence: "high",
		Source:     "조직도: 오선택",
		Note:       "탑솔라 · 기획조정실 · 전무 · 기획조정실장",
		Score:      recallOrgSourcePrior,
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("recallOrgEvidence() = %+v, want %+v", got, want)
	}
}

func TestRecallOrgEvidenceReturnsNodeMembersList(t *testing.T) {
	got := recallOrgEvidence(context.Background(), loadFake(fakeOrg()), nil, "모듈팀은 누구누구야?")
	want := []recallEvidence{{
		Kind:       "org",
		Confidence: "medium",
		Source:     "조직도: 탑솔라 · 기획조정실 · 모듈팀",
		Note:       "구성원 2명: 차남두 (부장), 김성훈 (이사)",
		Score:      recallOrgSourcePrior - 0.01,
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("recallOrgEvidence() = %+v, want %+v", got, want)
	}
}

func TestRecallOrgEvidence_PersonPageReplacesFallbackSource(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()
	person := wiki.NewPage("오선택 전무 (기획조정실장)", "인물", nil)
	const personPath = "인물/오선택-전무-(기획조정실장).md"
	if err := store.WritePage(personPath, person); err != nil {
		t.Fatal(err)
	}

	got := recallOrgEvidence(context.Background(), loadFake(fakeOrg()), store, "오선택 관련 현황")
	if len(got) != 1 || got[0].Source != personPath {
		t.Fatalf("recallOrgEvidence() = %+v, want person source %q", got, personPath)
	}
}

func TestRecallOrgEvidence_NoMatchIsEmpty(t *testing.T) {
	if ev := recallOrgEvidence(context.Background(), loadFake(fakeOrg()), nil, "오늘 날씨 어때?"); ev != nil {
		t.Errorf("want nil for an unrelated message, got %+v", ev)
	}
}

func TestRecallOrgEvidence_RanksMembersBeforeNodesWithinQuota(t *testing.T) {
	got := recallOrgEvidence(context.Background(), loadFake(fakeOrg()), nil, "모듈팀 오선택 차남두 김성훈 회의")
	if len(got) != recallOrgQuota {
		t.Fatalf("got %d rows, want quota %d: %+v", len(got), recallOrgQuota, got)
	}
	wantSources := []string{"조직도: 오선택", "조직도: 차남두", "조직도: 김성훈"}
	for i, want := range wantSources {
		if got[i].Source != want || got[i].Score != recallOrgSourcePrior {
			t.Errorf("row %d = %+v, want source %q with member score", i, got[i], want)
		}
		if strings.Contains(got[i].Source, "모듈팀") {
			t.Errorf("node row outranked a matched member: %+v", got)
		}
	}
}

func TestRecallOrgEvidenceReturnsFirstUniqueEntityMatch(t *testing.T) {
	tree := org.OrgTree{Nodes: []org.OrgNode{
		{ID: "first", Name: "팀", Members: []org.Member{{Name: "이수"}, {Name: "이수민", Rank: "과장"}}},
		{ID: "second", Name: "지원팀", Members: []org.Member{{Name: "이수민", Position: "팀장"}}},
	}}

	got := recallOrgEvidence(context.Background(), loadFake(tree), nil, "팀 이수 이수민 확인")
	want := []recallEvidence{{
		Kind:       "org",
		Confidence: "high",
		Source:     "조직도: 이수민",
		Note:       "팀 · 과장",
		Score:      recallOrgSourcePrior,
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("recallOrgEvidence() = %+v, want first unique 3-rune member only: %+v", got, want)
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
	failed := func() (org.OrgTree, error) { return org.OrgTree{}, errors.New("org unavailable") }
	if ev := recallOrgEvidence(context.Background(), failed, nil, "오선택"); ev != nil {
		t.Errorf("load error must yield nil, got %+v", ev)
	}
}

func TestRecallOrgEvidence_ShortQuerySkipsLoad(t *testing.T) {
	loads := 0
	load := func() (org.OrgTree, error) {
		loads++
		return fakeOrg(), nil
	}
	for _, message := range []string{"", " ", "가"} {
		if got := recallOrgEvidence(context.Background(), load, nil, message); got != nil {
			t.Errorf("message %q returned %+v, want nil", message, got)
		}
	}
	if loads != 0 {
		t.Fatalf("short queries loaded org chart %d times, want 0", loads)
	}
}

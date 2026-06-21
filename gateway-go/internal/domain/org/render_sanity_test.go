package org

import "testing"

func TestRenderText(t *testing.T) {
	tree := OrgTree{Nodes: []OrgNode{
		{ID: "g", Name: "탑솔라그룹", Type: "group"},
		{ID: "c", Name: "탑솔라", Type: "company", ParentID: "g"},
		{ID: "d", Name: "기획조정실", Type: "division", ParentID: "c", Members: []Member{{Name: "오선택", Rank: "전무", Position: "실장"}}},
		{ID: "t", Name: "1팀", Type: "team", ParentID: "d", Members: []Member{{Name: "김승리", Rank: "차장"}}},
	}}
	want := "- 탑솔라그룹 (그룹)\n  - 탑솔라 (회사)\n    - 기획조정실 (본부/실)\n      · 오선택 (전무·실장)\n      - 1팀 (팀)\n        · 김승리 (차장)\n"
	if got := tree.RenderText(); got != want {
		t.Fatalf("RenderText mismatch:\n got=%q\nwant=%q", got, want)
	}
	if got := (OrgTree{}).RenderText(); got != "" {
		t.Errorf("empty tree should render empty, got %q", got)
	}
}

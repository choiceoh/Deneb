package wiki

import (
	"sort"
	"testing"
)

// TestProjectOwnedRefs: a project owns pages that share its frozen code (folder
// inheritance) or explicitly link it (Related[]/[[wiki-link]], by path or code).
// Fuzzy signals (tags, mentions) are excluded, the 대표페이지 is never its own ref,
// and a different project's pages are never attributed.
func TestProjectOwnedRefs(t *testing.T) {
	store := newProjectTestStore(t)
	defer store.Close()

	const codeA = "pl1-tps-sup-001"
	write := func(path, title, code string, related []string, body string) {
		t.Helper()
		page := NewPage(title, "프로젝트", nil)
		page.Meta.Code = code
		page.Meta.Related = related
		if body == "" {
			body = "# " + title
		}
		page.Body = body
		if err := store.WritePage(path, page); err != nil {
			t.Fatalf("WritePage(%s): %v", path, err)
		}
	}

	// Project A (대표페이지) + four pages that should resolve to it, four ways.
	write("프로젝트/탑솔라.md", "탑솔라", codeA, nil, "")
	write("프로젝트/탑솔라/이력.md", "탑솔라 이력", codeA, nil, "")                  // shared code (folder inheritance)
	write("프로젝트/거래/한빛전기.md", "한빛전기", "", []string{"프로젝트/탑솔라.md"}, "")  // Related by path
	write("프로젝트/거래/남선.md", "남선", "", []string{codeA}, "")              // Related by code
	write("프로젝트/메모/킥오프.md", "킥오프", "", nil, "회의 메모 — [[프로젝트/탑솔라]] 참고") // inline link

	// Project B and an unrelated page — must never be attributed to A.
	write("프로젝트/영산고.md", "영산고", "pl2-ysg-epc-001", nil, "")
	write("프로젝트/거래/무관.md", "무관거래", "", nil, "아무 프로젝트와도 무관")

	projects := []ProjectStatus{
		{Path: "프로젝트/탑솔라.md", Code: codeA},
		{Path: "프로젝트/영산고.md", Code: "pl2-ysg-epc-001"},
	}
	owned := store.projectOwnedRefs(projects)

	gotA := append([]string(nil), owned["프로젝트/탑솔라.md"]...)
	sort.Strings(gotA)
	wantA := []string{
		"프로젝트/거래/남선.md",
		"프로젝트/거래/한빛전기.md",
		"프로젝트/메모/킥오프.md",
		"프로젝트/탑솔라/이력.md",
	}
	if len(gotA) != len(wantA) {
		t.Fatalf("탑솔라 refs = %v, want %v", gotA, wantA)
	}
	for i := range wantA {
		if gotA[i] != wantA[i] {
			t.Fatalf("탑솔라 refs = %v, want %v", gotA, wantA)
		}
	}

	// The 대표페이지 is never listed among its own refs.
	for _, r := range gotA {
		if r == "프로젝트/탑솔라.md" {
			t.Fatalf("project page listed as its own ref: %v", gotA)
		}
	}

	// Project B owns nothing here, and the unrelated deal page leaked to no one.
	if got := owned["프로젝트/영산고.md"]; len(got) != 0 {
		t.Fatalf("영산고 refs = %v, want empty", got)
	}
	for _, r := range gotA {
		if r == "프로젝트/거래/무관.md" {
			t.Fatalf("unrelated page attributed to 탑솔라: %v", gotA)
		}
	}
}

package wiki

import (
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestResolvePersonPaths(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	// A real page carries a 직급 in its title; NormalizePersonName strips it so a
	// bare org-member name ("오선택") resolves to the 직급-suffixed page.
	osk := NewPage("오선택 전무 (기획조정실장)", "인물", nil)
	if err := store.WritePage("인물/오선택-전무-(기획조정실장).md", osk); err != nil {
		t.Fatalf("WritePage 오선택: %v", err)
	}
	cnd := NewPage("차남두", "인물", nil)
	if err := store.WritePage("인물/차남두.md", cnd); err != nil {
		t.Fatalf("WritePage 차남두: %v", err)
	}

	got := store.ResolvePersonPaths([]string{"오선택", "차남두 부장", "없는사람"})

	if got["오선택"] != "인물/오선택-전무-(기획조정실장).md" {
		t.Errorf("오선택 → %q, want the 직급-suffixed page path", got["오선택"])
	}
	// "차남두 부장" normalizes to "차남두" and resolves under the original key.
	if got["차남두 부장"] != "인물/차남두.md" {
		t.Errorf("차남두 부장 → %q, want 인물/차남두.md", got["차남두 부장"])
	}
	if _, ok := got["없는사람"]; ok {
		t.Errorf("없는사람 must be absent, got %q", got["없는사람"])
	}
}

func TestResolvePersonByEmail_DisambiguatesHomonyms(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	// Two people share the name 김성훈 but own different addresses — exactly the
	// case name matching conflates. Each 인물 page declares its identity email.
	marsh := NewPage("김성훈", "인물", nil)
	marsh.Meta.Emails = []string{"sunghoon.kim@marsh.com"}
	if err := store.WritePage("인물/김성훈-marsh.md", marsh); err != nil {
		t.Fatalf("WritePage marsh: %v", err)
	}
	bohae := NewPage("김성훈", "인물", nil)
	bohae.Meta.Emails = []string{"akim@bohae.co.kr"}
	if err := store.WritePage("인물/김성훈-bohae.md", bohae); err != nil {
		t.Fatalf("WritePage bohae: %v", err)
	}

	// The emails frontmatter must survive a write→read round-trip.
	got := testutil.Must(store.ReadPage("인물/김성훈-marsh.md"))
	if len(got.Meta.Emails) != 1 || got.Meta.Emails[0] != "sunghoon.kim@marsh.com" {
		t.Fatalf("emails frontmatter round-trip failed: %v", got.Meta.Emails)
	}

	// Each address resolves to its OWN page — the disambiguation name matching
	// cannot do (case-insensitive).
	if p := store.ResolvePersonByEmail("SunghOon.Kim@Marsh.com"); p != "인물/김성훈-marsh.md" {
		t.Errorf("marsh email → %q, want 인물/김성훈-marsh.md", p)
	}
	if p := store.ResolvePersonByEmail("akim@bohae.co.kr"); p != "인물/김성훈-bohae.md" {
		t.Errorf("bohae email → %q, want 인물/김성훈-bohae.md", p)
	}
	if p := store.ResolvePersonByEmail("nobody@nowhere.com"); p != "" {
		t.Errorf("unknown email → %q, want empty", p)
	}
}

func TestResolvePersonPaths_EmptyInputs(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	if got := store.ResolvePersonPaths(nil); got != nil {
		t.Errorf("nil names → %v, want nil", got)
	}
	// No 인물 pages exist → every name is unresolved → nil.
	if got := store.ResolvePersonPaths([]string{"오선택"}); got != nil {
		t.Errorf("no pages → %v, want nil", got)
	}
}

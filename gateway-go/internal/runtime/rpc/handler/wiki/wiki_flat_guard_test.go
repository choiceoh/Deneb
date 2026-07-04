package wiki

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

// TestWrite_FlatProjectCreationNormalizesToSlot: the RPC write surface was one
// of the flat-creation bypasses — a blind client write of 프로젝트/<name>.md must
// land on the 대표.md layout slot (wiki-layout 불변식), extension present or not.
func TestWrite_FlatProjectCreationNormalizesToSlot(t *testing.T) {
	m, store := methodsWithStore(t)
	for _, spelled := range []string{"프로젝트/신규-태양광.md", "프로젝트/신규-이엔지"} {
		resp := callMethod(m, "wiki.write", map[string]any{
			"path":  spelled,
			"title": "신규 건",
			"body":  "본문",
		})
		mustOK(t, resp)
	}
	for _, name := range []string{"신규-태양광", "신규-이엔지"} {
		slot := wiki.RepPagePath(name)
		if _, err := store.ReadPage(slot); err != nil {
			t.Fatalf("flat create must land on %s: %v", slot, err)
		}
		if pg, _ := store.ReadPage("프로젝트/" + name + ".md"); pg != nil {
			t.Fatalf("flat page %s must not exist", name)
		}
	}
	// The response's path field reports the normalized target.
	resp := callMethod(m, "wiki.write", map[string]any{
		"path": "프로젝트/응답검증", "title": "응답", "body": "b",
	})
	mustOK(t, resp)
	if got := extractResult(t, resp)["path"]; got != wiki.RepPagePath("응답검증") {
		t.Fatalf("response path = %v, want the normalized slot", got)
	}
}

// TestWrite_LegacyFlatUpdateStaysInPlace: an EXISTING legacy flat 대표페이지
// keeps updating in place — only new creations normalize.
func TestWrite_LegacyFlatUpdateStaysInPlace(t *testing.T) {
	m, store := methodsWithStore(t)
	seedPage(t, store, "프로젝트/레거시.md", "레거시", "프로젝트", "기존 본문", nil)

	resp := callMethod(m, "wiki.write", map[string]any{
		"path":  "프로젝트/레거시.md",
		"title": "레거시",
		"body":  "갱신된 본문",
	})
	mustOK(t, resp)

	got, err := store.ReadPage("프로젝트/레거시.md")
	if err != nil || got.Body != "갱신된 본문" {
		t.Fatalf("legacy flat page must update in place: %v / %+v", err, got)
	}
	if pg, _ := store.ReadPage(wiki.RepPagePath("레거시")); pg != nil {
		t.Fatal("legacy flat update must not mint a 대표.md sibling")
	}
}

package wiki

import (
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestTier1Pages_ExcludesSupersededPages(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	current := NewPage("현재 사실", "사용자", nil)
	current.Meta.Importance = 0.9
	current.Body = "# 현재 사실\n\n새 값"
	if err := store.WritePage("사용자/current.md", current); err != nil {
		t.Fatal(err)
	}

	old := NewPage("폐기된 사실", "사용자", nil)
	old.Meta.Importance = 1
	old.Meta.SupersededBy = "사용자/current.md"
	old.Body = "# 폐기된 사실\n\n옛 값"
	if err := store.WritePage("사용자/old.md", old); err != nil {
		t.Fatal(err)
	}

	got := store.Tier1Pages(0.5)
	if len(got) != 1 || got[0].Path != "사용자/current.md" {
		t.Fatalf("Tier1Pages = %#v, want only current page", got)
	}
}

func TestTier1Pages_ExcludesGeneratedFactProjection(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	if _, err := store.UpsertFact(FactInput{
		Subject: "self", Key: "communication.language", Value: "한국어로 답변",
		Kind: FactKindPreference, Authority: FactAuthorityDirectUser,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadPage(factProfilePagePath); err != nil {
		t.Fatalf("generated fact page must remain directly readable: %v", err)
	}
	for _, result := range store.Tier1Pages(0.85) {
		if result.Path == factProfilePagePath {
			t.Fatalf("generated fact page duplicated into Tier1: %#v", result)
		}
	}
}

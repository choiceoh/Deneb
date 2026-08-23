package wiki

import (
	"os"
	"path/filepath"
	"testing"
)

// A profile-axis key keeps its own kind, whatever section the bullet sits under.
//
// The 2026-08-23 conflict: legacyFactKind derived the key from the text (via the
// axis table, where communication.response_length is a preference) but the kind
// from the Markdown heading ("## 사용자 모델" → identity). Two bullets then claimed
// different kinds for one identity, the fact store refused the second, and the
// cutover aborted — taking server init, and the gateway, down with it.
func TestLegacyFactKindFollowsTheAxisNotTheHeading(t *testing.T) {
	// Both bullets resolve to communication.response_length; only the second sits
	// under a heading that would otherwise make it an identity.
	if got := legacyFactKind("communication.response_length", "## 사용자 모델", "감정: 장황한 설명 대신 짧은 위로"); got != FactKindPreference {
		t.Fatalf("axis key was filed as %v under an identity heading", got)
	}
	if got := legacyFactKind("communication.answer_first", "## 상호 인식", "명시된 기대: 질문엔 즉답"); got != FactKindPreference {
		t.Fatalf("axis key was filed as %v under an identity heading", got)
	}
	// An identity axis stays identity even under a preference-ish heading.
	if got := legacyFactKind("identity.address", "## 선호", "나를 형이라고 불러줘"); got != FactKindIdentity {
		t.Fatalf("identity axis was downgraded to %v", got)
	}
	// Keys outside the axis table still fall back to the heading.
	if got := legacyFactKind("some.freeform", "## 사용자 모델", "탐구자 성향"); got != FactKindIdentity {
		t.Fatalf("non-axis key ignored its section: %v", got)
	}
}

// The two bullets that broke production now import instead of being skipped.
func TestConflictingHeadingBulletsImportTogether(t *testing.T) {
	store, _, _ := newFactTestStore(t)
	workspace := t.TempDir()
	legacy := "# MEMORY\n\n## 선호\n\n" +
		"- 답변은 간결하게(기본 3줄 내외), 장황한 설명 금지. 한국어 전용.\n" +
		"\n## 사용자 모델\n\n" +
		"- 감정: 피곤·기분 저하를 솔직히 표현 — 그럴 땐 장황한 설명 대신 가벼운 유머·짧은 위로가 효과적.\n"
	if err := os.WriteFile(filepath.Join(workspace, "MEMORY.md"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	imported, err := store.ImportLegacyFactFiles(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if skips := store.LegacyFactImportSkips(); len(skips) != 0 {
		t.Fatalf("bullets were skipped instead of imported: %v", skips)
	}
	if imported != 2 {
		t.Fatalf("imported=%d, want both bullets", imported)
	}
}

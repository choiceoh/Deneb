package compaction

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestGuidelineStore_SaveLoadCapDedup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "compaction-guidelines.json")
	store := NewGuidelineStore(path)

	// More than the cap, with blanks, whitespace dupes, and an over-long entry.
	long := strings.Repeat("가", maxGuidelineRunes+50)
	in := []string{
		"결제 금액과 기한을 보존하라",
		"  결제 금액과 기한을 보존하라  ", // dup after trim
		"", // dropped
		"담당자 변경 이력을 보존하라",
		long, // truncated
		"세 번째 규칙",
		"네 번째 규칙",
		"다섯 번째 규칙",
		"여섯 번째 규칙 — 캡 초과로 잘림",
	}
	if err := store.Save(in); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got := store.Load()
	if len(got) != MaxLearnedGuidelines {
		t.Fatalf("expected cap %d, got %d: %v", MaxLearnedGuidelines, len(got), got)
	}
	if got[0] != "결제 금액과 기한을 보존하라" || got[1] != "담당자 변경 이력을 보존하라" {
		t.Fatalf("dedup/order wrong: %v", got)
	}
	if r := []rune(got[2]); len(r) > maxGuidelineRunes {
		t.Fatalf("over-long entry not truncated: %d runes", len(r))
	}
	if strings.Contains(strings.Join(got, "|"), "여섯 번째") {
		t.Fatalf("entry past the cap leaked in: %v", got)
	}
}

func TestGuidelineStore_NilAndMissingSafe(t *testing.T) {
	if got := (*GuidelineStore)(nil).Load(); got != nil {
		t.Fatalf("nil store Load must be nil, got %v", got)
	}
	if err := (*GuidelineStore)(nil).Save([]string{"x"}); err != nil {
		t.Fatalf("nil store Save must be a no-op, got %v", err)
	}
	missing := NewGuidelineStore(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if got := missing.Load(); got != nil {
		t.Fatalf("missing file Load must be nil, got %v", got)
	}
}

func TestAugmentWithGuidelines(t *testing.T) {
	base := "BASE PROMPT"
	if got := augmentWithGuidelines(base, nil); got != base {
		t.Fatalf("no guidelines must return base unchanged, got %q", got)
	}
	got := augmentWithGuidelines(base, []string{"결제 금액 보존", "담당자 이력 보존"})
	if !strings.HasPrefix(got, base) {
		t.Fatalf("must preserve the base prompt prefix")
	}
	if !strings.Contains(got, "학습된 보존 지침") || !strings.Contains(got, "결제 금액 보존") || !strings.Contains(got, "담당자 이력 보존") {
		t.Fatalf("learned guidelines not rendered: %q", got)
	}
}

func TestCompactionPrompt_AppliesBoth(t *testing.T) {
	cfg := Config{AnchorKeywords: []string{"탑솔라"}, LearnedGuidelines: []string{"결제 기한 보존"}}
	got := compactionPrompt("BASE", cfg)
	if !strings.Contains(got, "Anchor") || !strings.Contains(got, "탑솔라") {
		t.Fatalf("anchors not applied: %q", got)
	}
	if !strings.Contains(got, "학습된 보존 지침") || !strings.Contains(got, "결제 기한 보존") {
		t.Fatalf("learned guidelines not applied: %q", got)
	}
}

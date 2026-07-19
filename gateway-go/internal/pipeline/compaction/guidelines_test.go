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
		"거래처명은 정식 상호로 보존하라",
		"발주 수량은 단위와 함께 보존하라",
		"계약 조건은 문구 그대로 보존하라",
		"여섯 번째 규칙 — 캡 초과로 잘림", // distinct concept, dropped by the cap
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

func TestSanitizeGuidelines_IgnoresPlaceholderJunk(t *testing.T) {
	// Live 2026-07-06: the tuner LLM echoed schema placeholders and they
	// persisted alongside real guidelines. Sanitize (Load AND Save path)
	// must drop them while keeping the real Korean directives.
	in := []string{
		"금액은 정확한 숫자와 통화로 보존하라",
		"guideline1",
		"guideline2",
		"Guideline 3",
		"규칙 1",
		"예시2",
		"rule_4",
		"placeholder text only", // no Hangul → schema noise
		"날짜는 구체적인 날짜로 보존하라",
	}
	got := sanitizeGuidelines(in)
	want := []string{
		"금액은 정확한 숫자와 통화로 보존하라",
		"날짜는 구체적인 날짜로 보존하라",
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("junk not filtered: %v", got)
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

func TestCompactionPrompt_RendersAnchorsAndGuidelines(t *testing.T) {
	cfg := Config{AnchorKeywords: []string{"탑솔라"}, LearnedGuidelines: []string{"결제 기한 보존"}}
	got := compactionPrompt("BASE", cfg)
	if !strings.Contains(got, "Anchor") || !strings.Contains(got, "탑솔라") {
		t.Fatalf("anchors not applied: %q", got)
	}
	if !strings.Contains(got, "학습된 보존 지침") || !strings.Contains(got, "결제 기한 보존") {
		t.Fatalf("learned guidelines not applied: %q", got)
	}
}

// The 2026-07-19 near-dup fix: reworded siblings of one rule must not each hold
// a slot, but genuinely distinct rules must survive.
func TestSanitizeGuidelines_MergesNearDuplicateConcepts(t *testing.T) {
	// The actual churned prod set (5 slots holding ~2 concepts).
	in := []string{
		"금액은 정확한 숫자와 통화로 보존하라",
		"단가, 예산 등 금액은 정확한 숫자와 단위를 보존하라", // ~dup of금액 → dropped
		"인물, 기업, 프로젝트명은 직책이나 호칭만 남기지 말고 보존하라",
		"인원은 정확한 직위와 성함으로 보존하라", // shares nothing with인물 rule → distinct
		"날짜는 구체적인 일자로 보존하라",
	}
	got := sanitizeGuidelines(in)
	if len(got) != 4 {
		t.Fatalf("want 4 distinct concepts after merge, got %d: %v", len(got), got)
	}
	// Newest-first: the first금액 phrasing wins, its sibling is gone.
	if got[0] != "금액은 정확한 숫자와 통화로 보존하라" {
		t.Fatalf("freshest phrasing must win: %v", got)
	}
	for _, g := range got {
		if strings.Contains(g, "단가") {
			t.Fatalf("near-dup sibling should have been dropped: %v", got)
		}
	}
	// The distinct rules all survive.
	for _, want := range []string{"인물", "인원", "날짜"} {
		found := false
		for _, g := range got {
			if strings.Contains(g, want) {
				found = true
			}
		}
		if !found {
			t.Fatalf("distinct rule %q dropped: %v", want, got)
		}
	}
}

func TestGuidelineOverlap_PrefixMatchAbsorbsParticles(t *testing.T) {
	// "금액" and "금액은" (particle) must count as the same token.
	a := guidelineContentTokens("금액은 숫자와 통화로 보존하라")
	b := guidelineContentTokens("금액 숫자 단위 보존하라")
	if ov := guidelineOverlap(a, b); ov < 0.5 {
		t.Fatalf("particle-variant tokens should overlap ≥0.5, got %.2f (a=%v b=%v)", ov, a, b)
	}
	// Unrelated rules overlap ~0.
	c := guidelineContentTokens("날짜는 구체적인 일자로 보존하라")
	if ov := guidelineOverlap(a, c); ov > 0 {
		t.Fatalf("unrelated rules must not overlap, got %.2f", ov)
	}
}

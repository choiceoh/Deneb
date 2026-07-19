package server

import (
	"testing"
	"time"
)

func kbDomains() []kbDomain {
	return []kbDomain{
		{Key: "a", Label: "A", Keywords: []string{"a"}},
		{Key: "b", Label: "B", Keywords: []string{"b"}},
		{Key: "c", Label: "C", Keywords: []string{"c"}},
	}
}

func TestPickKBInterviewSuggestion_MissingBeatsStaleAndCovered(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	fresh := now.Add(-10 * 24 * time.Hour).UnixMilli()
	stale := now.Add(-200 * 24 * time.Hour).UnixMilli()
	cov := []kbCoverage{
		{Domain: kbDomains()[0], PageCount: 3, NewestMs: fresh}, // covered+fresh: skip
		{Domain: kbDomains()[1], PageCount: 2, NewestMs: stale}, // stale
		{Domain: kbDomains()[2], PageCount: 0},                  // missing → wins
	}
	got := pickKBInterviewSuggestion(cov, now, nil)
	if got == nil || got.Domain.Key != "c" || got.Reason != "missing" {
		t.Fatalf("expected missing domain c, got %+v", got)
	}
}

func TestPickKBInterviewSuggestion_StaleWhenNothingMissing(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	fresh := now.Add(-10 * 24 * time.Hour).UnixMilli()
	staleOld := now.Add(-300 * 24 * time.Hour).UnixMilli()
	staleNewer := now.Add(-150 * 24 * time.Hour).UnixMilli()
	cov := []kbCoverage{
		{Domain: kbDomains()[0], PageCount: 1, NewestMs: fresh},      // fresh
		{Domain: kbDomains()[1], PageCount: 1, NewestMs: staleNewer}, // stale
		{Domain: kbDomains()[2], PageCount: 1, NewestMs: staleOld},   // stalest → wins
	}
	got := pickKBInterviewSuggestion(cov, now, nil)
	if got == nil || got.Domain.Key != "c" || got.Reason != "stale" {
		t.Fatalf("expected stalest domain c, got %+v", got)
	}
}

func TestPickKBInterviewSuggestion_CooldownSkipsRecentlySuggested(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	cov := []kbCoverage{
		{Domain: kbDomains()[0], PageCount: 0}, // missing but cooling down
		{Domain: kbDomains()[1], PageCount: 0}, // missing, eligible
	}
	last := map[string]int64{
		"a": now.Add(-3 * 24 * time.Hour).UnixMilli(),  // 3d < 14d cooldown
		"b": now.Add(-30 * 24 * time.Hour).UnixMilli(), // past cooldown
	}
	got := pickKBInterviewSuggestion(cov, now, last)
	if got == nil || got.Domain.Key != "b" {
		t.Fatalf("cooldown not honored: got %+v", got)
	}
}

func TestPickKBInterviewSuggestion_NothingWhenAllFreshOrCoolingDown(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	fresh := now.Add(-5 * 24 * time.Hour).UnixMilli()
	cov := []kbCoverage{
		{Domain: kbDomains()[0], PageCount: 2, NewestMs: fresh},
		{Domain: kbDomains()[1], PageCount: 0}, // missing but cooling down
	}
	last := map[string]int64{"b": now.Add(-1 * 24 * time.Hour).UnixMilli()}
	if got := pickKBInterviewSuggestion(cov, now, last); got != nil {
		t.Fatalf("expected no suggestion, got %+v", got)
	}
}

func TestPathMatchesDomain_SeparatorInsensitive(t *testing.T) {
	d := kbDomain{Keywords: []string{"시장 세분"}}
	// The real prod page uses hyphens between every word.
	if !pathMatchesDomain("업무/한국-태양광-시장-세분.md", d) {
		t.Fatal("hyphenated path must match spaced keyword")
	}
	if !pathMatchesDomain("업무/시장세분-2026.md", d) {
		t.Fatal("squashed keyword must match")
	}
	if !pathMatchesDomain("업무/시장 세분 노트.md", d) {
		t.Fatal("spaced keyword must match spaced path")
	}
	if pathMatchesDomain("프로젝트/무관.md", d) {
		t.Fatal("unrelated path must not match")
	}
}

func TestParseKBChecklist_ParsesBulletsAndSkipsProse(t *testing.T) {
	body := `# 지식 도메인 체크리스트

여기에 도메인을 관리합니다.

- market | 시장 세분 | 시장세분, 세그먼트
* rivals | 경쟁사 | 경쟁사, competitor
- 잘못된 줄 (파이프 없음)
- | 빈 키 | kw
`
	got := parseKBChecklist(body)
	if len(got) != 2 {
		t.Fatalf("expected 2 domains, got %d: %+v", len(got), got)
	}
	if got[0].Key != "market" || got[0].Label != "시장 세분" || len(got[0].Keywords) != 2 {
		t.Fatalf("first domain parsed wrong: %+v", got[0])
	}
	if got[1].Key != "rivals" {
		t.Fatalf("second domain parsed wrong: %+v", got[1])
	}
}

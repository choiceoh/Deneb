package genesis

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
)

// subtleBody has one imperative-rule line, one safety-note line, and one
// universal-quantifier line, each load-bearing and padded past the min-body
// floor, so every subtle/weaken degradation has a target.
func subtleBody() string {
	return strings.TrimSpace(`# 테스트 스킬

## When to Use
업무 보고서를 생성할 때. ` + strings.Repeat("구체적인 사용 조건 설명. ", 10) + `

## Procedure
1. 데이터를 수집한다. ` + strings.Repeat("절차 상세 설명. ", 8) + `
2. 보고서를 작성하기 전에 반드시 원본 데이터의 무결성을 재확인한다.
3. 주의: 미검증 첨부는 절대 자동 발송 경로에 넣지 않는다.
4. 발송 전 모든 수신자 주소를 명부와 대조해 확인한다.

## Verification
결과를 검증한다. ` + strings.Repeat("검증 상세. ", 10))
}

// Each subtle degradation must remove exactly its load-bearing line, keep the
// document structure, and change the body — otherwise it plants no defect.
func TestSubtleJudgeDegradations(t *testing.T) {
	body := subtleBody()

	imp, ok := degradeDropImperative(body)
	if !ok {
		t.Fatal("imperative-drop found no rule line to remove")
	}
	if strings.Contains(imp, "반드시 원본 데이터의 무결성") {
		t.Fatal("imperative-drop kept the imperative line")
	}
	if !strings.Contains(imp, "## Procedure") || !strings.Contains(imp, "## Verification") {
		t.Fatal("imperative-drop damaged document structure (headings lost)")
	}
	if imp == strings.TrimSpace(body) {
		t.Fatal("imperative-drop is a no-op")
	}

	safe, ok := degradeDropSafetyNote(body)
	if !ok {
		t.Fatal("safety-drop found no caution line to remove")
	}
	if strings.Contains(safe, "미검증 첨부는 절대") {
		t.Fatal("safety-drop kept the safety line")
	}
	if !strings.Contains(safe, "## Procedure") {
		t.Fatal("safety-drop damaged document structure")
	}
}

// dropFirstLineMatching skips headings and short lines, and reports ok=false
// when no substantive line carries a token.
func TestDropFirstLineMatching(t *testing.T) {
	// A heading that contains the token must NOT be dropped (that is
	// section-drop's job, and the honesty invariant excludes structure loss).
	headingOnly := "# 반드시 지켜라\n\n평범한 설명 문단입니다 이것은."
	if _, _, ok := dropFirstLineMatching(headingOnly, imperativeRuleTokens); ok {
		t.Fatal("dropped a heading line — must skip headings")
	}

	// A short line carrying the token is below the load-bearing threshold.
	shortLine := "본문 시작 문단.\n반드시.\n또 다른 문단 내용입니다 여기."
	if _, _, ok := dropFirstLineMatching(shortLine, imperativeRuleTokens); ok {
		t.Fatal("dropped a too-short line — must require substance")
	}

	// No token anywhere → ok=false.
	none := "그냥 평범한 절차 설명입니다.\n두 번째 줄도 평범합니다 정말로."
	if _, _, ok := dropFirstLineMatching(none, imperativeRuleTokens); ok {
		t.Fatal("reported a match where no token exists")
	}

	// A substantive body line carrying the token IS removed.
	hit := "도입 문단입니다 이것은 충분히 깁니다.\n작업 전에 반드시 백업을 먼저 완료한다.\n마무리 문단."
	mutated, removed, ok := dropFirstLineMatching(hit, imperativeRuleTokens)
	if !ok || !strings.Contains(removed, "반드시 백업") || strings.Contains(mutated, "반드시 백업") {
		t.Fatalf("substantive rule line not removed: ok=%v removed=%q", ok, removed)
	}
}

// Tier-3 weaken degradations mutate one token in place: the line survives
// (line count preserved — nothing for a diff to find missing), the binding
// force is diluted, and no other byte changes.
func TestWeakenJudgeDegradations(t *testing.T) {
	body := subtleBody()
	wantLines := strings.Count(body, "\n")

	imp, ok := degradeWeakenImperative(body)
	if !ok {
		t.Fatal("imperative-weaken found no rule line to dilute")
	}
	if strings.Count(imp, "\n") != wantLines {
		t.Fatal("imperative-weaken changed the line count — must mutate in place")
	}
	if strings.Contains(imp, "반드시") || !strings.Contains(imp, "가급적") {
		t.Fatalf("imperative-weaken did not dilute the hard-rule token: %q", imp)
	}
	if !strings.Contains(imp, "절대 자동 발송") {
		t.Fatal("imperative-weaken touched more than the first matching line")
	}

	nar, ok := degradeNarrowScope(body)
	if !ok {
		t.Fatal("scope-narrow found no quantifier line to shrink")
	}
	if strings.Count(nar, "\n") != wantLines {
		t.Fatal("scope-narrow changed the line count — must mutate in place")
	}
	if strings.Contains(nar, "모든 수신자") || !strings.Contains(nar, "일부 수신자") {
		t.Fatalf("scope-narrow did not shrink the quantifier: %q", nar)
	}

	// English tokens: a leading capital is preserved so the mutation stays
	// typographically clean, and trailing-space tokens dodge substring traps.
	english := "This intro line is long enough to pass.\nNever skip the raw-data verification step here.\nInstall the tooling for every checks run."
	weak, ok := weakenFirstLineMatching(english, imperativeWeakenSwaps)
	if !ok || !strings.Contains(weak, "Rarely skip") {
		t.Fatalf("English imperative-weaken failed: ok=%v %q", ok, weak)
	}
	if strings.Contains(weak, "rarely skip") {
		t.Fatal("leading capital lost on the replacement token")
	}
	if _, ok := weakenFirstLineMatching("Install the overall small tool here today.", scopeNarrowSwaps); ok {
		t.Fatal("substring trap: install/overall/small must not match quantifier tokens")
	}

	// No token anywhere → ok=false, never a fabricated pair.
	if _, ok := degradeWeakenImperative("그냥 평범한 절차 설명입니다 여기는.\n두 번째 줄도 평범합니다 정말로."); ok {
		t.Fatal("imperative-weaken reported a mutation with no token present")
	}
}

// Pair construction over a catalog: subtle pairs are built for the real body,
// stubs are skipped, the limit is honored, and construction is deterministic.
func TestBuildSubtleJudgeDegradationPairs(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) skills.SkillEntry {
		path := filepath.Join(dir, name+".md")
		full := "---\nname: " + name + "\nversion: 1.0.0\n---\n" + content
		if err := os.WriteFile(path, []byte(full), 0o644); err != nil {
			t.Fatal(err)
		}
		e := skills.SkillEntry{}
		e.Skill.Name = name
		e.Skill.FilePath = path
		return e
	}
	entries := []skills.SkillEntry{
		write("real", subtleBody()),
		write("stub", "# 짧은 스텁"),
	}

	pairs := buildSubtleJudgeDegradationPairs(entries, 10)
	if len(pairs) != 2 {
		t.Fatalf("pairs = %d, want 2 (imperative-drop + safety-drop of the real body)", len(pairs))
	}
	names := map[string]bool{}
	for _, p := range pairs {
		if p.Skill != "real" {
			t.Fatalf("stub body produced a pair: %+v", p)
		}
		if p.Degraded == p.Original {
			t.Fatalf("degradation %s is a no-op", p.Degradation)
		}
		names[p.Degradation] = true
	}
	if !names["imperative-drop"] || !names["safety-drop"] {
		t.Fatalf("missing a subtle class: %v", names)
	}
	if again := buildSubtleJudgeDegradationPairs(entries, 10); len(again) != len(pairs) || again[0].Degradation != pairs[0].Degradation {
		t.Fatal("subtle pair construction is not deterministic")
	}
	if capped := buildSubtleJudgeDegradationPairs(entries, 1); len(capped) != 1 {
		t.Fatalf("limit not honored: %d", len(capped))
	}

	// The escalated tier builds through the same constructor: both weaken
	// classes for the real body, none for the stub.
	weak := buildWeakenJudgeDegradationPairs(entries, 10)
	if len(weak) != 2 {
		t.Fatalf("weaken pairs = %d, want 2 (imperative-weaken + scope-narrow)", len(weak))
	}
	weakNames := map[string]bool{}
	for _, p := range weak {
		if p.Skill != "real" || p.Degraded == p.Original {
			t.Fatalf("bad weaken pair: %+v", p)
		}
		weakNames[p.Degradation] = true
	}
	if !weakNames["imperative-weaken"] || !weakNames["scope-narrow"] {
		t.Fatalf("missing a weaken class: %v", weakNames)
	}
}

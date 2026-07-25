package genesis

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/generation"
)

// The honesty invariant is what makes a miss a LABEL rather than noise: the
// mutant must be strictly weaker by definition, and it must still read as
// natural prose (otherwise the pair is easy and the tier buys nothing).
func TestDegradeDropExclusivityRemovesTheConstraintAndStaysGrammatical(t *testing.T) {
	tests := []struct {
		name, body, want string
	}{
		{
			name: "korean 때만",
			body: "승인이 끝났을 때만 배포한다. 그 전에는 대기.",
			want: "승인이 끝났을 때 배포한다. 그 전에는 대기.",
		},
		{
			name: "korean 경우에만",
			body: "테스트가 전부 통과한 경우에만 머지할 수 있다.",
			want: "테스트가 전부 통과한 경우에 머지할 수 있다.",
		},
		{
			name: "korean 오직",
			body: "오직 운영자만 이 값을 바꿀 수 있도록 유지한다.",
			want: "운영자만 이 값을 바꿀 수 있도록 유지한다.",
		},
		{
			name: "english only when",
			body: "Restart the gateway only when the drain has finished cleanly.",
			want: "Restart the gateway when the drain has finished cleanly.",
		},
		{
			name: "english only if",
			body: "Escalate to the operator only if the retry budget is exhausted.",
			want: "Escalate to the operator if the retry budget is exhausted.",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := degradeDropExclusivity(tc.body)
			if !ok {
				t.Fatalf("no mutation produced for %q", tc.body)
			}
			if got != tc.want {
				t.Fatalf("mutant = %q, want %q", got, tc.want)
			}
			if len(got) >= len(tc.body) {
				t.Errorf("exclusivity removal must SHRINK the line: %d >= %d", len(got), len(tc.body))
			}
		})
	}
}

// Bare "만" is a common Korean syllable. Matching it would mutate unrelated
// prose and file a pair whose "defect" is imaginary — the exact substring trap
// that already burned the negation guard once.
func TestDegradeDropExclusivityIgnoresIncidentalSyllables(t *testing.T) {
	safe := []string{
		"정말 중요한 규칙이므로 문서에 남겨 두어야 한다.",
		"만약 실패하면 폴백 경로로 내려가도록 구성한다.",
		"얼마만큼 재시도할지는 호출자가 결정하도록 남겨 둔다.",
		"보고서를 만들어서 운영자에게 전달하도록 구성한다.",
		"Commonly used tooling should stay installed on the host.",
	}
	for _, body := range safe {
		if got, ok := degradeDropExclusivity(body); ok {
			t.Errorf("must not mutate %q (got %q)", body, got)
		}
	}
}

// A no-op mutation would be filed as a pair whose "degraded" side equals the
// original — the judge cannot be wrong about it, so it is pure ledger noise.
func TestExclusivityTierSkipsBodiesWithoutAnExclusivityMarker(t *testing.T) {
	if _, ok := degradeDropExclusivity("배포 전에 게이트를 통과시킨다."); ok {
		t.Fatal("a body with no exclusivity marker must produce no pair")
	}
}

// The tier table is what the ladder gate keys on, so its shape is a contract.
func TestExclusivityTierTableIsRegistered(t *testing.T) {
	if len(exclusivityJudgeDegradations) == 0 {
		t.Fatal("tier-4 table must not be empty")
	}
	for _, d := range exclusivityJudgeDegradations {
		if strings.TrimSpace(d.name) == "" || d.apply == nil {
			t.Fatalf("malformed tier-4 entry: %+v", d)
		}
		// A tier class that rsi_status does not know about is invisible in the
		// L3 diagnosis, which is how a ceiling gets misreported as strength.
		if !rsiSubtleDegradationClasses[d.name] || !rsiWeakenDegradationClasses[d.name] {
			t.Errorf("tier-4 class %q must be registered in both rsi_status tier maps", d.name)
		}
	}
}

// The ladder must not skip a rung: tier 4 opens only once the incumbent has a
// saturated window of tier-3 (weaken) runs, and a tier-3 miss re-locks it.
// Without this, a judge that has merely outgrown the DROP tier would jump
// straight to the hardest probes and the ledger would fill with misses that
// say nothing about where the real frontier is.
func TestExclusivityTierOpensOnlyAfterWeakenTierSaturates(t *testing.T) {
	task, tr := accuracyFixture(t)
	version := task.Meta.Version(generation.MetaSkillJudgeSystemPrompt,
		generation.DefaultMetaArtifacts()[generation.MetaSkillJudgeSystemPrompt])
	seed := func(weakenMissed int) {
		t.Helper()
		if err := tr.logJudgeAccuracy(judgeAccuracyRecord{
			JudgeVersion: version, Pairs: 2, Correct: 2 - weakenMissed,
			ByClass: map[string][2]int{
				"imperative-weaken": {1 - weakenMissed, 1},
				"scope-narrow":      {1, 1},
			},
		}); err != nil {
			t.Fatal(err)
		}
	}

	if task.exclusivityTierUnlocked(version) {
		t.Fatal("tier 4 unlocked with no history")
	}
	for i := 0; i < judgeEscalationWindow-1; i++ {
		seed(0)
	}
	if task.exclusivityTierUnlocked(version) {
		t.Fatal("tier 4 unlocked below the saturation window")
	}
	seed(0)
	if !task.exclusivityTierUnlocked(version) {
		t.Fatal("tier 4 locked after a fully saturated weaken window")
	}
	// A tier-3 miss means the frontier is still at tier 3 — hold there.
	seed(1)
	if task.exclusivityTierUnlocked(version) {
		t.Fatal("tier 4 stayed unlocked past a weaken-tier miss")
	}
	// Version scoping mirrors the tier-3 gate: a revised judge re-earns it.
	if task.exclusivityTierUnlocked("revised-judge") {
		t.Fatal("tier 4 unlocked for a judge version with no history")
	}
}

// Drop-tier-only history must not unlock tier 4: those runs carry no weaken
// pairs at all, and "no pairs seen" is not evidence of saturation.
func TestExclusivityTierIgnoresDropTierOnlyHistory(t *testing.T) {
	task, tr := accuracyFixture(t)
	version := task.Meta.Version(generation.MetaSkillJudgeSystemPrompt,
		generation.DefaultMetaArtifacts()[generation.MetaSkillJudgeSystemPrompt])
	for i := 0; i < judgeEscalationWindow*2; i++ {
		if err := tr.logJudgeAccuracy(judgeAccuracyRecord{
			JudgeVersion: version, Pairs: 2, Correct: 2,
			ByClass: map[string][2]int{
				"imperative-drop": {1, 1},
				"safety-drop":     {1, 1},
			},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if !task.weakenTierUnlocked(version) {
		t.Fatal("fixture precondition: drop tier should be saturated")
	}
	if task.exclusivityTierUnlocked(version) {
		t.Fatal("tier 4 unlocked from drop-tier-only history (no weaken pairs seen)")
	}
}

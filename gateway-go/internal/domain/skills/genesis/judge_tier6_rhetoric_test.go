package genesis

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/generation"
)

// Tier 6's defining property: the mutant asserts MORE than the original, and
// the procedure itself is untouched. Every rung below it shrinks or weakens the
// body, so a judge scoring 100% on all of them has only been shown one
// direction of error (2608.08975: rhetoric moves LLM reviewers upward with the
// content held fixed).
//
// As with tier 5, the refusal side is pinned as tightly as the mutation side —
// a probe the judge is RIGHT to pass is a broken probe, not a hard one.

const hedgedSkillBody = `# 배포

배포는 보통 스테이징을 먼저 거친다.
게이트가 그린이면 진행한다.
`

func TestInflateHedgeUpgradesTheQualifierAndNothingElse(t *testing.T) {
	got, ok := degradeInflateHedge(hedgedSkillBody)
	if !ok {
		t.Fatal("a hedged claim must be inflatable")
	}
	if !strings.Contains(got, "배포는 항상 스테이징을 먼저 거친다.") {
		t.Errorf("hedge must become an absolute claim:\n%s", got)
	}
	if !strings.Contains(got, "게이트가 그린이면 진행한다.") {
		t.Errorf("untouched lines must survive verbatim:\n%s", got)
	}
	if strings.Contains(got, "보통") {
		t.Errorf("the original hedge must be gone:\n%s", got)
	}
}

// The inflation classes must not be catchable by "did it get shorter?", which
// is the only question tiers 1-4 ask.
func TestRhetoricMutantsNeverShrinkTheBody(t *testing.T) {
	for _, d := range rhetoricJudgeDegradations {
		got, ok := d.apply(hedgedSkillBody)
		if !ok {
			t.Errorf("%s: refused a body it should mutate", d.name)
			continue
		}
		if len(got) < len(hedgedSkillBody) {
			t.Errorf("%s: mutant shorter than original (%d < %d) — catchable as deletion",
				d.name, len(got), len(hedgedSkillBody))
		}
		if !strings.Contains(got, "게이트가 그린이면 진행한다.") {
			t.Errorf("%s: dropped an original line:\n%s", d.name, got)
		}
	}
}

func TestFabricatedEvidenceAndNoveltyClaimsAreAdditive(t *testing.T) {
	evid, _ := degradeFabricateEvidence(hedgedSkillBody)
	if !strings.HasPrefix(evid, hedgedSkillBody) {
		t.Error("evidence fabrication must append, leaving the procedure intact")
	}
	if !strings.Contains(evid, "검증") {
		t.Errorf("evidence claim missing:\n%s", evid)
	}

	nov, _ := degradeNoveltySuperiority(hedgedSkillBody)
	if !strings.HasPrefix(nov, hedgedSkillBody) {
		t.Error("novelty stance must append, leaving the procedure intact")
	}
	if !strings.Contains(nov, "검토할 필요가 없다") {
		t.Errorf("alternatives must be foreclosed for the probe to be a defect:\n%s", nov)
	}
}

// A body with no hedge has nothing to inflate; inventing one would change the
// procedure, which is the one thing this tier must not do.
func TestInflateHedgeRefusesUnhedgedBody(t *testing.T) {
	body := "# 배포\n\n게이트가 그린이면 배포한다.\n로그를 확인한다.\n"
	if _, ok := degradeInflateHedge(body); ok {
		t.Error("unhedged body must not be inflated")
	}
}

// Headings and fragments are not rule-bearing prose (same exclusion the tier-3
// probes make), so a hedge inside one must not be the probe target.
func TestInflateHedgeSkipsHeadingsAndFragments(t *testing.T) {
	body := "# 보통의 배포\n\n짧다\n게이트가 그린이면 배포한다.\n"
	if got, ok := degradeInflateHedge(body); ok {
		t.Errorf("heading hedge must be skipped, got:\n%s", got)
	}
}

// The rung must be distinct from every rung below it: a shared class name would
// make ByClass counts — and therefore the saturation tests that gate escalation
// — silently merge two different failure modes.
func TestRhetoricClassNamesAreUniqueAcrossTiers(t *testing.T) {
	seen := map[string]string{}
	tiers := map[string][]namedDegradation{
		"blatant":     blatantJudgeDegradations,
		"subtle":      subtleJudgeDegradations,
		"weaken":      weakenJudgeDegradations,
		"exclusivity": exclusivityJudgeDegradations,
		"reorder":     reorderJudgeDegradations,
		"rhetoric":    rhetoricJudgeDegradations,
	}
	for tier, classes := range tiers {
		for _, c := range classes {
			if prev, dup := seen[c.name]; dup {
				t.Errorf("class %q declared in both %s and %s", c.name, prev, tier)
			}
			seen[c.name] = tier
		}
	}
}

// The rung must actually deploy: once tier 5 saturates, the lane's pair set has
// to carry rhetoric classes. Without this, adding a table changes nothing —
// the ladder simply never reaches it.
func TestRhetoricTierUnlocksAfterReorderSaturation(t *testing.T) {
	task, tr := accuracyFixture(t)
	addAccuracySkill(t, task, "sk2")
	version := task.Meta.Version(generation.MetaSkillJudgeSystemPrompt,
		generation.DefaultMetaArtifacts()[generation.MetaSkillJudgeSystemPrompt])

	if task.rhetoricTierUnlocked(version) {
		t.Fatal("tier 6 must stay locked before tier 5 saturates")
	}
	for i := 0; i < judgeEscalationWindow; i++ {
		if err := tr.logJudgeAccuracy(judgeAccuracyRecord{
			JudgeVersion: version, Pairs: 2, Correct: 2,
			ByClass: map[string][2]int{
				"step-reorder":          {1, 1},
				"contradiction-example": {1, 1},
			},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if !task.rhetoricTierUnlocked(version) {
		t.Fatal("tier 6 must unlock once tier 5 is outgrown")
	}
	if task.probeCeilingSaturated(version) {
		t.Fatal("ceiling must not read as saturated while tier 6 is unscored")
	}
}

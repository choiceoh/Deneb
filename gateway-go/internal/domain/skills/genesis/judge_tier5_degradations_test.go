package genesis

import (
	"strings"
	"testing"
)

// Tier 5's defining property: the mutant is never shorter. Tiers 1-4 all shrink
// the body, which let the incumbent judge score 100% on all nine classes by
// noticing deletions alone (65 runs, zero misses, 2026-08-01).
//
// The second property matters just as much: a mutant the judge is RIGHT to pass
// is a broken probe, not a hard one. So these pin refusal as tightly as they pin
// mutation — separate tests, since one loop would hide failures behind the first.

const orderedSkillBody = `# Deploy

1. 게이트를 돌려 그린인지 확인한다
2. 그 다음 스테이징에 배포한다
3. 로그를 본다
`

func TestReorderSwapsOnlyDependentSteps(t *testing.T) {
	got, ok := degradeReorderSteps(orderedSkillBody)
	if !ok {
		t.Fatal("a step declaring it follows the previous one must be reorderable")
	}
	if !strings.Contains(got, "1. 그 다음 스테이징에 배포한다") ||
		!strings.Contains(got, "2. 게이트를 돌려 그린인지 확인한다") {
		t.Errorf("bodies must swap while the numbering stays put:\n%s", got)
	}
}

func TestReorderKeepsEveryByteOfBothSteps(t *testing.T) {
	// Nothing removed is the whole point — a completeness check must pass it.
	got, ok := degradeReorderSteps(orderedSkillBody)
	if !ok {
		t.Fatal("expected a swap")
	}
	if len(got) != len(orderedSkillBody) {
		t.Errorf("reorder must preserve length: %d vs %d", len(got), len(orderedSkillBody))
	}
	for _, frag := range []string{"게이트를 돌려 그린인지 확인한다", "그 다음 스테이징에 배포한다", "로그를 본다"} {
		if !strings.Contains(got, frag) {
			t.Errorf("lost %q — reorder must not delete:\n%s", frag, got)
		}
	}
}

func TestReorderRefusesIndependentSteps(t *testing.T) {
	// No back-reference: swapping these may yield an equally valid procedure,
	// so the judge would be right to pass it. Refuse rather than ship a probe
	// with no correct answer.
	body := "1. 로그를 본다\n2. 메트릭을 본다\n"
	if _, ok := degradeReorderSteps(body); ok {
		t.Error("independent steps must not be reordered")
	}
}

func TestReorderRefusesUnorderedList(t *testing.T) {
	// A bullet list asserts no sequence, so there is none to violate.
	body := "- 게이트를 돌린다\n- 그 다음 배포한다\n"
	if _, ok := degradeReorderSteps(body); ok {
		t.Error("bullets carry no order to break")
	}
}

func TestContradictionOnlyFiresOnAProhibition(t *testing.T) {
	body := "# Deploy\n\n실패한 빌드로 푸시하지 마라.\n"
	got, ok := degradeContradictExample(body)
	if !ok {
		t.Fatal("a stated prohibition must be contradictable")
	}
	if len(got) <= len(body) {
		t.Errorf("contradiction must ADD text, got %d vs %d", len(got), len(body))
	}
	if !strings.Contains(got, "실패한 빌드로 푸시하지 마라.") {
		t.Error("the original rule must survive — the defect is the contradiction, not a deletion")
	}
}

func TestContradictionRefusesBodyWithoutRules(t *testing.T) {
	if _, ok := degradeContradictExample("# Notes\n\n배포는 보통 오전에 한다.\n"); ok {
		t.Error("no prohibition means nothing to contradict")
	}
}

func TestTier5ClassesAreRegisteredForTheLadder(t *testing.T) {
	// The escalation gate keys saturation on the table, so a class missing here
	// would never be scored and the rung would silently do nothing.
	if len(reorderJudgeDegradations) != 2 {
		t.Fatalf("expected 2 tier-5 classes, got %d", len(reorderJudgeDegradations))
	}
	for _, d := range reorderJudgeDegradations {
		if !rsiSubtleDegradationClasses[d.name] {
			t.Errorf("%q must count as label-producing or L3 reads DATA-GATED at the new ceiling", d.name)
		}
	}
}

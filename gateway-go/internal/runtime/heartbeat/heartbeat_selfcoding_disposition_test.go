package heartbeat

import (
	"strings"
	"testing"
	"time"
)

func nowForNudge() time.Time { return time.Now() }

// The 2026-08-01 ledger audit found 22 candidates shelved into silence because
// the nudge itself defined accepted as "valid, follow-up needed" and made
// evicting the queue the goal. These pin the corrected contract so the wording
// cannot drift back — each is a separate test because a single loop would stop
// at the first failure and hide the rest.
func TestNudgeScopesAcceptedToDispatchableCandidates(t *testing.T) {
	nudge := selfCodingTask(t, 2, "2:sc-a:100").detectSelfCodingNudge(nowForNudge())
	if !strings.Contains(nudge, "consumer=coding-dispatch 인 후보에만") {
		t.Errorf("accepted must be scoped to candidates a consumer will claim:\n%s", nudge)
	}
}

func TestNudgeWarnsThatConsumerlessAcceptedDisappears(t *testing.T) {
	nudge := selfCodingTask(t, 2, "2:sc-a:100").detectSelfCodingNudge(nowForNudge())
	if !strings.Contains(nudge, "consumer=none") {
		t.Errorf("the reviewer must be told which candidates nothing will claim:\n%s", nudge)
	}
}

func TestNudgeOffersLeavingProposedInsteadOfForcingAVerdict(t *testing.T) {
	// A mandatory verdict with no honest "cannot act" option manufactures
	// rubber-stamps; leaving it proposed and reporting is the honest path.
	nudge := selfCodingTask(t, 2, "2:sc-a:100").detectSelfCodingNudge(nowForNudge())
	if !strings.Contains(nudge, "판정하지 말고 proposed로 두세요") {
		t.Errorf("nudge must permit an honest unresolved outcome:\n%s", nudge)
	}
}

func TestNudgeNamesSupersededForDuplicateClusters(t *testing.T) {
	// Duplicates of one root cause were each accepted separately because the
	// nudge never mentioned superseded.
	nudge := selfCodingTask(t, 2, "2:sc-a:100").detectSelfCodingNudge(nowForNudge())
	if !strings.Contains(nudge, "superseded") {
		t.Errorf("nudge must name superseded for same-root-cause derivatives:\n%s", nudge)
	}
}

func TestNudgeDoesNotFrameQueueEvictionAsTheGoal(t *testing.T) {
	nudge := selfCodingTask(t, 2, "2:sc-a:100").detectSelfCodingNudge(nowForNudge())
	if strings.Contains(nudge, "'제안됨'에서 내보내세요") {
		t.Errorf("evicting the queue must not be stated as the objective:\n%s", nudge)
	}
}

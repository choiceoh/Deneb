package heartbeat

import (
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
)

func sweepTask(t *testing.T, proposed int, funnel genesis.SelfCorrectionFunnelSummary, recurrences int) *heartbeatTask {
	t.Helper()
	return &heartbeatTask{
		homeDir: t.TempDir(),
		logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		proposedSelfCoding: func() (int, string) {
			return proposed, ""
		},
		selfImproveSignals: func() (genesis.SelfCorrectionFunnelSummary, int) {
			return funnel, recurrences
		},
	}
}

// The evidence bundle leads the nudge: clusters render as one bullet each and
// the instructions pivot to cluster-first mining. A nil evidence closure (or an
// empty bundle) falls back to the counter-only nudge.
func TestBuildSelfImproveSweepNudgeEvidenceBundleWithFallback(t *testing.T) {
	task := sweepTask(t, 0, genesis.SelfCorrectionFunnelSummary{Rejections7d: 4, PromotableRejections7d: 2}, 1)
	task.selfImproveEvidence = func(limit int) []genesis.FailureClusterSummary {
		if limit != sweepEvidenceClusterLimit {
			t.Errorf("sweep should request the nudge-sized bundle, got limit=%d", limit)
		}
		return []genesis.FailureClusterSummary{
			{
				Kind: genesis.FailureClusterKindUsage, Skill: "contract-review",
				Model:     "m2.5",
				Signature: "terminal=missing-artifact|mechanism=artifact-recovery",
				Support:   3, LastAt: time.Date(2026, 7, 8, 12, 0, 0, 0, time.Local).UnixMilli(),
				Example: "tool=bash; error=required artifact missing",
				Route: genesis.FailureInterventionRoute{
					Mode: genesis.FailureRouteModeShadow, FailureOrigin: genesis.FailureOriginInstruction,
					InterventionSurface: genesis.InterventionSurfaceSkill, Confidence: genesis.FailureRouteConfidenceMedium,
				},
			},
			{
				Kind: genesis.FailureClusterKindRejection, Skill: "contract-review",
				Signature: "surface-mismatch", Support: 2,
			},
		}
	}

	nudge := task.detectSelfImproveSweepNudge(time.Now())
	if nudge == "" {
		t.Fatal("sweep with signals should fire")
	}
	for _, want := range []string{
		"실패 클러스터",
		"shadow-route는 참고 분류일 뿐 배차·수정 권한이 아니므로",
		"[usage-failure] contract-review · terminal=missing-artifact|mechanism=artifact-recovery · 3건 · model=m2.5 · shadow-route=instruction→skill(medium) · 최근 07-08 · 예: \"tool=bash; error=required artifact missing\"",
		"[evolve-rejection] contract-review · surface-mismatch · 2건",
		"클러스터의 시그니처에서 시작",
		"클러스터 시그니처·지지도를 인용",
	} {
		if !strings.Contains(nudge, want) {
			t.Errorf("evidence nudge missing %q:\n%s", want, nudge)
		}
	}

	// Empty bundle → counter-only nudge without a dangling clusters header.
	task2 := sweepTask(t, 0, genesis.SelfCorrectionFunnelSummary{Rejections7d: 1}, 0)
	task2.selfImproveEvidence = func(int) []genesis.FailureClusterSummary { return nil }
	nudge2 := task2.detectSelfImproveSweepNudge(time.Now())
	if nudge2 == "" {
		t.Fatal("sweep with signals should fire without evidence too")
	}
	if strings.Contains(nudge2, "실패 클러스터") || strings.Contains(nudge2, "클러스터의 시그니처에서 시작") {
		t.Errorf("empty bundle must fall back to the counter-only nudge:\n%s", nudge2)
	}
}

// An empty queue with fresh signals fires the sweep; the interval throttles
// re-fires; past the interval it fires again while signals persist.
func TestDetectSelfImproveSweepFiresThenExpiresThrottle(t *testing.T) {
	task := sweepTask(t, 0, genesis.SelfCorrectionFunnelSummary{
		Rejections7d:           3,
		PromotableRejections7d: 1,
	}, 2)
	now := time.Now()

	nudge := task.detectSelfImproveSweepNudge(now)
	if nudge == "" {
		t.Fatal("empty queue with signals should fire the sweep")
	}
	for _, want := range []string{"자가개선 스윕", "거절 3건", "승격자격 1", "재발 2건", "self_correction", "NO_REPLY"} {
		if !strings.Contains(nudge, want) {
			t.Errorf("sweep nudge missing %q:\n%s", want, nudge)
		}
	}

	// 30 minutes later: throttled by the interval.
	if got := task.detectSelfImproveSweepNudge(now.Add(30 * time.Minute)); got != "" {
		t.Fatalf("sweep should be throttled inside the interval, got %q", got)
	}

	// Past the interval with signals still present: fires again.
	if got := task.detectSelfImproveSweepNudge(now.Add(selfImproveSweepMinInterval + time.Minute)); got == "" {
		t.Fatal("sweep should re-fire past the interval while signals persist")
	}
}

// Two consecutive nudges with zero queue movement escalate the third fire to a
// mandatory operator report; queue activity between fires marks a yield (the
// nudge — or any capture path — fed the queue), resetting the streak.
func TestDetectSelfImproveSweep_EscalatesAfterIgnoredNudges(t *testing.T) {
	proposed := 0
	task := &heartbeatTask{
		homeDir:            t.TempDir(),
		logger:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		proposedSelfCoding: func() (int, string) { return proposed, "" },
		selfImproveSignals: func() (genesis.SelfCorrectionFunnelSummary, int) {
			return genesis.SelfCorrectionFunnelSummary{Rejections7d: 3, PromotableRejections7d: 1}, 0
		},
	}
	now := time.Now()
	step := selfImproveSweepMinInterval + time.Minute

	first := task.detectSelfImproveSweepNudge(now)
	second := task.detectSelfImproveSweepNudge(now.Add(step))
	third := task.detectSelfImproveSweepNudge(now.Add(2 * step))
	if first == "" || second == "" || third == "" {
		t.Fatal("all three sweeps should fire while signals persist")
	}
	if strings.Contains(first, "★에스컬레이션") || strings.Contains(second, "★에스컬레이션") {
		t.Error("escalation must not appear before the ignored threshold")
	}
	if !strings.Contains(third, "★에스컬레이션") || !strings.Contains(third, "2회 연속") {
		t.Errorf("third consecutive ignored nudge should escalate:\n%s", third)
	}

	// Queue activity while the nudge is outstanding marks a yield: the next
	// fire is a fresh start, not a deeper streak.
	proposed = 1
	if got := task.detectSelfImproveSweepNudge(now.Add(2*step + time.Hour)); got != "" {
		t.Fatalf("busy queue must not sweep, got %q", got)
	}
	proposed = 0
	fourth := task.detectSelfImproveSweepNudge(now.Add(3 * step))
	if fourth == "" {
		t.Fatal("sweep should fire again once the queue drains")
	}
	if strings.Contains(fourth, "★에스컬레이션") {
		t.Errorf("yield must reset the ignored streak:\n%s", fourth)
	}
}

func TestDetectSelfImproveSweepBusyEmptyAndNilStayQuiet(t *testing.T) {
	// Queue not empty → the review lane owns the tick.
	busy := sweepTask(t, 2, genesis.SelfCorrectionFunnelSummary{Rejections7d: 3}, 1)
	if got := busy.detectSelfImproveSweepNudge(time.Now()); got != "" {
		t.Fatalf("non-empty queue must not sweep, got %q", got)
	}

	// No signals in the window → nothing to mine.
	quiet := sweepTask(t, 0, genesis.SelfCorrectionFunnelSummary{}, 0)
	if got := quiet.detectSelfImproveSweepNudge(time.Now()); got != "" {
		t.Fatalf("zero signals must not sweep, got %q", got)
	}

	// Lane unwired (tracker absent) → disabled.
	bare := &heartbeatTask{homeDir: t.TempDir(), logger: slog.Default()}
	if got := bare.detectSelfImproveSweepNudge(time.Now()); got != "" {
		t.Fatalf("nil signals should disable the lane, got %q", got)
	}
}

// Accepted (not proposed) dispatchable code candidates are still a consumer
// backlog — the generator sweep must stay quiet while coding-dispatch drains them.
func TestDetectSelfImproveSweep_SuppressedWhenAcceptedDispatchBacklog(t *testing.T) {
	task := sweepTask(t, 0, genesis.SelfCorrectionFunnelSummary{Rejections7d: 5}, 2)
	task.dispatchBacklogSelfCoding = func() int { return 7 }
	if got := task.detectSelfImproveSweepNudge(time.Now()); got != "" {
		t.Fatalf("accepted dispatch backlog must suppress sweep, got %q", got)
	}
}

// A future marker (clock skew, corrupted state) must not mute the lane.
func TestDetectSelfImproveSweep_ClockSkewRecovers(t *testing.T) {
	task := sweepTask(t, 0, genesis.SelfCorrectionFunnelSummary{Rejections7d: 1}, 0)
	now := time.Now()
	if err := saveSelfImproveSweepState(task.selfImproveSweepStatePath(), selfImproveSweepState{
		LastNudgeAt: now.Add(24 * time.Hour),
	}); err != nil {
		t.Fatalf("seed state: %v", err)
	}
	if got := task.detectSelfImproveSweepNudge(now); got == "" {
		t.Fatal("future marker should be treated as unset, not mute the lane")
	}
}

// Workout evidence alone (no rejections/recurrences — they're quarantined
// counters) must wake the sweep, or the synthetic exercise lane piles up
// clusters nothing consumes.
func TestDetectSelfImproveSweepWhenOnlyWorkoutEvidence(t *testing.T) {
	task := sweepTask(t, 0, genesis.SelfCorrectionFunnelSummary{}, 0)
	task.selfImproveEvidence = func(int) []genesis.FailureClusterSummary {
		return []genesis.FailureClusterSummary{{
			Kind: genesis.FailureClusterKindWorkout, Skill: "contract-review",
			Signature: "terminal=heldout-assertion|mechanism=skill-behavior-drift", Support: 2,
		}}
	}
	nudge := task.detectSelfImproveSweepNudge(time.Now())
	if nudge == "" {
		t.Fatal("workout-only evidence must fire the sweep")
	}
	if !strings.Contains(nudge, "workout-failure") {
		t.Fatalf("nudge should carry the workout cluster:\n%s", nudge)
	}
}

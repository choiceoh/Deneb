package server

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

// An empty queue with fresh signals fires the sweep; the interval throttles
// re-fires; past the interval it fires again while signals persist.
func TestDetectSelfImproveSweep_FireThrottleRefire(t *testing.T) {
	task := sweepTask(t, 0, genesis.SelfCorrectionFunnelSummary{
		Rejections7d:           3,
		PromotableRejections7d: 1,
	}, 2)
	now := time.Now()

	nudge := task.detectSelfImproveSweepNudge(now)
	if nudge == "" {
		t.Fatal("empty queue with signals should fire the sweep")
	}
	for _, want := range []string{"자가개선 스윕", "거절 3건", "승격자격 1", "재발 2건", "self_correction_propose", "NO_REPLY"} {
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

func TestDetectSelfImproveSweep_QuietPaths(t *testing.T) {
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

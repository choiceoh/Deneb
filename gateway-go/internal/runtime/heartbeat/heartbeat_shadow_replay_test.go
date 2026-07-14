package heartbeat

import (
	"context"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

// seedShadowFixtures writes an alternating quiet/actionable corpus: quiet
// fixtures really answered NO_REPLY, actionable ones really reported.
func seedShadowFixtures(t *testing.T, path string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		f := heartbeatFixture{
			FiredAt:     int64(i + 1),
			HeartbeatMD: "## Active Tasks\n- watch builds",
			OutcomeText: "빌드 상태 보고: srv1 그린",
		}
		if i%2 == 0 {
			f.OutcomeText = "NO_REPLY"
		} else {
			f.SweepNudge = "[자가개선 스윕] 후보를 발굴하세요"
		}
		if err := jsonlstore.Append(path, f); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
}

// A candidate that keeps quiet fixtures quiet and fixes an original failure
// accepts; one that goes silent on actionable fixtures rejects; a too-small
// corpus refuses to judge.
func TestRunHeartbeatShadowReplayAcceptRejectOrInsufficientVerdict(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/fixtures.jsonl"
	seedShadowFixtures(t, path, 8)

	// Executor: the original contract leaks an internal token on actionable
	// fixtures (a real failure mode); the candidate behaves. Quiet fixtures
	// stay NO_REPLY under both.
	executor := func(_ context.Context, _ string, user string) (string, error) {
		quiet := !strings.Contains(user, "자가개선 스윕")
		if quiet {
			return "NO_REPLY", nil
		}
		if strings.Contains(user, "candidate-marker") {
			return "스윕 후보 2건 제안 완료", nil
		}
		return "제안 완료 <function=skill_lifecycle>", nil
	}

	report, err := runHeartbeatShadowReplay(context.Background(), path, "## Active Tasks\n- watch builds\ncandidate-marker", 0, executor)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if !report.OK || report.Verdict != "accept" || !report.DryRun {
		t.Fatalf("improving candidate should accept (dry-run), got %+v", report)
	}
	if report.HeldInTotal+report.HeldOutTotal != 8 || len(report.Results) != 8 {
		t.Fatalf("all 8 fixtures should be scored: %+v", report)
	}
	if report.HeldInCandidate < report.HeldInOriginal || report.HeldOutCandidate < report.HeldOutOriginal {
		t.Fatalf("candidate should not regress either split: %+v", report)
	}

	// Candidate that silences actionable fixtures → reject with reason.
	muter := func(_ context.Context, _ string, user string) (string, error) {
		if strings.Contains(user, "candidate-marker") {
			return "NO_REPLY", nil
		}
		if !strings.Contains(user, "자가개선 스윕") {
			return "NO_REPLY", nil
		}
		return "정상 보고", nil
	}
	report, err = runHeartbeatShadowReplay(context.Background(), path, "## Active Tasks\ncandidate-marker", 0, muter)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if report.OK || report.Verdict != "reject" || !strings.Contains(report.Reason, "no-trade-off") {
		t.Fatalf("silencing candidate must reject via no-trade-off rule, got %+v", report)
	}

	// Insufficient corpus refuses to judge.
	small := dir + "/small.jsonl"
	seedShadowFixtures(t, small, heartbeatShadowMinFixtures-1)
	report, err = runHeartbeatShadowReplay(context.Background(), small, "x", 0, executor)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if report.Verdict != "insufficient-corpus" {
		t.Fatalf("small corpus must refuse judgment, got %+v", report)
	}
}

// Error-outcome fixtures are excluded from the corpus, and identical
// performance on both sides rejects for lack of improvement.
func TestRunHeartbeatShadowReplayExcludesErrorsAndRejectsNoImprovement(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/fixtures.jsonl"
	seedShadowFixtures(t, path, 6)
	for i := 0; i < 3; i++ {
		if err := jsonlstore.Append(path, heartbeatFixture{FiredAt: int64(100 + i), OutcomeErr: "turn failed"}); err != nil {
			t.Fatalf("seed err fixture: %v", err)
		}
	}

	perfect := func(_ context.Context, _ string, user string) (string, error) {
		if !strings.Contains(user, "자가개선 스윕") {
			return "NO_REPLY", nil
		}
		return "보고", nil
	}
	report, err := runHeartbeatShadowReplay(context.Background(), path, "candidate", 0, perfect)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if report.Fixtures != 6 {
		t.Fatalf("error-outcome fixtures must be excluded: %+v", report)
	}
	if report.OK || !strings.Contains(report.Reason, "no measurable improvement") {
		t.Fatalf("identical performance must reject, got %+v", report)
	}
}

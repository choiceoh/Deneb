package genesis

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
)

// watchTaskForJudge builds a task over an ISOLATED tracker. HOME and
// DENEB_STATE_DIR are both redirected because NewTracker guards against
// resolving the live state dir — test rows in the production ledger once
// tripped the self-brake and cost three manual freeze clearings.
func watchTaskForJudge(t *testing.T) (*SelfCorrectionWatchTask, *Tracker) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	quiet := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	tr, err := NewTracker(quiet)
	if err != nil {
		t.Skipf("tracker unavailable: %v", err)
	}
	return &SelfCorrectionWatchTask{Tracker: tr, Logger: quiet}, tr
}

// TestJudgeFallsThroughToTheCardOnEveryNonVerdict is the safety property.
//
// Three ways of not getting a verdict — no judge, a call error, an explicit
// abstain — must all reach the operator. A judge outage has to degrade to the
// old behavior, never to a silent swallow, or the inbox loses candidates in a
// way nothing surfaces.
func TestJudgeFallsThroughToTheCardOnEveryNonVerdict(t *testing.T) {
	task, _ := watchTaskForJudge(t)
	rec := SelfCorrectionCandidateRecord{ID: "sc-1", Title: "t"}
	logger := task.Logger

	// 1. No judge wired.
	if task.judged(context.Background(), rec, logger) {
		t.Error("a nil judge must not settle the candidate")
	}
	// 2. Judge call fails.
	task.Judge = func(context.Context, SelfCorrectionCandidateRecord) (SelfCorrectionVerdict, error) {
		return SelfCorrectionVerdict{}, errors.New("model unreachable")
	}
	if task.judged(context.Background(), rec, logger) {
		t.Error("a failed judge call must fall through to the card")
	}
	// 3. Judge abstains.
	task.Judge = func(context.Context, SelfCorrectionCandidateRecord) (SelfCorrectionVerdict, error) {
		return SelfCorrectionVerdict{Decided: false, Accept: true}, nil
	}
	if task.judged(context.Background(), rec, logger) {
		t.Error("an abstain must reach the operator — abstention is the point of having one")
	}
}

// TestZeroVerdictAbstains pins that the zero value is the safe answer, so any
// path that forgets to fill the struct lands on the card.
func TestZeroVerdictAbstains(t *testing.T) {
	if (SelfCorrectionVerdict{}).Decided {
		t.Error("the zero verdict must abstain")
	}
}

// TestJudgeRecordsProvenanceAndReason: a candidate closed without a person must
// say who closed it and why, or the ledger reads as if the operator approved.
func TestJudgeRecordsProvenanceAndReason(t *testing.T) {
	task, tracker := watchTaskForJudge(t)
	rec, err := tracker.RecordSelfCorrectionCandidate(SelfCorrectionCandidateRecord{
		Title: "재발하는 파싱 실패 수리", Scope: "code", Evidence: "3회 재발",
	})
	if err != nil {
		t.Skipf("candidate record unavailable: %v", err)
	}
	task.Judge = func(context.Context, SelfCorrectionCandidateRecord) (SelfCorrectionVerdict, error) {
		return SelfCorrectionVerdict{Decided: true, Accept: true, Rationale: "근거가 구체적이고 범위가 좁다"}, nil
	}
	if !task.judged(context.Background(), rec, task.Logger) {
		t.Fatal("a decided verdict must settle the candidate")
	}
	all, err := tracker.RecentSelfCorrectionCandidates("", "", 20)
	if err != nil {
		t.Fatalf("RecentSelfCorrectionCandidates: %v", err)
	}
	var after SelfCorrectionCandidateRecord
	for _, c := range all {
		if c.ID == rec.ID {
			after = c
		}
	}
	if after.ID == "" {
		t.Fatalf("candidate %s not found after review", rec.ID)
	}
	if after.Status != SelfCorrectionStatusAccepted {
		t.Errorf("status = %q, want accepted", after.Status)
	}
	if after.Reviewer != "llm-judge" {
		t.Errorf("reviewer = %q, want llm-judge — provenance must not read as operator approval", after.Reviewer)
	}
	if after.ReviewNote == "" {
		t.Error("a candidate closed without a person must record why")
	}
}

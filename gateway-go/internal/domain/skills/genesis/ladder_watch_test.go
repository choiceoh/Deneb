package genesis

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// The watch fires exactly once per READY transition: a fresh READY fires,
// a repeat run stays silent (snapshot persisted), a fall-back-and-re-earn
// fires again, and a nil OnReady never panics.
func TestLadderWatchFiresOnceOnReadyTransitionRetriesOnMissingOrFailedCallback(t *testing.T) {
	// Notify-only path: with auto-graduation on, supply would unlock immediately
	// and never surface as READY. Pin the kill switch so this test covers the
	// READY card transition itself.
	t.Setenv("DENEB_AUTO_GRADUATE", "0")
	// Hermetic HOME: graduatedDispatchSources() reads the operator's real
	// graduation_state.json otherwise, so a source graduated live (e.g.
	// sop-mining, unlocked 2026-08-16) silently becomes dispatchable here and
	// the re-earned READY below never fires.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	tr := newTestTracker(t)
	task := &LadderWatchTask{Tracker: tr}

	// Make the staged-sources row READY: one non-allowlist code candidate.
	// (runtime-error is compiled-dispatchable; use a novel prefix.)
	if _, err := tr.RecordSelfCorrectionCandidate(SelfCorrectionCandidateRecord{
		Scope: "code", Status: SelfCorrectionStatusProposed, SkillName: "sk",
		Title: "novel finding: triage gap", Source: "novel-miner:abcd",
	}); err != nil {
		t.Fatal(err)
	}

	var fired []string
	task.OnReady = func(title, detail string) error {
		fired = append(fired, title+"|"+detail)
		return nil
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(fired) != 1 || fired[0][:len("스테이징 소스 졸업")] != "스테이징 소스 졸업" {
		t.Fatalf("first run must fire the READY row once: %v", fired)
	}

	// Second run: same evidence, snapshot says already-notified — silent.
	fired = nil
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(fired) != 0 {
		t.Fatalf("repeat run must stay silent: %v", fired)
	}

	// Fall back (candidate rejected → staged row leaves READY), then re-earn
	// with a new candidate: the transition fires again.
	if _, err := tr.RecordSelfCorrectionReview(SelfCorrectionCandidateRecord{
		ID: selfCorrectionID(t, tr), Status: SelfCorrectionStatusRejected, Reason: "operator veto",
	}); err != nil {
		t.Fatal(err)
	}
	if err := task.Run(context.Background()); err != nil { // records the fall-back
		t.Fatal(err)
	}
	if _, err := tr.RecordSelfCorrectionCandidate(SelfCorrectionCandidateRecord{
		Scope: "code", Status: SelfCorrectionStatusProposed, SkillName: "sk2",
		Title: "sop mining: repeated triage", Source: "sop-mining:ffff",
	}); err != nil {
		t.Fatal(err)
	}
	fired = nil
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(fired) != 1 {
		t.Fatalf("re-earned READY must fire again: %v", fired)
	}

	// Snapshot file exists next to the ledgers. A missing delivery callback does
	// not consume READY, so restoring the callback retries the transition.
	if _, err := os.Stat(filepath.Join(filepath.Dir(tr.logPath), "ladder_watch_state.json")); err != nil {
		t.Fatalf("snapshot missing: %v", err)
	}
	task.OnReady = nil
	if err := os.Remove(task.ladderWatchStatePath()); err != nil {
		t.Fatal(err)
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	fired = nil
	task.OnReady = func(title, detail string) error {
		fired = append(fired, title+"|"+detail)
		return nil
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(fired) != 1 {
		t.Fatalf("READY transition consumed without a callback: %v", fired)
	}

	// A delivery error has the same retry contract.
	if err := os.Remove(task.ladderWatchStatePath()); err != nil {
		t.Fatal(err)
	}
	task.OnReady = func(string, string) error { return errors.New("feed unavailable") }
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	fired = nil
	task.OnReady = func(title, detail string) error {
		fired = append(fired, title+"|"+detail)
		return nil
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(fired) != 1 {
		t.Fatalf("failed READY delivery was not retried: %v", fired)
	}
}

// selfCorrectionID fetches the newest candidate's id (helper for the status
// transition in the watch test).
func selfCorrectionID(t *testing.T, tr *Tracker) string {
	t.Helper()
	cands, err := tr.RecentSelfCorrectionCandidates("", "", 10)
	if err != nil || len(cands) == 0 {
		t.Fatalf("no candidates: %v", err)
	}
	return cands[0].ID
}

package genesis

import (
	"context"
	"os"
	"strings"
	"testing"
)

// The unlock ledger: idempotent unlock + relock round-trip, lifecycle ledger
// entries, and the dispatch predicate consuming graduated sources. HOME is
// isolated because the state file lives at the FIXED shared path.
func TestGraduationStateUnlockIsIdempotentAndWidensDispatchPredicate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	tr := newTestTracker(t)

	if graduationUnlocked(graduationEProcess) {
		t.Fatal("fresh state must be locked everywhere")
	}
	fresh, err := tr.unlockGraduation(graduationEProcess, "n=20·합치 95%", 0, true)
	if err != nil || !fresh {
		t.Fatalf("unlock = %v, %v", fresh, err)
	}
	if fresh, _ = tr.unlockGraduation(graduationEProcess, "dup", 0, true); fresh {
		t.Fatal("second unlock must be a no-op")
	}
	if !graduationUnlocked(graduationEProcess) {
		t.Fatal("unlock not visible")
	}

	// Graduated staged source widens the dispatch predicate everywhere.
	if rsiSourceDispatchable("novel-miner:abcd") {
		t.Fatal("novel-miner must start staged")
	}
	if _, err := tr.unlockGraduation(graduationSourceKey("novel-miner"), "스테이징 후보 1건·기각 0", 0, true); err != nil {
		t.Fatal(err)
	}
	if !rsiSourceDispatchable("novel-miner:abcd") {
		t.Fatal("graduated source must dispatch")
	}
	if got := graduatedDispatchSources(); len(got) != 1 || got[0] != "novel-miner" {
		t.Fatalf("graduated sources = %v", got)
	}

	// Relock restores the lock and both transitions are ledgered.
	if err := tr.RelockGraduation(graduationSourceKey("novel-miner"), "operator veto"); err != nil {
		t.Fatal(err)
	}
	if rsiSourceDispatchable("novel-miner:abcd") {
		t.Fatal("relocked source must not dispatch")
	}
	entries, err := tr.RecentLifecycleLog(10)
	if err != nil {
		t.Fatal(err)
	}
	var grads, relocks int
	for _, e := range entries {
		switch e.Type {
		case "graduation":
			grads++
		case "graduation_relocked":
			relocks++
		}
	}
	if grads != 2 || relocks != 1 {
		t.Fatalf("ledger transitions = %d unlocks / %d relocks, want 2/1", grads, relocks)
	}
}

// A re-lock is a standing operator veto: because the evidence gates are
// cumulative, the next evidence-met auto ladder-watch would otherwise re-unlock
// and re-fire the graduation card, silently reverting the veto. The AUTO path
// must honor it; only an explicit operator (non-auto) unlock overrides.
func TestRelockVetoSurvivesEvidenceMetAutoRegraduation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	tr := newTestTracker(t)

	if fresh, err := tr.unlockGraduation(graduationDispatchCap, "판정 6·랜딩 60%", 4, true); err != nil || !fresh {
		t.Fatalf("first auto-unlock fresh=%v err=%v, want true/nil", fresh, err)
	}
	if err := tr.RelockGraduation(graduationDispatchCap, "operator veto"); err != nil {
		t.Fatal(err)
	}
	if graduationUnlocked(graduationDispatchCap) {
		t.Fatal("row must be locked after the operator veto")
	}

	// Same (still-met) evidence on the next auto cycle must NOT re-unlock.
	fresh, err := tr.unlockGraduation(graduationDispatchCap, "판정 6·랜딩 60% (still)", 4, true)
	if err != nil {
		t.Fatal(err)
	}
	if fresh {
		t.Error("auto re-unlock returned fresh=true — the veto was reverted")
	}
	if graduationUnlocked(graduationDispatchCap) {
		t.Error("operator re-lock veto was silently reverted by the auto path")
	}

	// An explicit operator (non-auto) unlock CAN override the veto.
	if fresh, err := tr.unlockGraduation(graduationDispatchCap, "operator manual", 4, false); err != nil || !fresh {
		t.Fatalf("operator manual unlock fresh=%v err=%v, want true/nil", fresh, err)
	}
	if !graduationUnlocked(graduationDispatchCap) {
		t.Error("operator manual unlock should override the veto")
	}
}

// eProcessOwnsRollback: graduation state flips ownership; the operator env
// knob overrides in BOTH directions.
func TestEProcessOwnsRollbackFlipsOnGraduationAndEnvOverride(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	t.Setenv("DENEB_EPROCESS_OWNS_ROLLBACK", "")
	tr := newTestTracker(t)
	if eProcessOwnsRollback() {
		t.Fatal("locked state must keep legacy ownership")
	}
	if _, err := tr.unlockGraduation(graduationEProcess, "evidence", 0, true); err != nil {
		t.Fatal(err)
	}
	if !eProcessOwnsRollback() {
		t.Fatal("graduated state must flip ownership")
	}
	t.Setenv("DENEB_EPROCESS_OWNS_ROLLBACK", "0")
	if eProcessOwnsRollback() {
		t.Fatal("env=0 must force legacy ownership past a graduation")
	}
	t.Setenv("DENEB_EPROCESS_OWNS_ROLLBACK", "1")
	if !eProcessOwnsRollback() {
		t.Fatal("env=1 must force e-process ownership")
	}
}

// The auto-graduator executes evidence-met unlocks once, respects a rejection
// veto, the kill switch, and the drift brake. Staged sources graduate on
// candidate supply alone (no human first-batch endorsement).
func TestLadderWatchAutoGraduatesOnFloorStopsForVetoKillSwitchAndDriftFreeze(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	t.Setenv("DENEB_AUTO_GRADUATE", "")
	tr := newTestTracker(t)
	task := &LadderWatchTask{Tracker: tr}

	seed := func(source, status string) {
		t.Helper()
		rec, err := tr.RecordSelfCorrectionCandidate(SelfCorrectionCandidateRecord{
			Scope: "code", Status: SelfCorrectionStatusProposed, SkillName: "sk",
			Title: "candidate " + source, Source: source,
		})
		if err != nil {
			t.Fatal(err)
		}
		if status != SelfCorrectionStatusProposed {
			if _, err := tr.RecordSelfCorrectionReview(SelfCorrectionCandidateRecord{
				ID: rec.ID, Status: status, Reason: "review verdict",
			}); err != nil {
				t.Fatal(err)
			}
		}
	}

	// One proposed candidate meets the supply floor — source graduates once.
	seed("novel-miner:a1", SelfCorrectionStatusProposed)
	var graduated []string
	task.OnGraduated = func(key, _, _ string) { graduated = append(graduated, key) }
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(graduated) != 1 || !strings.Contains(graduated[0], "novel-miner") {
		t.Fatalf("graduations = %v, want the novel-miner source", graduated)
	}
	if !rsiSourceDispatchable("novel-miner:zzz") {
		t.Fatal("graduated source must dispatch")
	}
	if err := task.Run(context.Background()); err != nil { // idempotent
		t.Fatal(err)
	}
	if len(graduated) != 1 {
		t.Fatalf("re-run must not re-graduate: %v", graduated)
	}

	// A rejection anywhere in a source blocks its graduation (standing veto).
	seed("sop-mining:b1", SelfCorrectionStatusProposed)
	seed("sop-mining:b2", SelfCorrectionStatusRejected)
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(graduated) != 1 {
		t.Fatalf("rejected source must not graduate: %v", graduated)
	}

	// Kill switch reverts to notify-only.
	t.Setenv("DENEB_AUTO_GRADUATE", "0")
	seed("other-miner:c1", SelfCorrectionStatusProposed)
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(graduated) != 1 {
		t.Fatalf("kill switch must stop execution: %v", graduated)
	}
	t.Setenv("DENEB_AUTO_GRADUATE", "")

	// Drift brake pauses auto-graduation with auto-adoption.
	if err := os.WriteFile(tr.autoAdoptFreezePath(), []byte(`{"frozen":true,"createdAt":1}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(graduated) != 1 {
		t.Fatalf("drift freeze must pause auto-graduation: %v", graduated)
	}
}

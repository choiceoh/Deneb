package genesis

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTrackerSaveWatchesWithoutPathIsNoop(t *testing.T) {
	t.Chdir(t.TempDir())

	tracker := &Tracker{}
	tracker.saveWatchesLocked()

	if _, err := os.Stat(".lock"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("empty watch path created a lock sidecar: %v", err)
	}
}

// An in-flight rollback watch must survive a tracker restart (the SIGUSR1
// deploy hot-swap) — before persistence, post-evolve failure counts silently
// reset and a regressing evolve could dodge its rollback forever.
func TestTracker_EvolveWatchSurvivesRestart(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DENEB_STATE_DIR", home)

	use := func(tr *Tracker, ok bool) {
		t.Helper()
		if err := tr.RecordUsage(UsageRecord{SkillName: "sk", SessionKey: "client:t", Success: ok, ErrorMsg: map[bool]string{true: "", false: "boom"}[ok]}); err != nil {
			t.Fatal(err)
		}
	}

	tr1, err := NewTracker(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	fired1 := make(chan string, 1)
	tr1.SetRollback(func(s string) bool { fired1 <- s; return true }, 3)
	use(tr1, true)
	use(tr1, false) // pre-evolve baseline: 2 uses, 1 fail
	if err := tr1.LogEvolveWithAudit("sk", "1.0.1", "d", HarnessEditAudit{}); err != nil {
		t.Fatal(err)
	}
	use(tr1, false)
	use(tr1, false) // postFails=2 < threshold — watch persisted mid-flight

	// Baseline snapshot must be in the persisted state.
	raw, err := os.ReadFile(filepath.Join(home, ".deneb", "data", "skill_evolve_watch.json"))
	if err != nil {
		t.Fatalf("watch state not persisted: %v", err)
	}
	var persisted map[string]persistedEvolveWatch
	if err := json.Unmarshal(raw, &persisted); err != nil {
		t.Fatal(err)
	}
	w := persisted["sk"]
	if w.PostFails != 2 || w.BaselineUses != 2 || w.BaselineFails != 1 {
		t.Fatalf("persisted watch wrong: %+v", w)
	}
	// The observation-mode e-process rides the same persistence: its wealth
	// must reflect the two observed post-evolve fails, not reset to 1.
	if w.EProcess == nil || w.EProcess.N != 2 || w.EProcess.E <= 1 {
		t.Fatalf("e-process not persisted mid-flight: %+v", w.EProcess)
	}

	// "Restart": a fresh tracker over the same HOME restores the watch, so ONE
	// more failure trips the threshold that would otherwise have reset to 0.
	tr2, err := NewTracker(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	fired2 := make(chan string, 1)
	tr2.SetRollback(func(s string) bool { fired2 <- s; return true }, 3)
	use(tr2, false)
	select {
	case s := <-fired2:
		if s != "sk" {
			t.Fatalf("rolled back wrong skill %q", s)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("restored watch did not trip rollback — persistence lost the counts")
	}

	// Resolution clears the persisted state: a third tracker restores nothing.
	raw, err = os.ReadFile(filepath.Join(home, ".deneb", "data", "skill_evolve_watch.json"))
	if err != nil {
		t.Fatal(err)
	}
	persisted = nil
	if err := json.Unmarshal(raw, &persisted); err != nil {
		t.Fatal(err)
	}
	if len(persisted) != 0 {
		t.Fatalf("resolved watch still persisted: %+v", persisted)
	}
}

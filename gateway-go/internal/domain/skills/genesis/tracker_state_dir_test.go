package genesis

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/generation"
)

// Tracker / genesis output / managed skills must root under DENEB_STATE_DIR so
// a live-test process cannot append to production ~/.deneb ledgers.
func TestGenesisPathsHonorDENEBStateDir(t *testing.T) {
	state := t.TempDir()
	t.Setenv("DENEB_STATE_DIR", state)
	t.Setenv("HOME", t.TempDir()) // must not win over DENEB_STATE_DIR

	tr, err := NewTracker(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.logJudgeAccuracy(judgeAccuracyRecord{Pairs: 1, Correct: 1}); err != nil {
		t.Fatal(err)
	}
	wantLedger := filepath.Join(state, "data", "judge_accuracy_log.jsonl")
	if _, err := os.Stat(wantLedger); err != nil {
		t.Fatalf("tracker ledger not under state dir: %v", err)
	}
	leak := filepath.Join(os.Getenv("HOME"), ".deneb", "data", "judge_accuracy_log.jsonl")
	if _, err := os.Stat(leak); err == nil {
		t.Fatalf("tracker wrote under HOME/.deneb despite DENEB_STATE_DIR: %s", leak)
	}

	cfg := generation.DefaultConfig()
	wantOut := filepath.Join(state, "skills", "genesis")
	if cfg.OutputDir != wantOut {
		t.Fatalf("OutputDir = %q, want %q", cfg.OutputDir, wantOut)
	}
	if got := skills.DefaultManagedSkillsDir(); got != filepath.Join(state, "skills") {
		t.Fatalf("DefaultManagedSkillsDir = %q", got)
	}
}

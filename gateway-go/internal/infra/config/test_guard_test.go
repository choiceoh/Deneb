package config

import (
	"path/filepath"
	"testing"
)

// The guard exists because test rows reached the live genesis ledger and
// tripped the self-brake's adoption-monotony detector, costing three manual
// freeze clearings (2026-07-31, 08-16, 08-23).
func TestGuardProductionState(t *testing.T) {
	home := processHome
	if home == "" {
		t.Skip("no home dir")
	}
	if !InGoTest() {
		t.Fatal("test binary must be detected as a test binary")
	}

	prod := filepath.Join(home, DefaultStateDirname)
	if err := GuardProductionState(prod, "genesis-tracker"); err == nil {
		t.Error("프로덕션 상태 디렉터리를 테스트에서 열 수 있으면 안 된다")
	}

	// An isolated dir is always fine — including one reached by overriding HOME,
	// which is the isolation the error message itself asks for.
	if err := GuardProductionState(t.TempDir(), "genesis-tracker"); err != nil {
		t.Errorf("격리된 디렉터리가 막힘: %v", err)
	}
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	if err := GuardProductionState(filepath.Join(tmp, DefaultStateDirname), "genesis-tracker"); err != nil {
		t.Errorf("HOME 격리한 테스트가 막힘: %v", err)
	}
	// A dir that merely sits under home is not the canonical one.
	if err := GuardProductionState(filepath.Join(home, ".deneb-dev"), "x"); err != nil {
		t.Errorf("다른 디렉터리가 막힘: %v", err)
	}
}

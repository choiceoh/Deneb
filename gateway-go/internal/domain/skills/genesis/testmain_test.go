package genesis

import (
	"os"
	"testing"
)

// TestMain isolates this package's tests from the operator's real state dir.
//
// Why: the agent/operator shell exports DENEB_STATE_DIR=$HOME/.deneb, and
// config.ResolveStateDir() prefers DENEB_STATE_DIR over HOME. That makes the
// per-test t.Setenv("HOME", t.TempDir()) isolation a no-op: any test touching
// NewTracker (or anything else resolved through ResolveStateDir) writes
// straight into the production data dir.
//
// This actually happened on 2026-08-16: two `go test` runs of this package
// appended ~2,200 synthetic entries into 7 production ledgers and left behind
// an empty auto_adopt_freeze.json, which the drift self-brake (fail-closed by
// design) read as a real freeze — auto-adopt was spuriously frozen until an
// operator cleared it. See wiki 시스템/rollback-drill-20260816 후속 3.
//
// Pinning DENEB_STATE_DIR to a package-level temp dir makes every test in this
// package hermetic regardless of the caller's environment. Tests that set HOME
// themselves keep working; the state-dir override simply wins, as it does in
// production.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "genesis-test-state-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	if err := os.Setenv("DENEB_STATE_DIR", dir); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

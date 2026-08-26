// test_guard.go — refuse the production state directory from a test binary.
//
// The state dir resolves from DENEB_STATE_DIR, else ~/.deneb. That makes
// isolation a CONVENTION every test must remember, and forgetting it writes
// straight into the live ledgers. The cost is not hypothetical: test rows in
// skill_genesis_log.jsonl (createdAt≈1000, artifact evolve.md, 27 auto_adopted
// rows) tripped the genesis self-brake's adoption-monotony detector twice, and
// the operator had to clear a false auto-adopt freeze by hand three times
// (2026-07-31, 08-16, 08-23).
//
// The intent was already written down — NewTracker says "Live-test/dev
// instances must not append to the production JSONL ledgers" — but nothing
// enforced it. This turns that sentence into a check.
package config

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
)

// InGoTest reports whether this process is a `go test` binary. Detected without
// importing "testing" (which would link test flags into the gateway binary):
// the test binary's name ends in ".test" and the testing package registers
// -test.* flags before any test runs.
func InGoTest() bool {
	if strings.HasSuffix(filepath.Base(os.Args[0]), ".test") {
		return true
	}
	return flag.Lookup("test.v") != nil
}

// processHome is the home directory as it was when the process started.
// os.UserHomeDir() reads $HOME, and a test that isolates itself with
// t.Setenv("HOME", t.TempDir()) changes that AFTER init — so asking later would
// call the test's own temp home "production" and block the very isolation this
// guard is asking for.
var processHome = func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}()

// IsProductionStateDir reports whether dir is the real home's canonical state
// directory — the one the running gateway owns.
func IsProductionStateDir(dir string) bool {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return false
	}
	home := processHome
	if home == "" {
		return false
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	return abs == filepath.Join(home, DefaultStateDirname)
}

// GuardProductionState returns an error when a test binary resolved the
// production state directory. Callers that own live ledgers check this before
// opening them, so a test missing its isolation fails loudly instead of
// appending to what the gateway is using.
func GuardProductionState(dir, owner string) error {
	if !InGoTest() || !IsProductionStateDir(dir) {
		return nil
	}
	return &ProductionStateError{Dir: dir, Owner: owner}
}

// ProductionStateError names the missing isolation and how to add it.
type ProductionStateError struct {
	Dir   string
	Owner string
}

func (e *ProductionStateError) Error() string {
	return e.Owner + ": 테스트가 프로덕션 상태 디렉터리(" + e.Dir +
		")를 열려고 했다 — t.Setenv(\"HOME\", t.TempDir()) 또는 t.Setenv(\"DENEB_STATE_DIR\", …)로 격리하라"
}

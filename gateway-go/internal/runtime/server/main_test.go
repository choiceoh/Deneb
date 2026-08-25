package server

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain keeps this package off the operator's real Deneb state. Server
// construction resolves the state dir (and, through it, the agent workspace
// where the fact plane writes generated USER.md/MEMORY.md), so a test that does
// not redirect DENEB_STATE_DIR runs against ~/.deneb. Measured 2026-08-25:
// `go test ./internal/runtime/server/...` republished the production workspace
// projection at revision 0 while the live journal held 126, leaving the running
// gateway's system prompt factless until its next restart. Many of these tests
// call t.Parallel(), where t.Setenv is illegal — a process-wide default is the
// only place the redirect can live for them.
//
// Tests that exercise HOME-based resolution clear the variable themselves
// (t.Setenv("DENEB_STATE_DIR", "")).
func TestMain(m *testing.M) {
	if os.Getenv("DENEB_STATE_DIR") != "" {
		os.Exit(m.Run())
	}
	dir, err := os.MkdirTemp("", "deneb-server-test-state-")
	if err != nil {
		panic("server tests: create isolated state dir: " + err.Error())
	}
	if err := os.MkdirAll(filepath.Join(dir, "workspace"), 0o700); err != nil {
		panic("server tests: create isolated workspace: " + err.Error())
	}
	_ = os.Setenv("DENEB_STATE_DIR", dir)
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

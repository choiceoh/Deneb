package cliprobe

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// writeScript creates an executable shell script in dir that exits with the
// given code, optionally printing a version line. Returns the command name.
// The test prepends dir to PATH so the fake tool is discovered by LookPath.
func writeScript(t *testing.T, dir, name, body string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatalf("write script %s: %v", name, err)
	}
	return name
}

func TestProbe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixtures are POSIX-only")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}

	dir := t.TempDir()
	// Isolate PATH so only our fixtures (plus the absolute-pathed sh) resolve.
	t.Setenv("PATH", dir)

	okName := writeScript(t, dir, "tool-ok",
		"#!/bin/sh\necho 'tool-ok 1.2.3'\nexit 0\n", 0o755)
	brokenExitName := writeScript(t, dir, "tool-broken",
		"#!/bin/sh\necho 'boom' >&2\nexit 1\n", 0o755)
	// Non-executable file: present on PATH (LookPath in pure-Go also checks the
	// exec bit, so this should read as missing — exercises the LookPath branch).
	notExecName := writeScript(t, dir, "tool-noexec",
		"#!/bin/sh\nexit 0\n", 0o644)
	// Broken interpreter shim: shebang points at a nonexistent interpreter, so
	// the OS refuses to exec it → classic broken-venv signature.
	brokenShimName := writeScript(t, dir, "tool-shim",
		"#!"+dir+"/nonexistent-python\nprint('x')\n", 0o755)

	tests := []struct {
		name       string
		cmd        string
		wantStatus Status
	}{
		{"ok tool runs --version cleanly", okName, StatusOK},
		{"nonzero exit is broken", brokenExitName, StatusBroken},
		{"non-executable reads as missing", notExecName, StatusMissing},
		{"broken interpreter shim is broken", brokenShimName, StatusBroken},
		{"absent tool is missing", "definitely-not-installed-xyz", StatusMissing},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Probe(context.Background(), tt.cmd, "")
			if r.Status != tt.wantStatus {
				t.Fatalf("Probe(%q).Status = %v (err=%v), want %v",
					tt.cmd, r.Status, r.Err, tt.wantStatus)
			}
			if r.OK() != (tt.wantStatus == StatusOK) {
				t.Errorf("OK() = %v, want %v", r.OK(), tt.wantStatus == StatusOK)
			}
			switch tt.wantStatus {
			case StatusOK:
				if r.Err != nil {
					t.Errorf("OK result should have nil Err, got %v", r.Err)
				}
				if r.Path == "" {
					t.Errorf("OK result should carry resolved Path")
				}
				if FormatProblem(r) != "" {
					t.Errorf("FormatProblem on OK should be empty, got %q", FormatProblem(r))
				}
			default:
				// Non-OK results must carry a hint (default fallback when "" passed).
				if r.Hint == "" {
					t.Errorf("%v result should carry a remediation hint", tt.wantStatus)
				}
				if FormatProblem(r) == "" {
					t.Errorf("FormatProblem on non-OK should be non-empty")
				}
			}
		})
	}
}

// A caller-supplied hint must survive onto the Result for non-OK outcomes.
func TestProbe_CustomHintPreserved(t *testing.T) {
	const hint = "pip install --force-reinstall yt-dlp"
	r := Probe(context.Background(), "definitely-not-installed-xyz", hint)
	if r.Status != StatusMissing {
		t.Fatalf("want missing, got %v", r.Status)
	}
	if r.Hint != hint {
		t.Errorf("custom hint not preserved: got %q want %q", r.Hint, hint)
	}
}

func TestStatusString(t *testing.T) {
	cases := map[Status]string{
		StatusOK:      "ok",
		StatusMissing: "missing",
		StatusBroken:  "broken",
		Status(99):    "unknown",
	}
	for s, want := range cases {
		if got := s.String(); got != want {
			t.Errorf("Status(%d).String() = %q, want %q", s, got, want)
		}
	}
}

func TestIsBrokenExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixtures are POSIX-only")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	dir := t.TempDir()

	// exit 127 — command-not-found signature.
	p127 := filepath.Join(dir, "e127")
	_ = os.WriteFile(p127, []byte("#!/bin/sh\nexit 127\n"), 0o755)
	// exit 1 — ordinary failure, not a broken-shim signal.
	p1 := filepath.Join(dir, "e1")
	_ = os.WriteFile(p1, []byte("#!/bin/sh\nexit 1\n"), 0o755)

	if err := exec.Command(p127).Run(); !IsBrokenExit(err) {
		t.Errorf("exit 127 should be a broken exit, err=%v", err)
	}
	if err := exec.Command(p1).Run(); IsBrokenExit(err) {
		t.Errorf("exit 1 should NOT be a broken exit, err=%v", err)
	}
	if IsBrokenExit(nil) {
		t.Errorf("nil err should not be a broken exit")
	}
}

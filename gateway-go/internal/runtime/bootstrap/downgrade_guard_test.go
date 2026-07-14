package bootstrap

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseAndCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int // sign of compare(a,b); 9 = a unparseable
	}{
		{"4.62.2", "4.47.1", 1},
		{"4.47.1", "4.62.2", -1},
		{"deneb-v4.62.2", "v4.62.2", 0},
		{"4.62", "4.62.0", 0},
		{"4.62.2-rc1", "4.62.2", 0}, // suffix ignored
		{"dev", "4.62.2", 9},
		{"", "4.62.2", 9},
	}
	for _, tc := range cases {
		av, aok := parseVersion(tc.a)
		if tc.want == 9 {
			if aok {
				t.Errorf("parseVersion(%q) ok=true, want unparseable", tc.a)
			}
			continue
		}
		bv, bok := parseVersion(tc.b)
		if !aok || !bok {
			t.Fatalf("parseVersion(%q/%q) failed", tc.a, tc.b)
		}
		if got := compareVersions(av, bv); got != tc.want {
			t.Errorf("compare(%q,%q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

// fakeCandidate points the guard's executable seam at a temp-dir path,
// optionally writing a shell script there to play the candidate binary.
// pathSuffix simulates the "/proc/self/exe (deleted)" readlink decoration.
func fakeCandidate(t *testing.T, script, pathSuffix string) string {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "deneb-gateway")
	if script != "" {
		if err := os.WriteFile(exe, []byte(script), 0o755); err != nil {
			t.Fatalf("write fake candidate: %v", err)
		}
	}
	orig := executablePath
	executablePath = func() (string, error) { return exe + pathSuffix, nil }
	t.Cleanup(func() { executablePath = orig })
	return exe
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError + 4}))
}

func TestShouldAcceptRestart(t *testing.T) {
	logger := quietLogger()

	t.Run("unversioned build: guard off", func(t *testing.T) {
		if !shouldAcceptRestart("", logger) || !shouldAcceptRestart("dev", logger) {
			t.Error("unversioned build must always accept restarts")
		}
	})

	t.Run("newer candidate accepted", func(t *testing.T) {
		exe := fakeCandidate(t, "#!/bin/sh\necho 9.9.9\n", "")
		if !shouldAcceptRestart("4.62.2", logger) {
			t.Error("newer candidate must be accepted")
		}
		if _, err := os.Stat(exe); err != nil {
			t.Errorf("accepted candidate must stay in place: %v", err)
		}
	})

	t.Run("deleted-suffix path still probes", func(t *testing.T) {
		// After a deploy renames the binary over our path, /proc/self/exe reads
		// "<path> (deleted)" — the probe must strip it or every legitimate
		// guard-era deploy would be refused with ENOENT.
		fakeCandidate(t, "#!/bin/sh\necho 9.9.9\n", " (deleted)")
		if !shouldAcceptRestart("4.62.2", logger) {
			t.Error("deleted-suffixed executable path must not break the probe")
		}
	})

	t.Run("version-less candidate refused and quarantined", func(t *testing.T) {
		exe := fakeCandidate(t, "#!/bin/sh\nexit 1\n", "")
		if shouldAcceptRestart("4.62.2", logger) {
			t.Fatal("version-less candidate must be refused")
		}
		// The stale file must be moved aside and a copy of the running image
		// restored at the exec path, so a crash/reboot cannot boot the refused
		// candidate.
		matches, _ := filepath.Glob(exe + ".refused-*")
		if len(matches) != 1 {
			t.Fatalf("expected one quarantined file, got %v", matches)
		}
		info, err := os.Stat(exe)
		if err != nil || info.Size() == 0 {
			t.Errorf("running image not restored at exec path: %v", err)
		}
	})

	t.Run("older candidate refused and quarantined", func(t *testing.T) {
		exe := fakeCandidate(t, "#!/bin/sh\necho 4.47.1\n", "")
		if shouldAcceptRestart("4.62.2", logger) {
			t.Fatal("older candidate must be refused")
		}
		if matches, _ := filepath.Glob(exe + ".refused-*"); len(matches) != 1 {
			t.Errorf("expected quarantine, got %v", matches)
		}
	})

	t.Run("marker authorizes one downgrade and is consumed", func(t *testing.T) {
		exe := fakeCandidate(t, "", "")
		marker := filepath.Join(filepath.Dir(exe), allowDowngradeMarker)
		if err := os.WriteFile(marker, nil, 0o600); err != nil {
			t.Fatalf("write marker: %v", err)
		}
		if !shouldAcceptRestart("4.62.2", logger) {
			t.Error("marker must authorize the restart")
		}
		if _, err := os.Stat(marker); err == nil {
			t.Error("marker must be consumed (single use)")
		}
	})

	t.Run("same version, older build stamp refused", func(t *testing.T) {
		origStamp := BuildUnix
		t.Cleanup(func() { BuildUnix = origStamp })
		BuildUnix = "2000000000"
		exe := fakeCandidate(t, "#!/bin/sh\necho '4.62.2 1000000000'\n", "")
		if shouldAcceptRestart("4.62.2", logger) {
			t.Fatal("same-version candidate with a much older build stamp must be refused")
		}
		if matches, _ := filepath.Glob(exe + ".refused-*"); len(matches) != 1 {
			t.Errorf("expected quarantine, got %v", matches)
		}
	})

	t.Run("same version, same stamp accepted", func(t *testing.T) {
		origStamp := BuildUnix
		t.Cleanup(func() { BuildUnix = origStamp })
		BuildUnix = "2000000000"
		fakeCandidate(t, "#!/bin/sh\necho '4.62.2 2000000000'\n", "")
		if !shouldAcceptRestart("4.62.2", logger) {
			t.Error("same-version same-stamp candidate must be accepted")
		}
	})

	t.Run("same version, no stamps: tiebreak skipped", func(t *testing.T) {
		origStamp := BuildUnix
		t.Cleanup(func() { BuildUnix = origStamp })
		BuildUnix = ""
		fakeCandidate(t, "#!/bin/sh\necho 4.62.2\n", "")
		if !shouldAcceptRestart("4.62.2", logger) {
			t.Error("without build stamps the same-version tiebreak must not refuse")
		}
	})
}

// TestQuarantineIgnoresRunningSelf: when the exec path still holds the RUNNING binary
// (no replacement happened), refusal paths must not rename it away.
func TestQuarantineIgnoresRunningSelf(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Skipf("os.Executable: %v", err)
	}
	self = strings.TrimSuffix(self, " (deleted)")
	quarantineCandidate(self, quietLogger())
	if matches, _ := filepath.Glob(self + ".refused-*"); len(matches) != 0 {
		for _, m := range matches {
			_ = os.Remove(m)
		}
		t.Errorf("quarantine must no-op on the running binary itself: %v", matches)
	}
}

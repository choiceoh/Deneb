package bootstrap

import (
	"log/slog"
	"os"
	"path/filepath"
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

func TestShouldAcceptRestart(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError + 1}))

	// Unversioned build (dev, tests, live-test instances): guard off.
	if !shouldAcceptRestart("", logger) || !shouldAcceptRestart("dev", logger) {
		t.Error("unversioned build must always accept restarts")
	}

	// Versioned build, candidate = this test binary, which cannot answer
	// --print-version: definitionally a pre-guard (stale) build → refuse.
	if shouldAcceptRestart("4.62.2", logger) {
		t.Error("versioned build must refuse a candidate that cannot report its version")
	}

	// The .allow-downgrade marker authorizes exactly one restart and is consumed.
	exe, err := os.Executable()
	if err != nil {
		t.Skipf("os.Executable: %v", err)
	}
	marker := filepath.Join(filepath.Dir(exe), allowDowngradeMarker)
	if werr := os.WriteFile(marker, nil, 0o600); werr != nil {
		t.Skipf("cannot write marker next to test binary: %v", werr)
	}
	t.Cleanup(func() { _ = os.Remove(marker) })
	if !shouldAcceptRestart("4.62.2", logger) {
		t.Error("marker must authorize the restart")
	}
	if _, serr := os.Stat(marker); serr == nil {
		t.Error("marker must be consumed (single use)")
	}
}

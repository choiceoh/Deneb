// downgrade_guard.go — refuse SIGUSR1 restarts into an older binary.
//
// Twice (2026-07-04 23:17, 2026-07-05 11:42) an unattributed deploy from a
// stale clone cut production over to a months-old build (version stamped from
// the clone's stale deneb-v* tags), taking every native-app data screen down.
// Sender-side guards cannot close this hole: the rogue path runs an OLD copy
// of deploy.sh that predates any warning. So the RECEIVER decides: before the
// running gateway honors SIGUSR1, it interrogates the candidate binary sitting
// at its own executable path (the file systemd will exec next) and refuses the
// restart when the candidate is older — every sender path is covered at once.
//
// Escape hatch for intentional rollbacks: a .allow-downgrade marker next to
// the binary (created by deploy.sh under DENEB_DEPLOY_FORCE=1), consumed on
// use so it never lingers.
package bootstrap

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// allowDowngradeMarker sits next to the gateway binary; its presence authorizes
// exactly one downgrade restart.
const allowDowngradeMarker = ".allow-downgrade"

// probeVersionTimeout bounds the candidate --print-version exec.
const probeVersionTimeout = 5 * time.Second

// acceptRestart is indirected for tests (the lifecycle signal tests self-send
// SIGUSR1 from a test binary that has no --print-version).
var acceptRestart = shouldAcceptRestart

// shouldAcceptRestart reports whether a SIGUSR1 restart may proceed. current is
// the running gateway's version; empty or "dev" (unversioned dev builds, unit
// tests, live-test instances) skips the guard entirely — this protects tagged
// production builds without getting in the way of development.
func shouldAcceptRestart(current string, logger *slog.Logger) bool {
	cur, ok := parseVersion(current)
	if !ok {
		return true // dev/unversioned build — guard off
	}
	exe, err := os.Executable()
	if err != nil {
		// Exotic failure; refusing here could wedge legitimate deploys forever,
		// and a rogue deploy still gets caught on the next versioned boot.
		logger.Warn("downgrade guard: cannot resolve own executable; allowing restart", "error", err)
		return true
	}

	marker := filepath.Join(filepath.Dir(exe), allowDowngradeMarker)
	if _, merr := os.Stat(marker); merr == nil {
		_ = os.Remove(marker) // single-use authorization
		logger.Warn("downgrade guard: explicit downgrade authorized via marker", "marker", marker)
		return true
	}

	ctx, cancel := context.WithTimeout(context.Background(), probeVersionTimeout)
	defer cancel()
	out, perr := exec.CommandContext(ctx, exe, "--print-version").Output()
	if perr != nil {
		// A candidate that cannot report its version predates this guard —
		// definitionally a stale build (every binary from now on has the flag).
		logger.Error("SIGUSR1 restart REFUSED: candidate binary cannot report a version (stale pre-guard build?)",
			"candidate", exe, "error", perr,
			"hint", "의도적 롤백이면 dist/.allow-downgrade 마커를 만들거나 DENEB_DEPLOY_FORCE=1 deploy.sh")
		return false
	}
	cand, cok := parseVersion(strings.TrimSpace(string(out)))
	if !cok {
		logger.Error("SIGUSR1 restart REFUSED: candidate version unparseable",
			"candidate", exe, "output", strings.TrimSpace(string(out)))
		return false
	}
	if compareVersions(cand, cur) < 0 {
		logger.Error("SIGUSR1 restart REFUSED: candidate binary is OLDER than the running gateway (stale-clone deploy guard)",
			"running", current, "candidate", strings.TrimSpace(string(out)),
			"hint", "의도적 롤백이면 dist/.allow-downgrade 마커 또는 DENEB_DEPLOY_FORCE=1 deploy.sh")
		return false
	}
	return true
}

// parseVersion extracts dotted numeric fields from "4.62.2", "v4.62.2",
// "deneb-v4.62.2". ok=false for empty/"dev"/no digits.
func parseVersion(s string) ([]int, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "deneb-")
	s = strings.TrimPrefix(s, "v")
	if s == "" || s == "dev" {
		return nil, false
	}
	// Cut any suffix after the numeric core ("4.62.2-rc1" → "4.62.2").
	core := s
	if i := strings.IndexFunc(s, func(r rune) bool { return (r < '0' || r > '9') && r != '.' }); i >= 0 {
		core = s[:i]
	}
	parts := strings.Split(core, ".")
	nums := make([]int, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, false
		}
		nums = append(nums, n)
	}
	if len(nums) == 0 {
		return nil, false
	}
	return nums, true
}

// compareVersions returns -1/0/+1 comparing dotted numeric versions field by
// field; missing fields count as 0 (4.62 == 4.62.0).
func compareVersions(a, b []int) int {
	n := len(a)
	if len(b) > n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		av, bv := 0, 0
		if i < len(a) {
			av = a[i]
		}
		if i < len(b) {
			bv = b[i]
		}
		switch {
		case av < bv:
			return -1
		case av > bv:
			return 1
		}
	}
	return 0
}

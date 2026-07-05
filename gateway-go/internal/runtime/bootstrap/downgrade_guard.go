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
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// BuildUnix is the build's unix timestamp, injected via ldflags (Makefile).
// It breaks same-version ties: every deploy between releases carries the same
// tag version, so a stale checkout with CURRENT tags but OLD code stamps the
// same version — only the build time tells them apart. Empty for dev builds.
var BuildUnix string

// allowDowngradeMarker sits next to the gateway binary; its presence authorizes
// exactly one downgrade restart.
const allowDowngradeMarker = ".allow-downgrade"

// probeVersionTimeout bounds the candidate --print-version exec.
const probeVersionTimeout = 5 * time.Second

// buildStampSlack tolerates clock skew / near-simultaneous builds when
// comparing same-version build stamps: only a candidate MORE than this much
// older than the running build is treated as stale.
const buildStampSlack = 10 * time.Minute

// acceptRestart is indirected for tests (the lifecycle signal tests self-send
// SIGUSR1 from a test binary that has no --print-version).
var acceptRestart = shouldAcceptRestart

// executablePath is indirected for tests so the guard can be pointed at a
// temp-dir fake candidate instead of the real test binary.
var executablePath = os.Executable

// shouldAcceptRestart reports whether a SIGUSR1 restart may proceed. current is
// the running gateway's version; empty or "dev" (unversioned dev builds, unit
// tests, live-test instances) skips the guard entirely — this protects tagged
// production builds without getting in the way of development.
func shouldAcceptRestart(current string, logger *slog.Logger) bool {
	cur, ok := parseVersion(current)
	if !ok {
		return true // dev/unversioned build — guard off
	}
	exe, err := executablePath()
	if err != nil {
		// Exotic failure; refusing here could wedge legitimate deploys forever,
		// and a rogue deploy still gets caught on the next versioned boot.
		logger.Warn("downgrade guard: cannot resolve own executable; allowing restart", "error", err)
		return true
	}
	// A deploy replaces the file at our path via rename, unlinking the inode we
	// are running from — Linux then reports /proc/self/exe as "<path> (deleted)".
	// Without stripping the suffix the probe exec would ENOENT and the guard
	// would refuse every LEGITIMATE deploy from the second guard-era deploy on.
	exe = strings.TrimSuffix(exe, " (deleted)")

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
		quarantineCandidate(exe, logger)
		return false
	}
	// Output: "<version>[ <buildUnix>]" (config.go --print-version).
	fields := strings.Fields(strings.TrimSpace(string(out)))
	candOut := strings.TrimSpace(string(out))
	var candVerStr, candStampStr string
	if len(fields) > 0 {
		candVerStr = fields[0]
	}
	if len(fields) > 1 {
		candStampStr = fields[1]
	}
	cand, cok := parseVersion(candVerStr)
	if !cok {
		logger.Error("SIGUSR1 restart REFUSED: candidate version unparseable",
			"candidate", exe, "output", candOut)
		quarantineCandidate(exe, logger)
		return false
	}
	if compareVersions(cand, cur) < 0 {
		logger.Error("SIGUSR1 restart REFUSED: candidate binary is OLDER than the running gateway (stale-clone deploy guard)",
			"running", current, "candidate", candOut,
			"hint", "의도적 롤백이면 dist/.allow-downgrade 마커 또는 DENEB_DEPLOY_FORCE=1 deploy.sh")
		quarantineCandidate(exe, logger)
		return false
	}
	// Same numeric version: fall back to build stamps. A stale checkout with
	// current tags stamps the same version as production — the timestamp is
	// the only signal left. Both stamps must exist (dev builds have none).
	if compareVersions(cand, cur) == 0 {
		curStamp, cerr := strconv.ParseInt(strings.TrimSpace(BuildUnix), 10, 64)
		candStamp, kerr := strconv.ParseInt(candStampStr, 10, 64)
		if cerr == nil && kerr == nil && curStamp-candStamp > int64(buildStampSlack.Seconds()) {
			logger.Error("SIGUSR1 restart REFUSED: same version but candidate build is older (stale checkout with current tags?)",
				"running", current, "runningBuildUnix", curStamp, "candidateBuildUnix", candStamp,
				"hint", "의도적 롤백이면 dist/.allow-downgrade 마커 또는 DENEB_DEPLOY_FORCE=1 deploy.sh")
			quarantineCandidate(exe, logger)
			return false
		}
	}
	return true
}

// quarantineCandidate moves a refused candidate out of the exec path and
// restores the running binary in its place. Without this, the refused stale
// file stays at the path systemd relaunches — any later crash, manual restart,
// or reboot would boot it with no guard (the guard lives in the OLD process).
// The running image is still readable via /proc/self/exe even after its inode
// was unlinked by the deploy's rename. Best-effort: failures log and leave
// state as-is. No-op when the path still IS the running binary (nothing was
// replaced — e.g. a plain restart signal).
func quarantineCandidate(exe string, logger *slog.Logger) {
	candInfo, err := os.Stat(exe)
	if err != nil {
		return
	}
	if selfInfo, serr := os.Stat("/proc/self/exe"); serr == nil && os.SameFile(candInfo, selfInfo) {
		return // the path still holds the running binary — nothing to quarantine
	}
	quarantined := exe + ".refused-" + strconv.FormatInt(time.Now().Unix(), 10)
	if err := os.Rename(exe, quarantined); err != nil {
		logger.Error("downgrade guard: failed to quarantine refused candidate", "error", err)
		return
	}
	self, err := os.Open("/proc/self/exe")
	if err != nil {
		logger.Error("downgrade guard: quarantined candidate but cannot read own image for restore",
			"quarantined", quarantined, "error", err)
		return
	}
	defer self.Close()
	dst, err := os.OpenFile(exe, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		logger.Error("downgrade guard: quarantined candidate but cannot restore running binary",
			"quarantined", quarantined, "error", err)
		return
	}
	defer dst.Close()
	if _, err := io.Copy(dst, self); err != nil {
		logger.Error("downgrade guard: restore copy failed", "error", err)
		return
	}
	logger.Warn("downgrade guard: refused candidate quarantined; running binary restored at exec path",
		"quarantined", quarantined)
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

// health_gpu.go adds DGX Spark GPU telemetry to /health (a "gpu" section) and a
// dedicated GET /health/gpu route. There is no other GPU telemetry in the
// gateway today — role health only pings reachability — so this is the operator's
// only window into utilization / VRAM / temperature of the box the local engine
// runs on.
//
// Source: a short `nvidia-smi --query-gpu=... --format=csv,noheader,nounits`
// shell-out, parsed into a small struct. The result is cached for a few seconds
// so a load balancer hammering /health does not fork nvidia-smi per probe.
//
// Graceful degradation is the whole point: on a host with no NVIDIA GPU,
// nvidia-smi is absent (exec error) — the collector returns no rows and /health
// simply omits the "gpu" section. It is NEVER an error and must NOT break
// /health on non-GPU hosts. This mirrors the silent-skip discipline of the
// other health probes (engine cache scrape, channel health, …).
package server

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// gpuQueryFields are the CSV columns requested, in order. The parser is
	// positional and depends on this exact ordering.
	gpuQueryFields = "utilization.gpu,memory.used,memory.total,temperature.gpu"
	// gpuSampleTTL caches a reading for a few seconds — long enough to absorb
	// load-balancer health polling, short enough to stay live for an operator
	// watching the box.
	gpuSampleTTL = 3 * time.Second
	// gpuQueryTimeout bounds the shell-out; nvidia-smi normally answers in tens
	// of ms but can hang if the driver is wedged, which must not stall /health.
	gpuQueryTimeout = 2 * time.Second
)

// gpuStat is one GPU's telemetry. Units match the nounits CSV: utilization and
// temperature in percent / Celsius, memory in MiB.
type gpuStat struct {
	Index       int `json:"index"`
	UtilPct     int `json:"utilizationPct"`
	MemUsedMiB  int `json:"memUsedMiB"`
	MemTotalMiB int `json:"memTotalMiB"`
	TempC       int `json:"temperatureC"`
}

// gpuHealth caches the most recent nvidia-smi reading. A nil/zero value is a
// valid, ready-to-use collector (no constructor needed); the zero gpuSnapshot
// just looks stale and triggers the first scrape.
type gpuHealth struct {
	mu       sync.Mutex
	cachedAt time.Time
	stats    []gpuStat
	present  bool // whether the last probe found nvidia-smi at all
	probed   bool // whether we have probed even once
}

// gpuRunner is the seam for the nvidia-smi shell-out, swappable in tests. It
// returns the raw stdout (CSV) and whether the binary ran at all (ok=false when
// nvidia-smi is absent or errored — the graceful-degradation signal).
type gpuRunner func(ctx context.Context) (csv string, ok bool)

// defaultGPURunner shells out to nvidia-smi. Absent binary, non-zero exit, or
// timeout all collapse to ok=false so the caller drops the gpu section.
func defaultGPURunner(ctx context.Context) (string, bool) {
	cctx, cancel := context.WithTimeout(ctx, gpuQueryTimeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, "nvidia-smi",
		"--query-gpu="+gpuQueryFields,
		"--format=csv,noheader,nounits")
	out, err := cmd.Output()
	if err != nil {
		return "", false // no GPU / no driver / wedged → silently skip
	}
	return string(out), true
}

// observe returns the current GPU stats, scraping at most once per gpuSampleTTL.
// present reports whether a GPU was found at all: when false the caller omits
// the gpu section. Pass a nil runner to use the real nvidia-smi.
func (g *gpuHealth) observe(ctx context.Context, runner gpuRunner) (stats []gpuStat, present bool) {
	if runner == nil {
		runner = defaultGPURunner
	}
	g.mu.Lock()
	fresh := g.probed && time.Since(g.cachedAt) < gpuSampleTTL
	if fresh {
		stats, present = g.stats, g.present
		g.mu.Unlock()
		return stats, present
	}
	g.mu.Unlock()

	csv, ok := runner(ctx)
	parsed := parseGPUStats(csv)
	// A successful run that parsed at least one row means a GPU is present.
	// A run that returned ok but parsed nothing (unexpected output) is treated
	// as "not present" so we never render an empty/garbage section.
	gotGPU := ok && len(parsed) > 0

	g.mu.Lock()
	g.cachedAt = time.Now()
	g.probed = true
	g.present = gotGPU
	if gotGPU {
		g.stats = parsed
	} else {
		g.stats = nil
	}
	stats, present = g.stats, g.present
	g.mu.Unlock()
	return stats, present
}

// parseGPUStats parses nvidia-smi CSV (noheader,nounits) rows of the form
// "utilization, memUsed, memTotal, temperature" into gpuStats. It is a pure
// function — the unit-tested core of the collector. Lines that are blank,
// short, or non-numeric are skipped individually so one malformed row never
// discards the rest. Missing/blank values default to 0, and "[N/A]" (which
// nvidia-smi emits for an unsupported field) is tolerated as 0.
func parseGPUStats(csv string) []gpuStat {
	var out []gpuStat
	idx := 0
	for _, line := range strings.Split(csv, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Split(line, ",")
		if len(fields) < 4 {
			continue // not the shape we asked for — skip defensively
		}
		out = append(out, gpuStat{
			Index:       idx,
			UtilPct:     parseGPUInt(fields[0]),
			MemUsedMiB:  parseGPUInt(fields[1]),
			MemTotalMiB: parseGPUInt(fields[2]),
			TempC:       parseGPUInt(fields[3]),
		})
		idx++
	}
	return out
}

// parseGPUInt parses one CSV cell to an int, tolerating surrounding spaces, a
// stray unit suffix, and nvidia-smi's "[N/A]" / "[Not Supported]" sentinels
// (all → 0). Never returns an error: a bad cell is just 0 so the rest of the
// row still renders.
func parseGPUInt(s string) int {
	s = strings.TrimSpace(s)
	if s == "" || strings.HasPrefix(s, "[") {
		return 0
	}
	// Drop any trailing non-digit junk (defensive — nounits should prevent it).
	if i := strings.IndexFunc(s, func(r rune) bool { return r < '0' || r > '9' }); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return 0
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return v
}

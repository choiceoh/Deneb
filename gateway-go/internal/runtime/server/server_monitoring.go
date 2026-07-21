package server

import (
	"context"
	"log/slog"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/daemon"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/events"
)

// SetDaemon attaches the daemon used by server monitoring.
func (s *Server) SetDaemon(d *daemon.Daemon) {
	s.daemon = d
}

// Broadcaster returns the event broadcaster for external use.
func (s *Server) Broadcaster() *events.Broadcaster {
	return s.broadcaster
}

// Publisher returns the event publisher for enriched event delivery.
func (s *Server) Publisher() *events.Publisher {
	return s.publisher
}

// StartMonitoring starts server health and lifecycle monitoring.
func (s *Server) StartMonitoring(ctx context.Context) {
	// No gateway self-watchdog and no channel health monitor: both were
	// false-positive prone in a single-user native deployment. The self-watchdog
	// caused frequent spurious restarts, and the channel health monitor became
	// vestigial once channel plugins were removed (PR #1922) — the native client
	// is the sole surface, has nothing to "restart", and silence is normal user
	// idle, not a fault. Liveness is reported on demand via /health instead.

	// Memory pressure monitor — tick every 30s, emit a compact snapshot when
	// the Go heap is large or Linux PSI memory indicates host-level pressure.
	// Motivation: diary 4/24 notes earlyoom SIGTERM-ing the gateway ~100 times
	// in a day (host load avg 7+, memory 114/121GB). The gateway had zero
	// warning before being killed — this monitor turns that surprise into a
	// trailing breadcrumb the operator can correlate with the next OOM.
	s.safeGo("memory-pressure-monitor", func() { runMemPressureMonitor(ctx, s.logger) })
}

// runMemPressureMonitor ticks every 30s and emits a compact memory snapshot
// when the Go heap is unusually large or Linux PSI reports stall time.
//
// Snapshot conditions (any one triggers a log line):
//   - Go Alloc >= 6 GiB  — gateway's normal resident is < 1 GiB; 6× headroom
//     avoids noise during transient spikes but catches the runaway case.
//   - /proc/pressure/memory "some" 10s avg >= 1.0 %  — host is stalling on
//     memory for this process or its peers; OOM killer is a short step away.
//   - Retained heap (HeapSys - HeapReleased) grew > 2× since the last tick —
//     detect process-footprint growth before it hits the absolute threshold.
//     Alloc is intentionally not used for this comparison because its normal
//     GC sawtooth can more than double between adjacent samples.
//
// At every tick we also Debug-log Go runtime stats so a future `--log-level
// debug` restart can show the full curve without code changes.
func runMemPressureMonitor(ctx context.Context, logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}
	var previous memPressureSnapshot
	ticker := time.NewTicker(memPressureTickEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			psi := readPSIMemorySome()
			current := newMemPressureSnapshot(m, psi)
			// Debug line every tick for full-history trace when enabled.
			logger.Debug("mem pressure tick",
				"heapAlloc", m.HeapAlloc,
				"heapSys", m.HeapSys,
				"heapRetained", current.retainedHeap,
				"alloc", m.Alloc,
				"numGoroutine", runtime.NumGoroutine(),
				"psiSome10", psi)
			if shouldWarnMemPressure(previous, current) {
				logger.Warn("mem pressure",
					"alloc", m.Alloc,
					"heapAlloc", m.HeapAlloc,
					"heapInuse", m.HeapInuse,
					"heapSys", m.HeapSys,
					"heapReleased", m.HeapReleased,
					"heapRetained", current.retainedHeap,
					"gcPauseTotalNs", m.PauseTotalNs,
					"numGC", m.NumGC,
					"numGoroutine", runtime.NumGoroutine(),
					"psiSome10Pct", psi,
					"retainedGrowthFactor", safeGrowth(previous.retainedHeap, current.retainedHeap))
			}
			previous = current
		}
	}
}

const (
	memPressureTickEvery        = 30 * time.Second
	memPressureHeapWarnBytes    = uint64(6 * 1024 * 1024 * 1024) // 6 GiB
	memPressurePSIWarnPercent   = 1.0                            // 1 % stall
	memPressureGrowthFactorWarn = 2.0
	memPressureGrowthFloorBytes = uint64(512 * 1024 * 1024)
)

type memPressureSnapshot struct {
	alloc        uint64
	retainedHeap uint64
	psiSome10    float64
}

func newMemPressureSnapshot(m runtime.MemStats, psiSome10 float64) memPressureSnapshot {
	return memPressureSnapshot{
		alloc:        m.Alloc,
		retainedHeap: retainedHeapBytes(m.HeapSys, m.HeapReleased),
		psiSome10:    psiSome10,
	}
}

func retainedHeapBytes(heapSys, heapReleased uint64) uint64 {
	if heapReleased >= heapSys {
		return 0
	}
	return heapSys - heapReleased
}

func shouldWarnMemPressure(previous, current memPressureSnapshot) bool {
	return current.alloc >= memPressureHeapWarnBytes ||
		current.psiSome10 >= memPressurePSIWarnPercent ||
		(previous.retainedHeap > 0 &&
			float64(current.retainedHeap) >= memPressureGrowthFactorWarn*float64(previous.retainedHeap) &&
			current.retainedHeap > memPressureGrowthFloorBytes)
}

// readPSIMemorySome parses /proc/pressure/memory and returns the "some" 10s
// average percent. Returns 0 when the file is unavailable (non-Linux, kernel
// without PSI, or permission denied) — callers should treat 0 as "no signal".
func readPSIMemorySome() float64 {
	b, err := os.ReadFile("/proc/pressure/memory")
	if err != nil {
		return 0
	}
	// File format:
	//   some avg10=0.00 avg60=0.00 avg300=0.00 total=0
	//   full avg10=0.00 avg60=0.00 avg300=0.00 total=0
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(line, "some ") {
			continue
		}
		for _, field := range strings.Fields(line) {
			const key = "avg10="
			if strings.HasPrefix(field, key) {
				v, err := strconv.ParseFloat(field[len(key):], 64)
				if err != nil {
					return 0
				}
				return v
			}
		}
	}
	return 0
}

// safeGrowth guards against divide-by-zero for the first tick or a reset.
func safeGrowth(prev, current uint64) float64 {
	if prev == 0 {
		return 0
	}
	return float64(current) / float64(prev)
}

// registerBuiltinMethods registers the core RPC methods handled natively in Go.

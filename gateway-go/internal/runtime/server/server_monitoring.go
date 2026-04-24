package server

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/daemon"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/monitoring"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/events"
)

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

// GatewaySubscriptions returns the gateway event subscription manager
// for emitting agent, transcript, and lifecycle events.
func (s *Server) GatewaySubscriptions() *events.GatewayEventSubscriptions {
	return s.gatewaySubs
}

func (s *Server) StartMonitoring(ctx context.Context) {
	// Note: Gateway self-watchdog removed — it caused frequent false-positive
	// restarts in a single-user deployment. Channel health monitor below is
	// sufficient: it restarts individual stale channels without killing the
	// entire gateway process.

	// Channel health monitor — simplified for single Telegram channel.
	s.channelHealth = monitoring.NewChannelHealthMonitor(monitoring.ChannelHealthDeps{
		GetChannelStatus: func() string {
			if s.telegramPlug == nil {
				return "unknown"
			}
			st := s.telegramPlug.Status()
			if st.Connected {
				return "running"
			}
			if st.Error != "" {
				return "error"
			}
			return "stopped"
		},
		GetChannelLastEventAt: func() int64 {
			if s.channelEvents != nil {
				return s.channelEvents.LastEventAt()
			}
			return 0
		},
		GetChannelStartedAt: func() int64 {
			if s.telegramPlug != nil {
				return s.telegramPlug.StartedAt()
			}
			return 0
		},
		RestartChannel: func() error {
			if s.telegramPlug == nil {
				return fmt.Errorf("telegram not available")
			}
			s.logger.Info("restarting telegram via watchdog")
			restartCtx, restartCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer restartCancel()
			s.telegramPlug.Stop(restartCtx) //nolint:errcheck // best-effort cleanup before restart
			err := s.telegramPlug.Start(restartCtx)
			if err != nil {
				s.logger.Error("telegram restart failed", "error", err)
			} else {
				s.emitChannelEvent("telegram", "restarted")
			}
			return err
		},
	}, monitoring.DefaultChannelHealthConfig(), s.logger)
	s.safeGo("channel-health-monitor", func() { s.channelHealth.Run(ctx) })

	// Memory pressure monitor — tick every 30s, emit a compact snapshot when
	// the Go heap is large or Linux PSI memory indicates host-level pressure.
	// Motivation: diary 4/24 notes earlyoom SIGTERM-ing the gateway ~100 times
	// in a day (host load avg 7+, memory 114/121GB). The gateway had zero
	// warning before being killed — this monitor turns that surprise into a
	// trailing breadcrumb the operator can correlate with the next OOM.
	s.safeGo("memory-pressure-monitor", func() { runMemPressureMonitor(ctx, s.logger) })
}

// emitChannelEvent broadcasts a telegram.changed event to WebSocket clients.
func (s *Server) emitChannelEvent(channelID string, action string) {
	s.broadcaster.Broadcast("telegram.changed", map[string]any{
		"channelId": channelID,
		"action":    action,
		"ts":        time.Now().UnixMilli(),
	})
}

// startProcessPruner runs a background loop that periodically prunes completed
// processes older than 1 hour to prevent unbounded memory growth.
func (s *Server) startProcessPruner(ctx context.Context) {
	if s.processes == nil {
		return
	}
	s.safeGo("process-pruner", func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pruned := s.processes.Prune(1 * time.Hour)
				if pruned > 0 {
					s.logger.Info("pruned completed processes", "count", pruned)
				}
			}
		}
	})
}

// runMemPressureMonitor ticks every 30s and emits a compact memory snapshot
// when the Go heap is unusually large or Linux PSI reports stall time.
//
// Snapshot conditions (any one triggers a log line):
//   - Go Alloc >= 6 GiB  — gateway's normal resident is < 1 GiB; 6× headroom
//     avoids noise during transient spikes but catches the runaway case.
//   - /proc/pressure/memory "some" 10s avg >= 1.0 %  — host is stalling on
//     memory for this process or its peers; OOM killer is a short step away.
//   - Heap grew > 2× since the last tick — detect the leak-in-progress curve
//     before it hits the absolute threshold.
//
// At every tick we also Debug-log Go runtime stats so a future `--log-level
// debug` restart can show the full curve without code changes.
func runMemPressureMonitor(ctx context.Context, logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}
	const (
		tickEvery        = 30 * time.Second
		heapWarnBytes    = uint64(6 * 1024 * 1024 * 1024) // 6 GiB
		psiWarnPercent   = 1.0                            // 1 % stall
		growthFactorWarn = 2.0
	)
	var prevAlloc uint64
	ticker := time.NewTicker(tickEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			psi := readPSIMemorySome()
			// Debug line every tick for full-history trace when enabled.
			logger.Debug("mem pressure tick",
				"heapAlloc", m.HeapAlloc,
				"heapSys", m.HeapSys,
				"alloc", m.Alloc,
				"numGoroutine", runtime.NumGoroutine(),
				"psiSome10", psi)
			shouldWarn := m.Alloc >= heapWarnBytes ||
				psi >= psiWarnPercent ||
				(prevAlloc > 0 && float64(m.Alloc) >= growthFactorWarn*float64(prevAlloc) && m.Alloc > 512*1024*1024)
			if shouldWarn {
				logger.Warn("mem pressure",
					"alloc", m.Alloc,
					"heapAlloc", m.HeapAlloc,
					"heapInuse", m.HeapInuse,
					"heapSys", m.HeapSys,
					"gcPauseTotalNs", m.PauseTotalNs,
					"numGC", m.NumGC,
					"numGoroutine", runtime.NumGoroutine(),
					"psiSome10Pct", psi,
					"growthFactor", safeGrowth(prevAlloc, m.Alloc))
			}
			prevAlloc = m.Alloc
		}
	}
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

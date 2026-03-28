package server

import (
	"context"
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/daemon"
	"github.com/choiceoh/deneb/gateway-go/internal/events"
	"github.com/choiceoh/deneb/gateway-go/internal/hooks"
	"github.com/choiceoh/deneb/gateway-go/internal/monitoring"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc"
	"github.com/choiceoh/deneb/gateway-go/internal/vega"
)

func (s *Server) SetDaemon(d *daemon.Daemon) {
	s.daemon = d
}

// SetVega sets the Vega backend and registers its RPC methods.
func (s *Server) SetVega(backend vega.Backend) {
	s.vegaBackend = backend
	rpc.RegisterVegaMethods(s.dispatcher, rpc.VegaDeps{Backend: backend})
	// Late-bind Vega backend into core tool deps so the vega chat tool works.
	if s.toolDeps != nil {
		s.toolDeps.VegaBackend = backend
	}
}

// Broadcaster returns the event broadcaster for external use.
func (s *Server) Broadcaster() *events.Broadcaster {
	return s.broadcaster
}

// GatewaySubscriptions returns the gateway event subscription manager
// for emitting agent, heartbeat, transcript, and lifecycle events.
func (s *Server) GatewaySubscriptions() *events.GatewayEventSubscriptions {
	return s.gatewaySubs
}

// registerPhase2Methods registers chat, config, monitoring, and event subscription methods.

func (s *Server) StartMonitoring(ctx context.Context) {
	// Gateway self-watchdog.
	s.watchdog = monitoring.NewWatchdog(monitoring.WatchdogDeps{
		IsServerListening: func() bool { return s.ready.Load() },
		GetExpectedChannelCount: func() int {
			return len(s.channels.List())
		},
		GetConnectedChannelCount: func() int {
			count := 0
			statusAll := s.channels.StatusAll()
			for _, st := range statusAll {
				if st.Connected {
					count++
				}
			}
			return count
		},
		OnRestartNeeded: func(reason string) {
			s.logger.Warn("watchdog restart requested, sending SIGUSR1", "reason", reason)
			// Send SIGUSR1 to self to trigger graceful restart via main's signal handler.
			if p, err := os.FindProcess(os.Getpid()); err == nil {
				_ = p.Signal(syscall.SIGUSR1)
			}
		},
	}, monitoring.DefaultWatchdogConfig(), s.logger)
	s.safeGo("watchdog", func() { s.watchdog.Run(ctx) })

	// Channel health monitor.
	s.channelHealth = monitoring.NewChannelHealthMonitor(monitoring.ChannelHealthDeps{
		ListChannelIDs: func() []string {
			return s.channels.List()
		},
		GetChannelStatus: func(id string) string {
			ch := s.channels.Get(id)
			if ch == nil {
				return "unknown"
			}
			st := ch.Status()
			if st.Connected {
				return "running"
			}
			if st.Error != "" {
				return "error"
			}
			return "stopped"
		},
		GetChannelLastEventAt: func(id string) int64 {
			if s.channelEvents != nil {
				return s.channelEvents.LastEventAt(id)
			}
			return 0
		},
		GetChannelStartedAt: func(id string) int64 {
			if s.channelLifecycle != nil {
				return s.channelLifecycle.GetStartedAt(id)
			}
			return 0
		},
		RestartChannel: func(id string) error {
			if s.channelLifecycle == nil {
				return fmt.Errorf("channel lifecycle manager not available")
			}
			s.logger.Info("restarting channel via watchdog", "channel", id)
			restartCtx, restartCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer restartCancel()
			err := s.channelLifecycle.RestartChannel(restartCtx, id)
			if err != nil {
				s.logger.Error("channel restart failed", "channel", id, "error", err)
			} else {
				s.emitChannelEvent(id, hooks.EventChannelConnect, "restarted")
			}
			return err
		},
	}, monitoring.DefaultChannelHealthConfig(), s.logger)
	s.safeGo("channel-health-monitor", func() { s.channelHealth.Run(ctx) })
}

// emitChannelEvent fires the appropriate hook and broadcasts a channels.changed event.
func (s *Server) emitChannelEvent(channelID string, hookEvent hooks.Event, action string) {
	if s.hooks != nil {
		s.safeGo("hooks:"+string(hookEvent), func() {
			s.hooks.Fire(context.Background(), hookEvent, map[string]string{
				"DENEB_CHANNEL_ID": channelID,
			})
		})
	}
	s.broadcaster.Broadcast("channels.changed", map[string]any{
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
	go func() {
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
	}()
}

// registerBuiltinMethods registers the core RPC methods handled natively in Go.

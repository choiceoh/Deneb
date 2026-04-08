package server

import (
	"context"
	"fmt"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/daemon"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/monitoring"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/events"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/hooks"
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
// for emitting agent, heartbeat, transcript, and lifecycle events.
func (s *Server) GatewaySubscriptions() *events.GatewayEventSubscriptions {
	return s.gatewaySubs
}

// registerPhase2Methods registers chat, config, monitoring, and event subscription methods.

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
				s.emitChannelEvent("telegram", hooks.EventChannelConnect, "restarted")
			}
			return err
		},
	}, monitoring.DefaultChannelHealthConfig(), s.logger)
	s.safeGo("channel-health-monitor", func() { s.channelHealth.Run(ctx) })
}

// emitChannelEvent fires the internal hook and broadcasts a telegram.changed event.
func (s *Server) emitChannelEvent(channelID string, hookEvent hooks.Event, action string) {
	if s.internalHooks != nil {
		env := map[string]string{"DENEB_CHANNEL_ID": channelID}
		s.safeGo("internal-hooks:"+string(hookEvent), func() {
			s.internalHooks.TriggerFromEvent(context.Background(), hookEvent, "", env)
		})
	}
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

// registerBuiltinMethods registers the core RPC methods handled natively in Go.

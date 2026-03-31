package server

import (
	"context"
	"fmt"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/daemon"
	"github.com/choiceoh/deneb/gateway-go/internal/events"
	"github.com/choiceoh/deneb/gateway-go/internal/hooks"
	"github.com/choiceoh/deneb/gateway-go/internal/monitoring"
	handlerffi "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/ffi"
	"github.com/choiceoh/deneb/gateway-go/internal/vega"
)

func (s *Server) SetDaemon(d *daemon.Daemon) {
	s.daemon = d
}

// SetVega sets the Vega backend and registers its RPC methods.
func (s *Server) SetVega(backend vega.Backend) {
	s.vegaBackend = backend
	s.dispatcher.RegisterDomain(handlerffi.VegaMethods(handlerffi.VegaDeps{Backend: backend}))
	// Late-bind Vega backend into core tool deps so the vega chat tool works.
	if s.toolDeps != nil {
		s.toolDeps.Vega.Backend = backend
	}
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
		ListChannelIDs: func() []string {
			if s.telegramPlug != nil {
				return []string{"telegram"}
			}
			return nil
		},
		GetChannelStatus: func(id string) string {
			if id != "telegram" || s.telegramPlug == nil {
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
		GetChannelLastEventAt: func(id string) int64 {
			if s.channelEvents != nil {
				return s.channelEvents.LastEventAt(id)
			}
			return 0
		},
		GetChannelStartedAt: func(_ string) int64 {
			return 0
		},
		RestartChannel: func(id string) error {
			if id != "telegram" || s.telegramPlug == nil {
				return fmt.Errorf("channel %q not available", id)
			}
			s.logger.Info("restarting telegram via watchdog")
			restartCtx, restartCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer restartCancel()
			s.telegramPlug.Stop(restartCtx)
			err := s.telegramPlug.Start(restartCtx)
			if err != nil {
				s.logger.Error("telegram restart failed", "error", err)
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

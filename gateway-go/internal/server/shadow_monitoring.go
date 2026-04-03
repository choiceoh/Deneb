package server

import (
	"fmt"

	handlershadow "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/shadow"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/internal/shadow"
)

// initShadowMonitoring creates and starts the shadow session monitoring service.
// It derives the main session key from the Telegram channel config (single-user
// deployment: the first allowed chat ID is the main session).
func (s *Server) initShadowMonitoring(hub *rpcutil.GatewayHub) {
	// Derive main session key from Telegram config.
	var mainSessionKey string
	if s.telegramPlug != nil {
		tgCfg := s.telegramPlug.Config()
		if tgCfg != nil && len(tgCfg.AllowFrom.IDs) > 0 {
			mainSessionKey = fmt.Sprintf("telegram:%d", tgCfg.AllowFrom.IDs[0])
		}
	}
	if mainSessionKey == "" {
		s.logger.Info("shadow monitoring: skipped (no Telegram main session key)")
		return
	}

	// Build notifier (reuse Telegram notifier pattern).
	var notifier shadow.Notifier
	if s.telegramPlug != nil {
		tgCfg := s.telegramPlug.Config()
		if tgCfg != nil && len(tgCfg.AllowFrom.IDs) > 0 {
			notifier = &telegramNotifier{
				plugin: s.telegramPlug,
				chatID: tgCfg.AllowFrom.IDs[0],
				logger: s.logger,
			}
		}
	}

	s.shadowSvc = shadow.NewService(shadow.Config{
		MainSessionKey:   mainSessionKey,
		Sessions:         s.sessions,
		TranscriptWriter: s.transcript,
		Notifier:         notifier,
		Logger:           s.logger,
	})

	// Broadcast shadow events to WebSocket clients.
	s.shadowSvc.OnEvent(func(event shadow.ShadowEvent) {
		hub.Broadcast("shadow.event", event)
	})

	// Register shadow RPC methods (shadow.status, shadow.tasks, shadow.task.dismiss).
	s.dispatcher.RegisterDomain(handlershadow.Methods(handlershadow.Deps{
		Shadow: s.shadowSvc,
	}))

	s.shadowSvc.Start()
}

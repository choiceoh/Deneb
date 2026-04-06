package server

import (
	"context"
	"encoding/json"
	"time"

	handlerplatform "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/platform"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// registerAuthRPCMethods registers credential and channel-logout RPC methods.
func (s *Server) registerAuthRPCMethods() {
	// Secret resolution methods.
	s.dispatcher.RegisterDomain(handlerplatform.SecretMethods(handlerplatform.SecretDeps{
		Resolver: s.secrets,
	}))

	s.dispatcher.Register("telegram.logout", func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Channel string `json:"channel"`
		}
		if len(req.Params) > 0 {
			_ = json.Unmarshal(req.Params, &p)
		}
		if p.Channel == "" {
			return rpcerr.MissingParam("channel").Response(req.ID)
		}
		// Validate channel exists.
		if p.Channel != "telegram" || s.telegramPlug == nil {
			return rpcerr.Newf(protocol.ErrNotFound, "channel not found: %s", p.Channel).Response(req.ID)
		}
		// Stop the channel (logout = stop + clear).
		loggedOut := true
		if err := s.telegramPlug.Stop(ctx); err != nil {
			s.logger.Warn("telegram.logout: stop failed", "channel", p.Channel, "error", err)
			loggedOut = false
		}
		// Broadcast channel change event.
		if loggedOut {
			s.broadcaster.Broadcast("telegram.changed", map[string]any{
				"channelId": p.Channel,
				"action":    "logged_out",
				"ts":        time.Now().UnixMilli(),
			})
		}
		return protocol.MustResponseOK(req.ID, map[string]any{
			"ok":        true,
			"channel":   p.Channel,
			"loggedOut": loggedOut,
			"cleared":   loggedOut,
		})
	})
}

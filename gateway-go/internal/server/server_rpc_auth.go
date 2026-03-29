package server

import (
	"context"
	"encoding/json"
	"time"

	handlerplatform "github.com/choiceoh/deneb/gateway-go/internal/rpc/handler/platform"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// registerAuthRPCMethods registers credential, provider-auth, web-login stub, and
// channel-logout RPC methods.
func (s *Server) registerAuthRPCMethods() {
	// Secret resolution methods.
	s.dispatcher.RegisterDomain(handlerplatform.SecretMethods(handlerplatform.SecretDeps{
		Resolver: s.secrets,
	}))

	// Stub handlers for methods that required the removed Node.js bridge.
	// Registered explicitly so callers receive ErrUnavailable instead of
	// "unknown method", and RPC parity tests pass.
	stubUnavailable := func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return protocol.NewResponseError(req.ID, protocol.NewError(
			protocol.ErrUnavailable, req.Method+" not available (requires browser/web-login integration)"))
	}
	s.dispatcher.Register("browser.request", stubUnavailable)
	s.dispatcher.Register("web.login.start", stubUnavailable)
	s.dispatcher.Register("web.login.wait", stubUnavailable)
	s.dispatcher.Register("channels.logout", func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Channel string `json:"channel"`
		}
		if len(req.Params) > 0 {
			_ = json.Unmarshal(req.Params, &p)
		}
		if p.Channel == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "channel is required"))
		}
		// Validate channel exists.
		ch := s.channels.Get(p.Channel)
		if ch == nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrNotFound, "channel not found: "+p.Channel))
		}
		// Stop the channel (logout = stop + clear).
		loggedOut := true
		if s.channelLifecycle != nil {
			if err := s.channelLifecycle.StopChannel(ctx, p.Channel); err != nil {
				s.logger.Warn("channels.logout: stop failed", "channel", p.Channel, "error", err)
				loggedOut = false
			}
		}
		// Broadcast channel change event.
		if loggedOut {
			s.broadcaster.Broadcast("channels.changed", map[string]any{
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

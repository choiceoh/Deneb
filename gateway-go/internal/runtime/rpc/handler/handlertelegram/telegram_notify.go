// telegram_notify.go — telegram.notify_status RPC handler.
//
// Pushes a Korean summary of the current gateway state (running sessions,
// last outputs) to the secondary "monitoring" Telegram chat configured via
// telegram.notificationChatID. The handler is registered only when the
// notify service is wired in method_registry — when monitoring is disabled
// (no notification chat ID, no Telegram plugin), the method is omitted
// from the dispatcher entirely so callers receive METHOD_NOT_FOUND.
package handlertelegram

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
)

// NotifyDeps wires the snapshot-sender closure provided by the server. The
// handler does not import the server package or the notify service struct
// — Rule 3 (handlers never depend on internals).
type NotifyDeps struct {
	// SendStatusSnapshot composes and sends the current gateway status to
	// the monitoring chat. Returns (delivered=false, err=nil) when the
	// monitoring chat is configured but the plugin has no client yet
	// (during startup); the handler surfaces this as a soft Unavailable.
	SendStatusSnapshot func(ctx context.Context) (bool, error)
}

// NotifyMethods returns the telegram.notify_status RPC handler.
// Returns nil when the snapshot sender is not wired (monitoring disabled),
// so the dispatcher does not register the method.
func NotifyMethods(deps NotifyDeps) map[string]rpcutil.HandlerFunc {
	if deps.SendStatusSnapshot == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"telegram.notify_status": telegramNotifyStatus(deps),
	}
}

func telegramNotifyStatus(deps NotifyDeps) rpcutil.HandlerFunc {
	return rpcutil.BindHandlerCtx[struct{}](func(ctx context.Context, _ struct{}) (any, error) {
		delivered, err := deps.SendStatusSnapshot(ctx)
		if err != nil {
			return nil, rpcerr.WrapUnavailable("notify_status send failed", err).WithChannel("telegram")
		}
		return map[string]any{"delivered": delivered}, nil
	})
}

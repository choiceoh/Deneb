// Package handlertelegram provides RPC handlers for Telegram lifecycle
// (start/stop/restart) methods.
package handlertelegram

import (
	"context"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/events"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
)

// ---------------------------------------------------------------------------
// Deps structs
// ---------------------------------------------------------------------------

// LifecycleDeps holds the dependencies for Telegram lifecycle RPC methods
// (telegram.start, telegram.stop, telegram.restart).
type LifecycleDeps struct {
	TelegramPlugin *telegram.Plugin
	Broadcaster    *events.Broadcaster
}

// ---------------------------------------------------------------------------
// Method registries
// ---------------------------------------------------------------------------

// LifecycleMethods returns Telegram start/stop/restart RPC handlers.
// Returns nil if TelegramPlugin is not configured.
func LifecycleMethods(deps LifecycleDeps) map[string]rpcutil.HandlerFunc {
	if deps.TelegramPlugin == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"telegram.start":   telegramStart(deps),
		"telegram.stop":    telegramStop(deps),
		"telegram.restart": telegramRestart(deps),
	}
}

// ---------------------------------------------------------------------------
// Telegram lifecycle handlers
// ---------------------------------------------------------------------------

// emitTelegramLifecycleEvent broadcasts a telegram.changed event after a successful Telegram operation.
func emitTelegramLifecycleEvent(deps LifecycleDeps, id string, action string) {
	if deps.Broadcaster != nil {
		deps.Broadcaster.Broadcast("telegram.changed", map[string]any{
			"channelId": id,
			"action":    action,
			"ts":        time.Now().UnixMilli(),
		})
	}
}

func telegramStart(deps LifecycleDeps) rpcutil.HandlerFunc {
	type params struct {
		ID string `json:"id"`
	}
	return rpcutil.BindHandlerCtx[params](func(ctx context.Context, p params) (any, error) {
		if p.ID == "" {
			return nil, rpcerr.MissingParam("id")
		}
		if p.ID != "telegram" {
			return nil, rpcerr.Unavailable("channel " + p.ID + " not found").WithChannel(p.ID)
		}
		if err := deps.TelegramPlugin.Start(ctx); err != nil {
			return nil, rpcerr.WrapUnavailable("channel start failed", err).WithChannel(p.ID)
		}
		emitTelegramLifecycleEvent(deps, p.ID, "started")
		return map[string]any{"started": true, "id": p.ID}, nil
	})
}

func telegramStop(deps LifecycleDeps) rpcutil.HandlerFunc {
	type params struct {
		ID string `json:"id"`
	}
	return rpcutil.BindHandlerCtx[params](func(ctx context.Context, p params) (any, error) {
		if p.ID == "" {
			return nil, rpcerr.MissingParam("id")
		}
		if p.ID != "telegram" {
			return nil, rpcerr.Unavailable("channel " + p.ID + " not found").WithChannel(p.ID)
		}
		if err := deps.TelegramPlugin.Stop(ctx); err != nil {
			return nil, rpcerr.WrapUnavailable("channel stop failed", err).WithChannel(p.ID)
		}
		emitTelegramLifecycleEvent(deps, p.ID, "stopped")
		return map[string]any{"stopped": true, "id": p.ID}, nil
	})
}

func telegramRestart(deps LifecycleDeps) rpcutil.HandlerFunc {
	type params struct {
		ID string `json:"id"`
	}
	return rpcutil.BindHandlerCtx[params](func(ctx context.Context, p params) (any, error) {
		if p.ID == "" {
			return nil, rpcerr.MissingParam("id")
		}
		if p.ID != "telegram" {
			return nil, rpcerr.Unavailable("channel " + p.ID + " not found").WithChannel(p.ID)
		}
		deps.TelegramPlugin.Stop(ctx) //nolint:errcheck // best-effort cleanup before restart
		if err := deps.TelegramPlugin.Start(ctx); err != nil {
			return nil, rpcerr.WrapUnavailable("channel restart failed", err).WithChannel(p.ID)
		}
		emitTelegramLifecycleEvent(deps, p.ID, "restarted")
		return map[string]any{"restarted": true, "id": p.ID}, nil
	})
}

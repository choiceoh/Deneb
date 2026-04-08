// telegram_status.go — telegram.list, telegram.get, telegram.status,
// telegram.health RPC handlers (read-only queries).
package handlertelegram

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// StatusDeps holds dependencies for Telegram status/query RPC methods.
type StatusDeps struct {
	TelegramPlugin *telegram.Plugin
	SnapshotStore  *telegram.SnapshotStore
}

// StatusMethods returns Telegram read-only query handlers.
func StatusMethods(deps StatusDeps) map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"telegram.list":   telegramList(deps),
		"telegram.get":    telegramGet(deps),
		"telegram.status": telegramStatus(deps),
		"telegram.health": telegramHealth(deps),
	}
}

func telegramList(deps StatusDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var channels []string
		if deps.TelegramPlugin != nil {
			channels = []string{"telegram"}
		}
		return rpcutil.RespondOK(req.ID, channels)
	}
}

func telegramGet(deps StatusDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			ID string `json:"id"`
		}](req)
		if errResp != nil {
			return errResp
		}
		if p.ID == "" {
			return rpcerr.MissingParam("id").Response(req.ID)
		}
		if p.ID != "telegram" || deps.TelegramPlugin == nil {
			return rpcerr.NotFound("channel").
				WithChannel(rpcutil.TruncateForError(p.ID)).
				Response(req.ID)
		}
		plug := deps.TelegramPlugin
		return rpcutil.RespondOK(req.ID, map[string]any{
			"id":           plug.ID(),
			"capabilities": plug.Capabilities(),
			"status":       plug.Status(),
		})
	}
}

func telegramStatus(deps StatusDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if deps.SnapshotStore != nil {
			return rpcutil.RespondOK(req.ID, deps.SnapshotStore.Snapshot())
		}
		if deps.TelegramPlugin != nil {
			return rpcutil.RespondOK(req.ID, map[string]telegram.Status{
				"telegram": deps.TelegramPlugin.Status(),
			})
		}
		return rpcutil.RespondOK(req.ID, map[string]telegram.Status{})
	}
}

func telegramHealth(deps StatusDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if deps.TelegramPlugin == nil {
			return rpcutil.RespondOK(req.ID, map[string]any{"channels": []any{}})
		}
		status := deps.TelegramPlugin.Status()
		return rpcutil.RespondOK(req.ID, map[string]any{
			"channels": []map[string]any{{
				"id":        "telegram",
				"connected": status.Connected,
				"error":     status.Error,
			}},
		})
	}
}

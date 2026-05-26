// chat.go — miniapp.chat.send RPC handler.
//
// Wraps the chat handler's synchronous SendSync entry point so the Mini App
// can act as a simple Q&A surface: the operator types in a webview, gets one
// complete answer back. Streaming token-by-token UX is a clean follow-up
// against the existing SendSyncStream helper; this PR keeps the call
// pattern boringly synchronous to land the surface fast.
//
// Session key behavior: callers may pass an explicit sessionKey (to continue
// a previous Mini App conversation, or to share a session with another
// channel). When omitted the handler derives one from the InitData user ID
// — `miniapp:<userId>` — so successive sends from the same Telegram user
// fall into the same multi-turn session by default.

package handlerminiapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// ChatSender is the subset of *chat.Handler the miniapp chat handler needs.
// Tests inject a fake; production wires hub.Chat() directly.
type ChatSender interface {
	SendSync(ctx context.Context, sessionKey, message, model string, opts *chat.SyncOptions) (*chat.SyncResult, error)
}

// ChatDeps groups the dependencies. Sender is lazy because hub.Chat() is
// populated during the session phase right before late-phase registration;
// the factory defers the lookup so the gateway boots cleanly even on the
// brief window where chat.Handler is still nil.
type ChatDeps struct {
	Sender func() (ChatSender, error)
}

const (
	maxChatMessageRunes = 8000
)

// ErrChatUnavailable is the sentinel callers (method_registry) return from
// the Sender factory when chat init has not populated hub.Chat() yet.
// Handler maps it to UNAVAILABLE so the Mini App can show a "백엔드 준비 중"
// banner instead of a generic failure.
var ErrChatUnavailable = errors.New("chat handler not yet initialized")

// ChatMethods returns the miniapp.chat.* handler map. Returns nil when no
// sender factory is provided so method_registry can register conditionally.
func ChatMethods(deps ChatDeps) map[string]rpcutil.HandlerFunc {
	if deps.Sender == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"miniapp.chat.send": chatSend(deps),
	}
}

func chatSend(deps ChatDeps) rpcutil.HandlerFunc {
	type params struct {
		Message    string `json:"message"`
		SessionKey string `json:"sessionKey,omitempty"`
		Model      string `json:"model,omitempty"`
	}
	type out struct {
		SessionKey   string `json:"sessionKey"`
		Response     string `json:"response"`
		Model        string `json:"model,omitempty"`
		StopReason   string `json:"stopReason,omitempty"`
		DurationMs   int64  `json:"durationMs"`
		InputTokens  int    `json:"inputTokens,omitempty"`
		OutputTokens int    `json:"outputTokens,omitempty"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		var p params
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return rpcerr.InvalidParams(err).Response(req.ID)
			}
		}
		message := strings.TrimSpace(p.Message)
		if message == "" {
			return rpcerr.MissingParam("message").Response(req.ID)
		}
		// Rune-aware cap so a Korean essay doesn't get cut mid-character
		// upstream. The cap is generous (~8K characters) but bounded so a
		// runaway paste doesn't blow up the agent context.
		if count := utf8.RuneCountInString(message); count > maxChatMessageRunes {
			return rpcerr.InvalidRequest(fmt.Sprintf(
				"message exceeds %d characters (got %d)", maxChatMessageRunes, count,
			)).Response(req.ID)
		}

		sessionKey := strings.TrimSpace(p.SessionKey)
		if sessionKey == "" {
			data := telegram.InitDataFromContext(ctx)
			if data == nil || data.User == nil {
				return rpcerr.New(protocol.ErrUnauthorized,
					"cannot derive session key without authenticated user").Response(req.ID)
			}
			sessionKey = fmt.Sprintf("miniapp:%d", data.User.ID)
		}

		sender, err := deps.Sender()
		if err != nil {
			return rpcerr.WrapUnavailable("chat handler unavailable", err).Response(req.ID)
		}

		start := time.Now()
		result, err := sender.SendSync(ctx, sessionKey, message, p.Model, nil)
		duration := time.Since(start).Milliseconds()
		if err != nil {
			return rpcerr.WrapUnavailable("chat send failed", err).Response(req.ID)
		}
		if result == nil {
			return rpcerr.Unavailable("chat returned no result").Response(req.ID)
		}

		// Prefer Text (final assistant text), fall back to AllText
		// (transcript including tool calls) when the run produced no clean
		// terminating message. AllText is rarely the right thing to show
		// users but is better than an empty response.
		response := result.Text
		if response == "" {
			response = result.AllText
		}

		return rpcutil.RespondOK(req.ID, out{
			SessionKey:   sessionKey,
			Response:     response,
			Model:        result.Model,
			StopReason:   result.StopReason,
			DurationMs:   duration,
			InputTokens:  result.InputTokens,
			OutputTokens: result.OutputTokens,
		})
	}
}

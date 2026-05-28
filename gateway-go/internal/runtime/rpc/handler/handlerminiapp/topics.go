// Package handlerminiapp — topics.go exposes the "create a forum topic"
// RPC the Mini App's home menu calls. Telegram owns the topic data (we
// don't keep a deneb-side topic store); this handler is a thin wrapper
// around the Bot API's createForumTopic that injects the active-home
// chat ID and surfaces permission errors in Korean.
package handlerminiapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// TopicCreator is the subset of *telegram.Client this handler needs. A
// tiny interface so the test can swap in a fake without standing up the
// full HTTP-backed client.
type TopicCreator interface {
	CreateForumTopic(ctx context.Context, chatID int64, name string, iconColor int) (*telegram.ForumTopic, error)
}

// TopicsDeps wires the create-topic handler. Both fields are factories
// so the handler can register at boot time but reach into services that
// finish initializing later (chat phase, telegram plugin connect).
// ActiveHomeChatID returns 0 when no migration has occurred — the
// handler then refuses with VALIDATION_FAILED so the Mini App can
// explain the user has to run /use-forum first. Using a plain func
// (rather than an interface returning a struct) keeps handlerminiapp
// free of any infra/appsettings import.
type TopicsDeps struct {
	Creator          func() (TopicCreator, error)
	ActiveHomeChatID func() int64
}

// TopicsMethods returns the miniapp.topics.* handler map. Returns nil
// when neither factory is configured so the gateway doesn't register an
// always-failing endpoint.
func TopicsMethods(deps TopicsDeps) map[string]rpcutil.HandlerFunc {
	if deps.Creator == nil || deps.ActiveHomeChatID == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"miniapp.topics.create": topicsCreate(deps),
	}
}

// Telegram-supplied forum topic icon colors (decimal RGB). The Bot API
// rejects any other value; surfacing this as a guard turns a confusing
// 400 from upstream into a clear MissingParam at our boundary.
var allowedIconColors = map[int]struct{}{
	7322096:  {},
	16766590: {},
	13338331: {},
	9367192:  {},
	16749490: {},
	16478047: {},
}

// topicsCreate handles miniapp.topics.create. Required body: {name}.
// Optional: {iconColor}. The chat ID is pulled from the active home
// (set by /use-forum) so the Mini App doesn't have to track it.
func topicsCreate(deps TopicsDeps) rpcutil.HandlerFunc {
	type params struct {
		Name      string `json:"name"`
		IconColor int    `json:"iconColor,omitempty"`
	}
	type result struct {
		MessageThreadID int64  `json:"messageThreadId"`
		Name            string `json:"name"`
		IconColor       int    `json:"iconColor,omitempty"`
		ChatID          int64  `json:"chatId"`
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
		name := strings.TrimSpace(p.Name)
		if name == "" {
			return rpcerr.MissingParam("name").Response(req.ID)
		}
		// Telegram's hard limit is 128 chars; clamp early so a malformed
		// 10MB payload doesn't make it out the network.
		if len(name) > 128 {
			return rpcerr.New(protocol.ErrInvalidRequest,
				"topic name too long (max 128 chars)").Response(req.ID)
		}
		if p.IconColor != 0 {
			if _, ok := allowedIconColors[p.IconColor]; !ok {
				return rpcerr.New(protocol.ErrInvalidRequest,
					fmt.Sprintf("iconColor %d is not one of Telegram's six allowed values", p.IconColor)).Response(req.ID)
			}
		}

		chatID := deps.ActiveHomeChatID()
		if chatID == 0 {
			return rpcerr.ValidationFailed(
				"active home not configured — run /use-forum in the target supergroup first").Response(req.ID)
		}

		creator, err := deps.Creator()
		if err != nil {
			return rpcerr.WrapUnavailable("telegram client unavailable", err).Response(req.ID)
		}

		topic, err := creator.CreateForumTopic(ctx, chatID, name, p.IconColor)
		if err != nil {
			// Most common: the bot lost Manage Topics. The Telegram
			// error message is plain English; we forward as-is rather
			// than swallow so the Mini App can show why the create
			// failed without the user having to dig through logs.
			return rpcerr.WrapDependencyFailed("createForumTopic failed", err).Response(req.ID)
		}
		if topic == nil {
			return rpcerr.WrapDependencyFailed("createForumTopic returned empty result",
				errors.New("nil topic")).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, result{
			MessageThreadID: topic.MessageThreadID,
			Name:            topic.Name,
			IconColor:       topic.IconColor,
			ChatID:          chatID,
		})
	}
}

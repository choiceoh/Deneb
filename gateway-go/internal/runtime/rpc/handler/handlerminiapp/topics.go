// topics.go — miniapp.topics.* RPC handlers.
//
// "Topics" in the Mini App are Telegram forum topics in the supergroup the
// operator migrated into via /use-forum. Creating one from the Mini App
// saves the user from switching to Telegram's three-dot menu just to spin
// up a new thread.
//
// Today we only expose create — list lives in miniapp.sessions.recent
// (which surfaces the topic IDs each session attaches to), and Telegram
// doesn't offer a "list all topics in a supergroup" Bot API at all.

package handlerminiapp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// TopicsClient is the subset of *telegram.Client the topics handler needs.
// Defined here so tests can drop in a fake without booting the real client.
type TopicsClient interface {
	CreateForumTopic(ctx context.Context, chatID int64, name string, iconColor int64) (*telegram.ForumTopic, error)
}

// TopicsDeps wires the topics handler. Client is a lazy factory so we boot
// fine when the Telegram plugin is missing; ActiveChatID resolves the
// supergroup the operator migrated into via /use-forum (zero when no
// migration yet — handler surfaces a clear error in that case).
type TopicsDeps struct {
	Client       func() (TopicsClient, error)
	ActiveChatID func() int64
}

// maxTopicNameRunes mirrors Telegram's createForumTopic name limit so we
// fail fast with a clear error before the API rejects.
const maxTopicNameRunes = 128

// TopicsMethods returns the miniapp.topics.* handler map. Returns nil when
// no client factory is wired so method_registry can register conditionally.
func TopicsMethods(deps TopicsDeps) map[string]rpcutil.HandlerFunc {
	if deps.Client == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"miniapp.topics.create": topicsCreate(deps),
	}
}

// topicsCreate creates a new forum topic in the active supergroup.
//
// Parameters:
//   - name (required): topic title, 1-128 chars after trim.
//   - iconColor (optional): one of Telegram's allowed palette RGB ints;
//     0 lets Telegram pick the default.
//
// Response: the created topic's threadId + name + iconColor.
func topicsCreate(deps TopicsDeps) rpcutil.HandlerFunc {
	type params struct {
		Name      string `json:"name"`
		IconColor int64  `json:"iconColor,omitempty"`
	}
	type out struct {
		ThreadID  int64  `json:"threadId"`
		Name      string `json:"name"`
		IconColor int64  `json:"iconColor,omitempty"`
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
		if utf8.RuneCountInString(name) > maxTopicNameRunes {
			return rpcerr.ValidationFailed("topic name exceeds 128 characters").Response(req.ID)
		}

		var chatID int64
		if deps.ActiveChatID != nil {
			chatID = deps.ActiveChatID()
		}
		if chatID == 0 {
			return rpcerr.WrapUnavailable("no active forum supergroup",
				errors.New("run /use-forum in a supergroup first")).Response(req.ID)
		}
		// Forum topics only exist in supergroups (negative chat IDs);
		// catch the obvious misconfiguration before the API call.
		if chatID >= 0 {
			return rpcerr.WrapUnavailable("active chat is not a supergroup",
				errors.New("forum topics require a supergroup")).Response(req.ID)
		}

		client, err := deps.Client()
		if err != nil {
			return rpcerr.WrapUnavailable("telegram client unavailable", err).Response(req.ID)
		}

		topic, err := client.CreateForumTopic(ctx, chatID, name, p.IconColor)
		if err != nil {
			return rpcerr.WrapDependencyFailed("createForumTopic failed", err).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, out{
			ThreadID:  topic.MessageThreadID,
			Name:      topic.Name,
			IconColor: topic.IconColor,
		})
	}
}

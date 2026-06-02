// topics.go - miniapp.topics.* RPC handlers.
//
// The gateway still owns topic knowledge files and topic-to-session routing,
// while the Android app needs a native shape: display label plus client session
// key. These handlers adapt the existing topics.map config into that contract
// without exposing any transport-specific concept to the client.
package handlerminiapp

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"unicode"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

const defaultNativeTopicSessionKey = "client:main"

// NativeTopic is the native-client projection of a configured Deneb topic.
type NativeTopic struct {
	Key        string `json:"key"`
	Label      string `json:"label"`
	SessionKey string `json:"sessionKey"`
	SourceID   string `json:"sourceId,omitempty"`
	IsDefault  bool   `json:"isDefault"`
}

// TopicsDeps wires native topic discovery. TopicMap returns the configured
// topic key map lazily so deneb.json edits are visible without a restart.
type TopicsDeps struct {
	TopicMap func() (map[string]string, error)
}

// TopicsMethods returns the miniapp.topics.* handler map.
func TopicsMethods(deps TopicsDeps) map[string]rpcutil.HandlerFunc {
	if deps.TopicMap == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"miniapp.topics.list":    topicsList(deps),
		"miniapp.topics.resolve": topicsResolve(deps),
	}
}

func topicsList(deps TopicsDeps) rpcutil.HandlerFunc {
	type out struct {
		Topics            []NativeTopic `json:"topics"`
		DefaultSessionKey string        `json:"defaultSessionKey"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		topics, err := nativeTopicsFromDeps(deps)
		if err != nil {
			return rpcerr.WrapUnavailable("topics config unavailable", err).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, out{
			Topics:            topics,
			DefaultSessionKey: defaultNativeTopicSessionKey,
		})
	}
}

func topicsResolve(deps TopicsDeps) rpcutil.HandlerFunc {
	type params struct {
		Key        string `json:"key,omitempty"`
		SessionKey string `json:"sessionKey,omitempty"`
	}
	type out struct {
		Topic NativeTopic `json:"topic"`
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
		key := strings.TrimSpace(p.Key)
		sessionKey := strings.TrimSpace(p.SessionKey)
		if key == "" && sessionKey == "" {
			key = "main"
		}
		topics, err := nativeTopicsFromDeps(deps)
		if err != nil {
			return rpcerr.WrapUnavailable("topics config unavailable", err).Response(req.ID)
		}
		for _, topic := range topics {
			if key != "" && strings.EqualFold(topic.Key, key) {
				return rpcutil.RespondOK(req.ID, out{Topic: topic})
			}
			if sessionKey != "" && topic.SessionKey == sessionKey {
				return rpcutil.RespondOK(req.ID, out{Topic: topic})
			}
		}
		return rpcerr.New(protocol.ErrNotFound, "topic not found").Response(req.ID)
	}
}

func nativeTopicsFromDeps(deps TopicsDeps) ([]NativeTopic, error) {
	m, err := deps.TopicMap()
	if err != nil {
		return nil, err
	}
	return buildNativeTopics(m), nil
}

func buildNativeTopics(topicMap map[string]string) []NativeTopic {
	type row struct {
		sourceID string
		key      string
	}
	rows := make([]row, 0, len(topicMap))
	for sourceID, key := range topicMap {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		rows = append(rows, row{sourceID: strings.TrimSpace(sourceID), key: key})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].sourceID != rows[j].sourceID {
			return rows[i].sourceID < rows[j].sourceID
		}
		return rows[i].key < rows[j].key
	})

	topics := make([]NativeTopic, 0, len(rows)+1)
	seenSession := map[string]bool{}
	hasDefault := false
	for _, r := range rows {
		isDefault := isDefaultNativeTopic(r.sourceID, r.key)
		sessionKey := nativeTopicSessionKey(r.key, isDefault)
		if seenSession[sessionKey] {
			if isDefault {
				hasDefault = true
			}
			continue
		}
		topic := NativeTopic{
			Key:        r.key,
			Label:      nativeTopicLabel(r.key),
			SessionKey: sessionKey,
			SourceID:   r.sourceID,
			IsDefault:  isDefault,
		}
		topics = append(topics, topic)
		seenSession[sessionKey] = true
		if isDefault {
			hasDefault = true
		}
	}
	if !hasDefault {
		topics = append([]NativeTopic{{
			Key:        "main",
			Label:      "업무",
			SessionKey: defaultNativeTopicSessionKey,
			SourceID:   "0",
			IsDefault:  true,
		}}, topics...)
		seenSession[defaultNativeTopicSessionKey] = true
	}

	sort.SliceStable(topics, func(i, j int) bool {
		if topics[i].IsDefault != topics[j].IsDefault {
			return topics[i].IsDefault
		}
		if topics[i].Label != topics[j].Label {
			return topics[i].Label < topics[j].Label
		}
		return topics[i].Key < topics[j].Key
	})
	return topics
}

func isDefaultNativeTopic(sourceID, key string) bool {
	if strings.TrimSpace(sourceID) == "0" {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "main", "work", "general", "home":
		return true
	default:
		return false
	}
}

func nativeTopicSessionKey(key string, isDefault bool) string {
	if isDefault {
		return defaultNativeTopicSessionKey
	}
	slug := nativeTopicSlug(key)
	if slug == "" {
		slug = "topic"
	}
	return "client:" + slug
}

func nativeTopicLabel(key string) string {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "main", "work", "general", "home":
		return "업무"
	case "coding", "code", "dev":
		return "코딩"
	case "personal", "private":
		return "개인"
	case "chat", "casual":
		return "잡담"
	default:
		return strings.ReplaceAll(strings.TrimSpace(key), "_", " ")
	}
}

func nativeTopicSlug(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	var b strings.Builder
	lastDash := false
	for _, r := range key {
		keep := unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == '.'
		if keep {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

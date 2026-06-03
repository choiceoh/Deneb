// sessions.go — miniapp.sessions.recent RPC handler.
//
// Wraps session.Manager.List() and trims/sorts the result for the Mini
// App's "recent sessions" card. We deliberately do not call
// sessions.list (which exists for the broader RPC surface) — the Mini App
// only needs a small projection of each session and we want to keep the
// browser-origin response shape stable independently of the wider list
// method.

package handlerminiapp

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// SessionsLister is the subset of *session.Manager the handler needs.
// Tests provide a fake; production wires the real Manager.
type SessionsLister interface {
	List() []*session.Session
}

// TranscriptLoader is the subset of chat.TranscriptStore the transcript
// handler needs. Lets tests provide a fake without standing up file I/O.
type TranscriptLoader interface {
	Load(sessionKey string, limit int) ([]toolctx.ChatMessage, int, error)
}

// SessionsDeps holds the session list manager (required) and an optional
// lazy transcript factory. Transcripts is lazy because the underlying
// store is created during session-phase init; the factory pattern lets us
// register sessions.* in the early phase and still surface a meaningful
// UNAVAILABLE when the transcript path is unset.
type SessionsDeps struct {
	Manager     SessionsLister
	Transcripts func() (TranscriptLoader, error)
}

const (
	defaultSessionsLimit   = 10
	maxSessionsLimit       = 100
	defaultTranscriptLimit = 30
	maxTranscriptLimit     = 200
)

// SessionsMethods returns the miniapp.sessions.* handler map.
func SessionsMethods(deps SessionsDeps) map[string]rpcutil.HandlerFunc {
	if deps.Manager == nil {
		return nil
	}
	out := map[string]rpcutil.HandlerFunc{
		"miniapp.sessions.recent": sessionsRecent(deps),
	}
	// Transcript registration is conditional — without a transcript
	// loader factory the gateway boots fine, the method just isn't
	// available.
	if deps.Transcripts != nil {
		out["miniapp.sessions.transcript"] = sessionsTranscript(deps)
	}
	return out
}

// sessionsTranscript returns the most recent N messages of a single
// session. The Mini App's session detail view renders these as a
// timeline; the rest of the chat history (compaction, system prompt,
// etc.) is intentionally excluded.
func sessionsTranscript(deps SessionsDeps) rpcutil.HandlerFunc {
	type params struct {
		SessionKey string `json:"sessionKey"`
		Limit      int    `json:"limit,omitempty"`
	}
	type attachmentOut struct {
		Type     string `json:"type,omitempty"`
		MimeType string `json:"mimeType,omitempty"`
		URL      string `json:"url,omitempty"`
		Data     string `json:"data,omitempty"`
		Name     string `json:"name,omitempty"`
		Size     int64  `json:"size,omitempty"`
	}
	type messageOut struct {
		ID          string          `json:"id,omitempty"`
		Role        string          `json:"role"`
		Content     string          `json:"content"`
		Attachments []attachmentOut `json:"attachments,omitempty"`
		TimestampMs int64           `json:"timestampMs,omitempty"`
	}
	type out struct {
		SessionKey string       `json:"sessionKey"`
		Messages   []messageOut `json:"messages"`
		Total      int          `json:"total"`
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
		key := strings.TrimSpace(p.SessionKey)
		if key == "" {
			return rpcerr.MissingParam("sessionKey").Response(req.ID)
		}
		limit := p.Limit
		if limit <= 0 {
			limit = defaultTranscriptLimit
		}
		if limit > maxTranscriptLimit {
			limit = maxTranscriptLimit
		}

		store, err := deps.Transcripts()
		if err != nil {
			return rpcerr.WrapUnavailable("transcript store unavailable", err).Response(req.ID)
		}
		msgs, total, err := store.Load(key, limit)
		if err != nil {
			return rpcerr.WrapUnavailable("transcript load failed", err).Response(req.ID)
		}

		rows := make([]messageOut, 0, len(msgs))
		for _, m := range msgs {
			var atts []attachmentOut
			for _, a := range m.Attachments {
				atts = append(atts, attachmentOut{
					Type:     a.Type,
					MimeType: a.MimeType,
					URL:      a.URL,
					Data:     a.Data,
					Name:     a.Name,
					Size:     a.Size,
				})
			}
			rows = append(rows, messageOut{
				ID:          m.ID,
				Role:        m.Role,
				Content:     decodeChatContent(m.Content),
				Attachments: atts,
				TimestampMs: m.Timestamp,
			})
		}
		return rpcutil.RespondOK(req.ID, out{
			SessionKey: key,
			Messages:   rows,
			Total:      total,
		})
	}
}

// decodeChatContent collapses ChatMessage.Content (which can be a plain
// JSON string or an array of ContentBlock-like objects) into a single
// display string. The Mini App's transcript view doesn't need the full
// structured form; we just want readable text per message.
func decodeChatContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	// Try plain string first — fast path covering most messages.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	// Array of blocks. We pull "text" fields and concatenate; tool
	// calls etc. are surfaced as a short tag so the user can see them
	// without rendering raw JSON.
	var blocks []map[string]any
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return string(raw)
	}
	var sb strings.Builder
	for i, b := range blocks {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		t, _ := b["type"].(string)
		switch t {
		case "text":
			if txt, ok := b["text"].(string); ok {
				sb.WriteString(txt)
			}
		case "tool_use":
			name, _ := b["name"].(string)
			sb.WriteString("⚙️ ")
			sb.WriteString(name)
		case "tool_result":
			content, _ := b["content"].(string)
			sb.WriteString("↩️ ")
			sb.WriteString(content)
		default:
			sb.WriteString("[")
			sb.WriteString(t)
			sb.WriteString("]")
		}
	}
	return sb.String()
}

func sessionsRecent(deps SessionsDeps) rpcutil.HandlerFunc {
	type params struct {
		Limit   int    `json:"limit,omitempty"`
		Channel string `json:"channel,omitempty"`
	}
	type rowOut struct {
		Key         string `json:"key"`
		Kind        string `json:"kind,omitempty"`
		Status      string `json:"status,omitempty"`
		Channel     string `json:"channel,omitempty"`
		Model       string `json:"model,omitempty"`
		Label       string `json:"label,omitempty"`
		UpdatedAtMs int64  `json:"updatedAtMs,omitempty"`
		StartedAtMs *int64 `json:"startedAtMs,omitempty"`
		RuntimeMs   *int64 `json:"runtimeMs,omitempty"`
		TotalTokens *int64 `json:"totalTokens,omitempty"`
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
		limit := p.Limit
		if limit <= 0 {
			limit = defaultSessionsLimit
		}
		if limit > maxSessionsLimit {
			limit = maxSessionsLimit
		}

		sessions := deps.Manager.List()

		// Filter by channel if requested.
		if p.Channel != "" {
			filtered := sessions[:0]
			for _, s := range sessions {
				if s.Channel == p.Channel {
					filtered = append(filtered, s)
				}
			}
			sessions = filtered
		}

		// Sort newest-first by UpdatedAt (UnixMilli). Sessions whose
		// UpdatedAt is zero fall to the back so they don't pollute the
		// fresh top of the list.
		sort.SliceStable(sessions, func(i, j int) bool {
			return sessions[i].UpdatedAt > sessions[j].UpdatedAt
		})
		if len(sessions) > limit {
			sessions = sessions[:limit]
		}

		out := make([]rowOut, 0, len(sessions))
		for _, s := range sessions {
			out = append(out, rowOut{
				Key:         s.Key,
				Kind:        string(s.Kind),
				Status:      string(s.Status),
				Channel:     s.Channel,
				Model:       s.Model,
				Label:       s.Label,
				UpdatedAtMs: s.UpdatedAt,
				StartedAtMs: s.StartedAt,
				RuntimeMs:   s.RuntimeMs,
				TotalTokens: s.TotalTokens,
			})
		}
		return rpcutil.RespondOK(req.ID, map[string]any{
			"sessions": out,
			"count":    len(out),
		})
	}
}

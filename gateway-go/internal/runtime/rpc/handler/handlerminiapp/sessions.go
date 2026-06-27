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
	"log/slog"
	"sort"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	handlerchat "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// SessionsLister is the subset of *session.Manager the handler needs:
// List backs the recent card, while Get+Delete let sessions.delete remove
// a session the user dismissed from the drawer (without them the row
// resurrects on the next sessions.recent fetch). Tests provide a fake;
// production wires the real Manager.
type SessionsLister interface {
	List() []*session.Session
	Get(key string) *session.Session
	Delete(key string) bool
}

// TranscriptLoader is the subset of chat.TranscriptStore the session
// handlers need: Load for the transcript view, Delete so sessions.delete
// can remove a dismissed conversation's history for good rather than just
// hiding the live row. Lets tests provide a fake without standing up file I/O.
type TranscriptLoader interface {
	Load(sessionKey string, limit int) ([]toolctx.ChatMessage, int, error)
	Delete(sessionKey string) error
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
		"miniapp.sessions.delete": sessionsDelete(deps),
	}
	// Transcript registration is conditional — without a transcript
	// loader factory the gateway boots fine, the method just isn't
	// available.
	if deps.Transcripts != nil {
		out["miniapp.sessions.transcript"] = sessionsTranscript(deps)
	}
	return out
}

// Wire shapes (package-scoped + marked for Kotlin codegen so the native client
// shares them — see generated/MiniappWireTypes.kt). sessions.recent returns
// {sessions: []sessionRowOut, count}; sessions.transcript wraps []transcriptMsgOut.

//deneb:wire
type sessionRowOut struct {
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

type transcriptAttachmentOut struct {
	Type     string `json:"type,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	URL      string `json:"url,omitempty"`
	Data     string `json:"data,omitempty"`
	Name     string `json:"name,omitempty"`
	Size     int64  `json:"size,omitempty"`
}

//deneb:wire
type transcriptMsgOut struct {
	ID          string                    `json:"id,omitempty"`
	Role        string                    `json:"role"`
	Content     string                    `json:"content"`
	Attachments []transcriptAttachmentOut `json:"attachments,omitempty"`
	TimestampMs int64                     `json:"timestampMs,omitempty"`
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
	type out struct {
		SessionKey string             `json:"sessionKey"`
		Messages   []transcriptMsgOut `json:"messages"`
		Total      int                `json:"total"`
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
		if strings.TrimSpace(p.SessionKey) == "" {
			return rpcerr.MissingParam("sessionKey").Response(req.ID)
		}
		key, err := handlerchat.NormalizeMiniappSessionKey(p.SessionKey)
		if err != nil {
			return rpcerr.InvalidParams(err).Response(req.ID)
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

		// Display-only sanitation, same pipeline as chat.history: hide
		// link-enrichment appendages from user bubbles, drop tool_result
		// messages so raw tool output (ps dumps, command stdout) never renders
		// as a quotable bubble, and strip the baked "[<RFC3339>] " timestamp
		// prefix so user bubbles show what was typed. The stored transcript is
		// untouched. This RPC is what the native client actually loads its
		// timeline from, so the strip must live here, not only on chat.history.
		msgs = toolctx.StripLinkEnrichmentForDisplay(msgs)
		msgs = toolctx.StripToolResultBlocksForDisplay(msgs)
		msgs = toolctx.StripUserMessageTimestampsForDisplay(msgs)
		// Read Sino-Korean Hanja in assistant prose as Hangul (報告書 → 보고서) —
		// Chinese-lineage models sometimes emit it. Display-only; transcript intact.
		msgs = toolctx.TransliterateAssistantTextForDisplay(msgs)

		rows := make([]transcriptMsgOut, 0, len(msgs))
		for _, m := range msgs {
			var atts []transcriptAttachmentOut
			for _, a := range m.Attachments {
				atts = append(atts, transcriptAttachmentOut{
					Type:     a.Type,
					MimeType: a.MimeType,
					URL:      a.URL,
					Data:     a.Data,
					Name:     a.Name,
					Size:     a.Size,
				})
			}
			content := decodeChatContent(m.Content)
			if content == "" && len(atts) == 0 {
				// Tool/thinking-only message — nothing a bubble can show.
				continue
			}
			rows = append(rows, transcriptMsgOut{
				ID:          m.ID,
				Role:        m.Role,
				Content:     content,
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
// display string. Only text blocks render: tool_use/tool_result/thinking
// blocks are turn machinery, not conversation — the live UI already showed
// tool activity as transient status rows, and raw tool output must never
// come back as a quotable bubble on reload (tool_result is additionally
// stripped upstream; skipping it here keeps the decode safe on its own).
func decodeChatContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	// Try plain string first — fast path covering most messages.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var blocks []map[string]any
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return string(raw)
	}
	var parts []string
	for _, b := range blocks {
		t, _ := b["type"].(string)
		switch t {
		case "text":
			if txt, ok := b["text"].(string); ok && txt != "" {
				parts = append(parts, txt)
			}
		case "tool_use", "tool_result", "thinking", "redacted_thinking":
			// Internal turn machinery — never bubble content.
		default:
			// Unknown block type: a content-free tag so the bubble shows
			// *something* was here without rendering raw JSON.
			parts = append(parts, "["+t+"]")
		}
	}
	return strings.Join(parts, "\n\n")
}

func sessionsRecent(deps SessionsDeps) rpcutil.HandlerFunc {
	type params struct {
		Limit   int    `json:"limit,omitempty"`
		Channel string `json:"channel,omitempty"`
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
		filtered := sessions[:0]
		for _, s := range sessions {
			if _, err := handlerchat.NormalizeMiniappSessionKey(s.Key); err == nil {
				filtered = append(filtered, s)
			}
		}
		sessions = filtered

		// Filter by channel if requested.
		if p.Channel != "" {
			filtered = sessions[:0]
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

		out := make([]sessionRowOut, 0, len(sessions))
		for _, s := range sessions {
			out = append(out, sessionRowOut{
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

// sessionsDelete removes a session the user dismissed (the × in the drawer's
// session selector). It drops the in-memory session AND deletes its transcript.
// Both are required: although session.Manager is itself a pure in-memory map,
// the gateway's startup restore (Server.restoreAndWakeSessions) rebuilds
// sessions by SCANNING the transcript dir — so a dismissed row whose .jsonl
// survives resurrects on the next restart (and the gateway restarts every few
// minutes on SIGUSR1). Deleting the transcript is the difference between
// "hidden until restart" and "gone for good"; an earlier comment here wrongly
// claimed "no disk restore", which was the exact blind spot that left zombies.
//
// A running session is left intact unless force=true: yanking it out from under
// an in-flight turn would let that run re-Set the session on completion, so the
// row would just come back — a subtler resurrection than the one we're fixing.
func sessionsDelete(deps SessionsDeps) rpcutil.HandlerFunc {
	type params struct {
		SessionKey string `json:"sessionKey"`
		Force      bool   `json:"force,omitempty"`
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
		if strings.TrimSpace(p.SessionKey) == "" {
			return rpcerr.MissingParam("sessionKey").Response(req.ID)
		}
		key, err := handlerchat.NormalizeMiniappSessionKey(p.SessionKey)
		if err != nil {
			return rpcerr.InvalidParams(err).Response(req.ID)
		}

		if s := deps.Manager.Get(key); s != nil && s.Status == session.StatusRunning && !p.Force {
			return rpcutil.RespondOK(req.ID, map[string]bool{"deleted": false})
		}

		deleted := deps.Manager.Delete(key)

		// Transcript removal is best-effort for the RPC result (the manager
		// delete already cleared the live row the drawer reads from), but a
		// failure is not silent: per the doc comment, a surviving .jsonl
		// resurrects this row on the next restart, so surface it as a warning
		// rather than swallowing it with `_ =`.
		if deps.Transcripts != nil {
			if store, err := deps.Transcripts(); err == nil && store != nil {
				if delErr := store.Delete(key); delErr != nil {
					slog.Error("miniapp.sessions.delete: transcript removal failed; session may resurrect on restart",
						"sessionKey", key, "error", delErr)
				}
			}
		}

		return rpcutil.RespondOK(req.ID, map[string]bool{"deleted": deleted})
	}
}

// sessions.go — miniapp.sessions.recent RPC handler.
//
// Wraps session.Manager.List() and trims/sorts the result for the Mini
// App's "recent sessions" card. We deliberately do not call
// sessions.list (which exists for the broader RPC surface) — the Mini App
// only needs a small projection of each session and we want to keep the
// browser-origin response shape stable independently of the wider list
// method.

package sessions

import (
	"context"
	"encoding/json"
	"log/slog"
	"sort"
	"strconv"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/core/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	miniappcontract "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp/contract"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/minibind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

type (
	sessionRowOut           = miniappcontract.SessionRowOut
	transcriptAttachmentOut = miniappcontract.TranscriptAttachmentOut
	transcriptToolTraceOut  = miniappcontract.TranscriptToolTraceOut
	transcriptMsgOut        = miniappcontract.TranscriptMsgOut
	sessionSearchHitOut     = miniappcontract.SessionSearchHitOut
	sessionSearchResult     = miniappcontract.SessionSearchResult
	sessionFocusResult      = miniappcontract.SessionFocusResult
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
	// Patch backs miniapp.sessions.rename (Label field). The label persistence
	// sweep (server/session_labels.go) snapshots manager labels, so a rename
	// here survives restarts with no extra plumbing.
	Patch(key string, patch session.PatchFields) *session.Session
}

// TranscriptLoader is the subset of chat.TranscriptStore the session
// handlers need: Load for the transcript view, Delete so sessions.delete
// can remove a dismissed conversation's history for good rather than just
// hiding the live row. Lets tests provide a fake without standing up file I/O.
type TranscriptLoader interface {
	Load(sessionKey string, limit int) ([]toolport.ChatMessage, int, error)
	Delete(sessionKey string) error
	Search(query string, maxResults int) ([]toolport.SearchResult, error)
}

// SessionsDeps holds the session list manager (required) and an optional
// lazy transcript factory. Transcripts is lazy because the underlying
// store is created during session-phase init; the factory pattern lets us
// register sessions.* in the early phase and still surface a meaningful
// UNAVAILABLE when the transcript path is unset.
type SessionsDeps struct {
	Manager     SessionsLister
	Transcripts func() (TranscriptLoader, error)
	// OnForgotten drops per-session sidecars (model override, list pin) so a
	// deleted conversation cannot resurrect its extras on the next sweep.
	OnForgotten func(sessionKey string)
	Focus       func() string
	SetFocus    func(sessionKey string) error
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
		"miniapp.sessions.rename": sessionsRename(deps),
		"miniapp.sessions.pin":    sessionsPin(deps),
		"miniapp.sessions.focus":  sessionsFocus(deps),
	}
	// Transcript registration is conditional — without a transcript
	// loader factory the gateway boots fine, the method just isn't
	// available.
	if deps.Transcripts != nil {
		out["miniapp.sessions.transcript"] = sessionsTranscript(deps)
		out["miniapp.sessions.search"] = sessionsSearch(deps)
	}
	return out
}

// syntheticTraceAnchor prefixes the request-local message anchors stamped in
// sessionsTranscript. The NUL byte keeps it impossible as a real stored ID.
const syntheticTraceAnchor = "\x00trace:"

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
		SessionKey  string             `json:"sessionKey"`
		Messages    []transcriptMsgOut `json:"messages"`
		Total       int                `json:"total"`
		TurnRunning bool               `json:"turnRunning"`
	}
	return minibind.BindOptional[params](func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
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

		// A session that asked not to see reasoning (ShowThinkingInChat) must not
		// get it back on reload either — the live stream already withholds it.
		showReasoning := true
		if sess := deps.Manager.Get(key); sess != nil && sess.ShowThinkingInChat != nil {
			showReasoning = *sess.ShowThinkingInChat
		}

		// Display-only sanitation, same pipeline as chat.history: hide
		// link-enrichment appendages from user bubbles, drop tool_result
		// messages so raw tool output (ps dumps, command stdout) never renders
		// as a quotable bubble, and strip the baked "[<RFC3339>] " timestamp
		// prefix so user bubbles show what was typed. The stored transcript is
		// untouched. This RPC is what the native client actually loads its
		// timeline from, so the strip must live here, not only on chat.history.
		// Rebuild tool chips from the raw tool_use/tool_result pairs BEFORE the
		// strips drop those blocks — a restored conversation keeps the same
		// tool activity (and the same gateway-authored digests) the live
		// stream showed. Two ground truths shape the mechanics: stored
		// messages carry no ID (the store keys by index), so we stamp a
		// request-local anchor; and store.Load can hand back a cache-shared
		// slice, so we stamp (and run the in-place display strips) on a
		// value copy, never on the shared elements.
		work := make([]toolport.ChatMessage, len(msgs))
		copy(work, msgs)
		for i := range work {
			if work[i].ID == "" {
				work[i].ID = syntheticTraceAnchor + strconv.Itoa(i)
			}
		}
		traces := toolport.CollectToolTraces(work)
		work = toolport.StripLinkEnrichmentForDisplay(work)
		work = toolport.StripToolResultBlocksForDisplay(work)
		work = toolport.StripUserMessageTimestampsForDisplay(work)
		// Read Sino-Korean Hanja in assistant prose as Hangul (報告書 → 보고서) —
		// Chinese-lineage models sometimes emit it. Display-only; transcript intact.
		work = toolport.TransliterateAssistantTextForDisplay(work)

		rows := make([]transcriptMsgOut, 0, len(work))
		for _, m := range work {
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
			var trace []transcriptToolTraceOut
			for _, item := range traces[m.ID] {
				trace = append(trace, transcriptToolTraceOut{
					Tool:    item.Tool,
					Detail:  item.Detail,
					Summary: item.Summary,
					Preview: item.Preview,
					IsError: item.IsError,
				})
			}
			content := decodeChatContent(m.Content)
			if content == "" && len(atts) == 0 && len(trace) == 0 {
				// Tool/thinking-only message with no rebuilt chips — nothing
				// a bubble can show. (A tool_use-only assistant step DOES
				// survive now: its chips are the content.)
				continue
			}
			// The stamped anchor is request-local bookkeeping, not a message
			// ID — never let it out on the wire.
			id := m.ID
			if strings.HasPrefix(id, syntheticTraceAnchor) {
				id = ""
			}
			reasoning := ""
			if showReasoning {
				reasoning = decodeThinkingContent(m.Content)
			}
			rows = append(rows, transcriptMsgOut{
				ID:          id,
				Role:        m.Role,
				Content:     content,
				Reasoning:   reasoning,
				Attachments: atts,
				TimestampMs: m.Timestamp,
				ToolTrace:   trace,
			})
		}
		turnRunning := false
		if s := deps.Manager.Get(key); s != nil && s.Status == session.StatusRunning {
			turnRunning = true
		}
		return rpcutil.RespondOK(req.ID, out{
			SessionKey:  key,
			Messages:    rows,
			Total:       total,
			TurnRunning: turnRunning,
		})
	})
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
			// Internal turn machinery — never bubble content. Thinking is surfaced
			// separately via decodeThinkingContent (as the reasoning block), not here.
		default:
			// Unknown block type: a content-free tag so the bubble shows
			// *something* was here without rendering raw JSON.
			parts = append(parts, "["+t+"]")
		}
	}
	return strings.Join(parts, "\n\n")
}

// decodeThinkingContent extracts the assistant reasoning from a persisted
// message's content blocks — the "thinking" blocks decodeChatContent drops as
// turn machinery — so a reloaded conversation can show the expandable reasoning
// block. Empty for plain-string (user) content and messages with no thinking.
func decodeThinkingContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var blocks []map[string]any
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return ""
	}
	var parts []string
	for _, b := range blocks {
		if t, _ := b["type"].(string); t != "thinking" {
			continue
		}
		// The Korean display copy the run persisted alongside the block wins:
		// the raw reasoning is whatever language the model wrote in, and a
		// reopened conversation should read the way it read live. Absent for
		// pre-2026-09 rows and for turns whose translation was refused.
		if meta, ok := b["metadata"].(map[string]any); ok {
			if display, ok := meta["ko"].(string); ok && strings.TrimSpace(display) != "" {
				parts = append(parts, display)
				continue
			}
		}
		if txt, ok := b["thinking"].(string); ok && txt != "" {
			parts = append(parts, txt)
		} else if txt, ok := b["text"].(string); ok && txt != "" {
			parts = append(parts, txt)
		}
	}
	return strings.Join(parts, "\n\n")
}

// effectiveChannel classifies a conversation for the drawer's channel scoping.
// A row created by a live run carries no Channel (Manager.Create takes only key
// and kind), while the SAME conversation carries the key-derived one once a
// restart restores it from its transcript (server/session_restore.go). A
// channel-scoped fetch therefore saw a conversation only after a restart — the
// desktop chat list, which scopes to "client", came up completely empty for
// everything started since boot. Fall back to restore's own classifier so both
// spellings of one conversation agree.
func effectiveChannel(s *session.Session) string {
	if s.Channel != "" {
		return s.Channel
	}
	channel, _ := session.RestorableTranscriptChannel(s.Key)
	return channel
}

func sessionsRecent(deps SessionsDeps) rpcutil.HandlerFunc {
	type params struct {
		Limit          int    `json:"limit,omitempty"`
		Offset         int    `json:"offset,omitempty"`
		Channel        string `json:"channel,omitempty"`
		ExcludeChannel string `json:"excludeChannel,omitempty"`
	}
	return minibind.BindOptional[params](func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
		limit := p.Limit
		if limit <= 0 {
			limit = defaultSessionsLimit
		}
		if limit > maxSessionsLimit {
			limit = maxSessionsLimit
		}
		offset := p.Offset
		if offset < 0 {
			offset = 0
		}

		sessions := deps.Manager.List()

		// Filter by channel if requested.
		if p.Channel != "" {
			filtered := sessions[:0]
			for _, s := range sessions {
				if effectiveChannel(s) == p.Channel {
					filtered = append(filtered, s)
				}
			}
			sessions = filtered
		}
		if p.ExcludeChannel != "" {
			filtered := make([]*session.Session, 0, len(sessions))
			for _, s := range sessions {
				if effectiveChannel(s) != p.ExcludeChannel {
					filtered = append(filtered, s)
				}
			}
			sessions = filtered
		}

		// Pinned conversations stay at the top; within each group, newest
		// UpdatedAt wins. Zero UpdatedAt falls to the back of its group.
		sort.SliceStable(sessions, func(i, j int) bool {
			if sessions[i].Pinned != sessions[j].Pinned {
				return sessions[i].Pinned
			}
			return sessions[i].UpdatedAt > sessions[j].UpdatedAt
		})

		// `total` is the size of the whole (filtered) set, before the page is
		// cut. Without it a client cannot tell "that is all of them" from "the
		// cap truncated you" — and since conversations are no longer GC'd (#4353)
		// the set grows without bound, so a limit-only API silently hides every
		// conversation past maxSessionsLimit.
		total := len(sessions)
		if offset >= total {
			sessions = nil
		} else {
			sessions = sessions[offset:]
			if len(sessions) > limit {
				sessions = sessions[:limit]
			}
		}

		out := make([]sessionRowOut, 0, len(sessions))
		for _, s := range sessions {
			out = append(out, sessionRowOut{
				Key:         s.Key,
				Kind:        string(s.Kind),
				Status:      string(s.Status),
				Channel:     effectiveChannel(s),
				Model:       s.Model,
				Label:       s.Label,
				Pinned:      s.Pinned,
				UpdatedAtMs: s.UpdatedAt,
				StartedAtMs: s.StartedAt,
				RuntimeMs:   s.RuntimeMs,
				TotalTokens: s.TotalTokens,
				// Same liveness test the single-session transcript reply uses,
				// so a row and the conversation it opens never disagree. Read
				// off the listed snapshot rather than a second Manager lookup:
				// that keeps the whole page consistent with one another.
				TurnRunning: s.Status == session.StatusRunning,
			})
		}
		payload := map[string]any{
			"sessions": out,
			"count":    len(out),
			"offset":   offset,
			"total":    total,
		}
		if deps.Focus != nil {
			payload["focus"] = deps.Focus()
		}
		return rpcutil.RespondOK(req.ID, payload)
	})
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
// sessionsRename sets a conversation's display label from the drawer's rename
// affordance. Truncated to the same budget as auto-titles so the drawer row
// never overflows; persistence rides the existing label sweep.
func sessionsRename(deps SessionsDeps) rpcutil.HandlerFunc {
	type params struct {
		SessionKey string `json:"sessionKey"`
		Label      string `json:"label"`
	}
	return minibind.BindOptional[params](func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
		key := strings.TrimSpace(p.SessionKey)
		if key == "" {
			return rpcerr.MissingParam("sessionKey").Response(req.ID)
		}
		label := strings.TrimSpace(p.Label)
		if label == "" {
			return rpcerr.MissingParam("label").Response(req.ID)
		}
		if r := []rune(label); len(r) > 60 {
			label = string(r[:60])
		}
		if deps.Manager.Get(key) == nil {
			return rpcutil.RespondOK(req.ID, map[string]any{"renamed": false})
		}
		// Pin the label: a user-chosen name must never be overwritten by the
		// background auto-titler's periodic re-title (session_autotitle.go).
		pinned := true
		s := deps.Manager.Patch(key, session.PatchFields{Label: &label, LabelPinned: &pinned})
		return rpcutil.RespondOK(req.ID, map[string]any{"renamed": s != nil, "label": label})
	})
}

func sessionsDelete(deps SessionsDeps) rpcutil.HandlerFunc {
	type params struct {
		SessionKey string `json:"sessionKey"`
		Force      bool   `json:"force,omitempty"`
	}
	return minibind.BindOptional[params](func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
		key := strings.TrimSpace(p.SessionKey)
		if key == "" {
			return rpcerr.MissingParam("sessionKey").Response(req.ID)
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
		if deps.OnForgotten != nil {
			deps.OnForgotten(key)
		}

		return rpcutil.RespondOK(req.ID, map[string]bool{"deleted": deleted})
	})
}

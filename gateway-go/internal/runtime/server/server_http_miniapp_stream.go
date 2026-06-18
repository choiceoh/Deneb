// server_http_miniapp_stream.go — Server-Sent Events (SSE) variant of the
// miniapp chat bridge. The standalone native client posts one chat turn and
// receives the assistant text token-by-token instead of waiting for the full
// reply (which the blocking miniapp.chat.send RPC returns in one shot).
//
// Pipeline:
//
//	POST /api/v1/miniapp/chat/stream
//	  X-Deneb-Client-Token: <token>
//	  Body: {"sessionKey"?, "message", "model"?}
//	    │
//	    ▼  same auth as handleMiniappRPC
//	  chat.Handler.SendSyncStream(... onDelta + tool/thinking callbacks)
//	    │
//	    ▼  each stream event → one SSE frame
//	  event: delta     data: {"delta":"..."}                        (zero or more)
//	  event: tool      data: {"state":"started"|"completed","tool":"...","toolUseId":"...",
//	                          "detail":"..."?, "isError":bool?}
//	  event: thinking  data: {"preview":"..."?}                     (throttled liveness +
//	                          chip-sized tail of the live reasoning text)
//	  event: done      data: {"text":...,"model":...,"fellBack":...}   (success terminal)
//	  event: error     data: {"error":"..."}                        (failure terminal)
//
// The native client renders deltas live, shows tool/thinking progress in its
// waiting indicator, and replaces the message with the canonical `done.text`
// on completion. Unknown event names are ignored by the client's SSE parser,
// so older clients degrade gracefully.

package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	handlerchat "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/chat"
)

// maxMiniappChatStreamBodyBytes caps the POST /api/v1/miniapp/chat/stream body.
// This endpoint carries only {sessionKey, message, model, skipRecall} — plain
// text — so a few MiB is ample headroom even for a long pasted message, while
// still stopping an unbounded io.ReadAll. Captures (base64 blobs) go through the
// RPC endpoint, which has the larger maxMiniappRPCBodyBytes.
const maxMiniappChatStreamBodyBytes = 8 << 20 // 8 MiB

// chatStreamKeepaliveInterval bounds how long the SSE connection may sit silent
// during a long tool call that emits no text. A periodic comment frame keeps
// intermediaries (cloudflared, nginx) from idling the connection out.
const chatStreamKeepaliveInterval = 15 * time.Second

// chatStreamResult is the terminal payload of a streamed chat turn.
type chatStreamResult struct {
	Text     string
	Model    string
	FellBack bool
}

// chatStreamSinks carries the per-event callbacks writeChatStreamSSE hands to
// the runner: text deltas, tool lifecycle transitions, and thinking liveness.
// All callbacks are safe for the runner to invoke concurrently (writes are
// serialized by the SSE writer's mutex).
type chatStreamSinks struct {
	Delta    func(delta string)
	Tool     func(ev chat.ToolStreamEvent)
	Thinking func(preview string)
}

// toolStreamFrame is the wire payload of one SSE "tool" frame. Detail and
// isError are omitted when zero so the common frames stay minimal.
type toolStreamFrame struct {
	State     string `json:"state"`
	Tool      string `json:"tool"`
	ToolUseID string `json:"toolUseId"`
	Detail    string `json:"detail,omitempty"`
	IsError   bool   `json:"isError,omitempty"`
}

// thinkingStreamFrame is the wire payload of one SSE "thinking" frame. Preview
// is a chip-sized tail of the live reasoning text; omitted while empty so the
// bare liveness pulse stays minimal (and older clients ignore it either way).
type thinkingStreamFrame struct {
	Preview string `json:"preview,omitempty"`
}

// chatStreamRunner runs a streaming chat turn, invoking the sink callbacks as
// stream events arrive and returning the final result. It is the seam that
// lets writeChatStreamSSE be unit-tested without a live chat handler.
type chatStreamRunner func(ctx context.Context, sinks chatStreamSinks) (*chatStreamResult, error)

// handleMiniappChatStream runs one chat turn for the native client and streams
// the assistant text back as SSE. Auth and the session/channel wiring mirror
// the blocking miniapp.chat.send bridge.
func (s *Server) handleMiniappChatStream(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.authenticateMiniappRequest(w, r)
	if !ok {
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxMiniappChatStreamBodyBytes))
	if err != nil {
		status := http.StatusBadRequest
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			status = http.StatusRequestEntityTooLarge
		}
		s.writeJSON(w, status, map[string]any{"error": "read body: " + err.Error()})
		return
	}
	var reqBody struct {
		SessionKey string `json:"sessionKey"`
		Message    string `json:"message"`
		Model      string `json:"model"`
		// SkipRecall is the native client's "focused chat / memory off" toggle:
		// skip the long-term-memory recall preflight for this turn (faster, no
		// unrelated work-context injection). Persona unchanged. Default false.
		SkipRecall bool `json:"skipRecall"`
	}
	if err := json.Unmarshal(body, &reqBody); err != nil {
		s.writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid body: " + err.Error()})
		return
	}
	if strings.TrimSpace(reqBody.Message) == "" {
		s.writeJSON(w, http.StatusBadRequest, map[string]any{"error": "missing message"})
		return
	}
	sessionKey := handlerchat.DefaultSessionKey(reqBody.SessionKey)
	if s.chatHandler == nil {
		s.writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "chat handler not ready"})
		return
	}

	// Bound concurrent interactive turns (unified-memory OOM guard). Acquired
	// before any SSE byte so an over-limit caller still gets a clean JSON 503.
	// Held for the whole turn via the deferred release.
	release, err := s.chatHandler.AcquireInteractiveTurn(r.Context())
	if err != nil {
		s.writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "gateway busy: too many concurrent turns"})
		return
	}
	defer release()

	// From here on the response is SSE — no more writeJSON.
	ctx := clientauth.WithContext(r.Context(), identity)
	runner := func(ctx context.Context, sinks chatStreamSinks) (*chatStreamResult, error) {
		res, err := s.chatHandler.SendSyncStream(ctx, sessionKey, reqBody.Message, strings.TrimSpace(reqBody.Model), &chat.SyncOptions{
			Delivery: &chat.DeliveryContext{Channel: handlerchat.NativeClientChannel, To: sessionKey},
			// The reply text is streamed here, not pushed via the message tool.
			AutoDeliveredOutput: true,
			SkipRecall:          reqBody.SkipRecall,
			// Block irreversible tools (exec, gmail send) if promptware enters the turn.
			GateUntrustedTools: true,
			// Live progress for the client's waiting indicator: which tool is
			// running, and a throttled "thinking" pulse before the first token.
			OnToolEvent: sinks.Tool,
			OnThinking:  sinks.Thinking,
		}, sinks.Delta)
		if err != nil {
			return nil, err
		}
		// BestText (not res.Text) so a tool wrap-up final turn — e.g. the agent
		// writing its answer to the wiki and closing with "위키에 기록했습니다" —
		// doesn't replace the streamed body in the client's done frame.
		return &chatStreamResult{Text: res.BestText(), Model: res.Model, FellBack: res.FellBack}, nil
	}
	writeChatStreamSSE(ctx, w, sessionKey, runner, s.logger)
}

// writeChatStreamSSE drives one chat turn and serializes its output as SSE.
// All writes go through one mutex because a keepalive ticker emits comment
// frames concurrently with the delta callbacks. The keepalive goroutine is
// joined before the terminal frame so it can never write after this returns.
func writeChatStreamSSE(ctx context.Context, w http.ResponseWriter, sessionKey string, run chatStreamRunner, logger *slog.Logger) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		// net/http's ResponseWriter is always a Flusher; this only trips with a
		// non-streaming test double. Fail loudly rather than buffer silently.
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	// A streamed turn runs up to DefaultTurnDeadline; lift the global WriteTimeout.
	disableWriteDeadline(w)

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no") // disable proxy buffering (cloudflared/nginx)
	h.Set("Server", "deneb-gateway")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	var mu sync.Mutex
	writeEvent := func(event string, payload any) {
		mu.Lock()
		defer mu.Unlock()
		// Best-effort: a client disconnect makes these writes fail. We ignore the
		// error and rely on the canceled ctx to stop the underlying run.
		_, _ = io.WriteString(w, "event: "+event+"\n")
		data, err := json.Marshal(payload)
		if err != nil {
			data = []byte("{}")
		}
		_, _ = io.WriteString(w, "data: ")
		_, _ = w.Write(data)
		_, _ = io.WriteString(w, "\n\n")
		flusher.Flush()
	}

	// Keepalive ticker: comment frames during silent stretches (long tool
	// calls). Bounded by stop/ctx and joined below; recover keeps a stray write
	// panic from taking down the process (see .claude/rules/concurrency.md).
	stop := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		defer func() {
			if rec := recover(); rec != nil && logger != nil {
				logger.Error("panic in chat stream keepalive", "session", sessionKey, "panic", rec)
			}
		}()
		ticker := time.NewTicker(chatStreamKeepaliveInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				mu.Lock()
				_, _ = io.WriteString(w, ": keepalive\n\n")
				flusher.Flush()
				mu.Unlock()
			}
		}
	}()

	result, runErr := run(ctx, chatStreamSinks{
		Delta: func(delta string) {
			if delta == "" {
				return
			}
			writeEvent("delta", map[string]string{"delta": delta})
		},
		Tool: func(ev chat.ToolStreamEvent) {
			if ev.Tool == "" {
				return
			}
			writeEvent("tool", toolStreamFrame{
				State:     ev.State,
				Tool:      ev.Tool,
				ToolUseID: ev.ToolUseID,
				Detail:    ev.Detail,
				IsError:   ev.IsError,
			})
		},
		Thinking: func(preview string) {
			writeEvent("thinking", thinkingStreamFrame{Preview: preview})
		},
	})

	// Stop and join the keepalive before the terminal frame so no comment can
	// interleave after "done"/"error" and nothing writes once this returns.
	close(stop)
	<-stopped

	switch {
	case runErr != nil:
		writeEvent("error", map[string]string{"error": runErr.Error()})
		if logger != nil {
			logger.Warn("miniapp chat stream failed", "session", sessionKey, "error", runErr)
		}
	case result == nil:
		writeEvent("error", map[string]string{"error": "empty result"})
	default:
		writeEvent("done", map[string]any{
			"text":     result.Text,
			"model":    result.Model,
			"fellBack": result.FellBack,
		})
	}
}

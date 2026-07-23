// miniapp chat SSE stream (formerly server_http_miniapp_stream.go).
// The standalone native client posts one chat turn and
// receives the assistant text token-by-token instead of waiting for the full
// reply (which the blocking miniapp.chat.send RPC returns in one shot).
//// Pipeline:
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
//	  event: reasoning data: {"reasoning":"..."}                    (throttled full
//	                          reasoning-so-far → client's live expandable block)
//	  event: done      data: {"text":...,"model":...,"fellBack":...,"reasoning":...}  (success terminal)
//	  event: error     data: {"error":"..."}                        (failure terminal)
//
// The native client renders deltas live, shows tool/thinking progress in its
// waiting indicator, and replaces the message with the canonical `done.text`
// on completion. Unknown event names are ignored by the client's SSE parser,
// so older clients degrade gracefully.

package nativeapi

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
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/nativeauth"
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

// chatStreamTurnDeadline hard-caps a DETACHED streamed turn so a run that
// outlives its client connection can never run forever. It sits just above the
// chat pipeline's own turn deadline (server.DefaultTurnDeadline = 5m; not
// imported here to avoid a server→nativeapi→server cycle) so the run's own
// deadline fires first with a cleaner error and this is only a backstop.
const chatStreamTurnDeadline = chatport.InteractiveTurnDeadline

// chatStreamResult is the terminal payload of a streamed chat turn.
type chatStreamResult struct {
	Text     string
	Model    string
	FellBack bool
	// Reasoning is the turn's accumulated chain-of-thought (empty when none),
	// carried on the done frame so the client can show an expandable reasoning
	// block for the just-completed answer without a transcript re-fetch.
	Reasoning string
}

// chatStreamSinks carries the per-event callbacks writeChatStreamSSE hands to
// the runner: text deltas, tool lifecycle transitions, and thinking liveness.
// All callbacks are safe for the runner to invoke concurrently (writes are
// serialized by the SSE writer's mutex).
type chatStreamSinks struct {
	Delta     func(delta string)
	Tool      func(ev chatport.ToolStreamEvent)
	Thinking  func(preview string)
	Reasoning func(full string)
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
func (s *Handler) ChatStream(w http.ResponseWriter, r *http.Request) {
	identity, ok := nativeauth.Authenticate(w, r, s.logger)
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
	if s.chatHandler == nil || !s.chatHandler.ChatReady() {
		s.writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "chat handler not ready"})
		return
	}

	// From here on the response is SSE — no more writeJSON.
	//
	// DETACH the agent run from the HTTP request lifecycle. The native client
	// backgrounding (Android closes the SSE socket) cancels r.Context(); that
	// must NOT kill the in-flight turn — the reported "background → answer lost,
	// session ends" bug. The run instead rides the server shutdown context with
	// a turn-deadline backstop, so it COMPLETES and persists to the session
	// transcript even after the client drops; the client re-fetches it on resume.
	// The live connection (r.Context()) still governs streaming + keepalive, and
	// an explicit user stop aborts via the chat.abort RPC (CancelBySessionKey) —
	// the connection teardown no longer stands in for it.
	runCtx, cancelRun := context.WithTimeout(clientauth.WithContext(s.shutdownContext, identity), chatStreamTurnDeadline)
	defer cancelRun()
	runner := func(ctx context.Context, sinks chatStreamSinks) (*chatStreamResult, error) {
		res, err := s.chatHandler.RunSyncStream(ctx, chatport.SyncRequest{
			SessionKey: sessionKey,
			Message:    reqBody.Message,
			Model:      strings.TrimSpace(reqBody.Model),
			Delivery:   &chatport.DeliveryContext{Channel: handlerchat.NativeClientChannel, To: sessionKey},
			// The reply text is streamed here, not pushed via the message tool.
			AutoDeliveredOutput: true,
			SkipRecall:          reqBody.SkipRecall,
			// Block irreversible tools (exec, gmail send) if promptware enters the turn.
			GateUntrustedTools: true,
			// Live progress for the client's waiting indicator: which tool is
			// running, and a throttled "thinking" pulse before the first token.
			OnToolEvent: sinks.Tool,
			OnThinking:  sinks.Thinking,
			OnReasoning: sinks.Reasoning,
		}, sinks.Delta)
		if err != nil {
			return nil, err
		}
		// BestText (not res.Text) so a tool wrap-up final turn — e.g. the agent
		// writing its answer to the wiki and closing with "위키에 기록했습니다" —
		// doesn't replace the streamed body in the client's done frame.
		return &chatStreamResult{Text: res.BestText, Model: res.Model, FellBack: res.FellBack, Reasoning: res.Thinking}, nil
	}
	writeChatStreamSSE(runCtx, r.Context(), w, sessionKey, runner, s.logger)
}

// writeChatStreamSSE drives one chat turn and serializes its output as SSE.
// All writes go through one mutex because a keepalive ticker emits comment
// frames concurrently with the delta callbacks. The keepalive goroutine is
// joined before the terminal frame so it can never write after this returns.
//
// ctx runs the turn (detached from the client connection, so a background
// disconnect doesn't kill it); connCtx tracks the live client connection so the
// keepalive stops the moment the client drops. Writes after a disconnect fail
// harmlessly (best-effort) while the detached run finishes and persists.
func writeChatStreamSSE(ctx, connCtx context.Context, w http.ResponseWriter, sessionKey string, run chatStreamRunner, logger *slog.Logger) {
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
	// panic from taking down the process (see docs/agent-rules/concurrency.md).
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
			case <-connCtx.Done():
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
		Tool: func(ev chatport.ToolStreamEvent) {
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
		Reasoning: func(full string) {
			if full == "" {
				return
			}
			// Full reasoning-so-far: the client replaces its live expandable block
			// with this on each throttled frame (the chip still uses `thinking`).
			writeEvent("reasoning", map[string]string{"reasoning": full})
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
			"text":      result.Text,
			"model":     result.Model,
			"fellBack":  result.FellBack,
			"reasoning": result.Reasoning,
		})
	}
}

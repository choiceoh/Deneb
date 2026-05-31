// server_http_miniapp_stream.go — Server-Sent Events (SSE) variant of the
// miniapp chat bridge. The standalone native client posts one chat turn and
// receives the assistant text token-by-token instead of waiting for the full
// reply (which the blocking miniapp.chat.send RPC returns in one shot).
//
// Pipeline:
//
//	POST /api/v1/miniapp/chat/stream
//	  X-Deneb-Client-Token: <token>   (or Authorization: tma <initData>)
//	  Body: {"sessionKey"?, "message", "model"?}
//	    │
//	    ▼  same auth as handleMiniappRPC
//	  chat.Handler.SendSyncStream(... onDelta)
//	    │
//	    ▼  each text chunk → one SSE "delta" frame
//	  event: delta  data: {"delta":"..."}      (zero or more)
//	  event: done   data: {"text":...,"model":...,"fellBack":...}   (on success)
//	  event: error  data: {"error":"..."}      (on failure)
//
// The native client renders deltas live and replaces the message with the
// canonical `done.text` on completion.

package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
)

// nativeClientChannel mirrors the constant of the same name in the blocking
// miniapp chat bridge (internal/runtime/rpc/handler/chat/miniapp_bridge.go).
// Both must name the same channel so the chat pipeline's richUIChannel enables
// kai-ui emission for the native client; the streaming and blocking paths share
// one session and one channel. Kept in sync by hand — there is no shared export.
const nativeClientChannel = "client"

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

// chatStreamRunner runs a streaming chat turn, invoking onDelta for each text
// chunk and returning the final result. It is the seam that lets
// writeChatStreamSSE be unit-tested without a live chat handler.
type chatStreamRunner func(ctx context.Context, onDelta func(string)) (*chatStreamResult, error)

// handleMiniappChatStream runs one chat turn for the native client and streams
// the assistant text back as SSE. Auth and the session/channel wiring mirror
// the blocking miniapp.chat.send bridge.
func (s *Server) handleMiniappChatStream(w http.ResponseWriter, r *http.Request) {
	initData, ok := s.authenticateMiniappRequest(w, r)
	if !ok {
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.writeJSON(w, http.StatusBadRequest, map[string]any{"error": "read body: " + err.Error()})
		return
	}
	var reqBody struct {
		SessionKey string `json:"sessionKey"`
		Message    string `json:"message"`
		Model      string `json:"model"`
	}
	if err := json.Unmarshal(body, &reqBody); err != nil {
		s.writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid body: " + err.Error()})
		return
	}
	if strings.TrimSpace(reqBody.Message) == "" {
		s.writeJSON(w, http.StatusBadRequest, map[string]any{"error": "missing message"})
		return
	}
	sessionKey := strings.TrimSpace(reqBody.SessionKey)
	if sessionKey == "" {
		sessionKey = nativeClientChannel + ":main"
	}
	if s.chatHandler == nil {
		s.writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "chat handler not ready"})
		return
	}

	// From here on the response is SSE — no more writeJSON.
	ctx := telegram.WithInitDataContext(r.Context(), initData)
	runner := func(ctx context.Context, onDelta func(string)) (*chatStreamResult, error) {
		res, err := s.chatHandler.SendSyncStream(ctx, sessionKey, reqBody.Message, strings.TrimSpace(reqBody.Model), &chat.SyncOptions{
			// Channel "client" flips on kai-ui emission (richUIChannel).
			Delivery: &chat.DeliveryContext{Channel: nativeClientChannel, To: sessionKey},
			// The reply text is streamed here, not pushed via the message tool.
			AutoDeliveredOutput: true,
		}, onDelta)
		if err != nil {
			return nil, err
		}
		return &chatStreamResult{Text: res.Text, Model: res.Model, FellBack: res.FellBack}, nil
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

	result, runErr := run(ctx, func(delta string) {
		if delta == "" {
			return
		}
		writeEvent("delta", map[string]string{"delta": delta})
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

// Package nativeapi — HTTP bridge for the native-client RPC surface.
// Formerly server_http_miniapp.go.
//
// Pipeline:
//
//	POST /api/v1/miniapp/rpc
//	  X-Deneb-Client-Token: <secret>
//	  Body:                 protocol.RequestFrame (miniapp.* method)
//	    │
//	    ▼
//	  client-token verification (constant-time compare)
//	    │
//	    ▼
//	  Dispatcher.Dispatch(ctx + synthetic *clientauth.Identity, frame)
//	    │
//	    ▼
//	  protocol.ResponseFrame JSON
//
// The Telegram Mini App webview (which authenticated with signed initData) was
// retired; the standalone native client is now the only caller. It presents a
// static bearer secret in the X-Deneb-Client-Token header (see
// internal/infra/clientauth), and the server attaches a synthetic operator
// identity so downstream miniapp.* handlers are unchanged. The miniapp.* method
// name and route are kept for native-client wire compatibility.

package nativeapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/nativeauth"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// ClientTokenHeader is the public HTTP header used by native-client routes.
// Route middleware should reference this constant instead of duplicating the
// authentication package's wire name.
const ClientTokenHeader = clientauth.Header

// Authenticator binds the native client authentication adapter to a logger for
// sibling HTTP surfaces that share the same token contract.
func Authenticator(logger *slog.Logger) func(http.ResponseWriter, *http.Request) (*clientauth.Identity, bool) {
	return func(w http.ResponseWriter, r *http.Request) (*clientauth.Identity, bool) {
		return nativeauth.Authenticate(w, r, logger)
	}
}

// Config supplies the gateway-owned dependencies used by native HTTP handlers.
type Config struct {
	Dispatcher      *rpc.Dispatcher
	ChatHandler     chatport.SyncStreamRunner
	ShutdownContext context.Context
	Logger          *slog.Logger
	// TranslateThinking renders the turn's reasoning into Korean for the done
	// frame the native client renders as its expandable reasoning block. The
	// live `reasoning` deltas stay in the model's own language — the block
	// settles to Korean when the turn completes. Optional; nil disables.
	TranslateThinking func(ctx context.Context, text string) (string, bool)
}

// Handler serves the authenticated native-client HTTP surface.
type Handler struct {
	dispatcher        *rpc.Dispatcher
	chatHandler       chatport.SyncStreamRunner
	shutdownContext   context.Context
	logger            *slog.Logger
	translateThinking func(ctx context.Context, text string) (string, bool)
}

// New creates a native-client HTTP handler set.
func New(cfg Config) *Handler {
	shutdownContext := cfg.ShutdownContext
	if shutdownContext == nil {
		shutdownContext = context.Background()
	}
	return &Handler{
		dispatcher:        cfg.Dispatcher,
		chatHandler:       cfg.ChatHandler,
		shutdownContext:   shutdownContext,
		logger:            cfg.Logger,
		translateThinking: cfg.TranslateThinking,
	}
}

// maxMiniappRPCBodyBytes caps the POST /api/v1/miniapp/rpc body. This endpoint
// carries the whole miniapp.* surface, including capture RPCs whose params hold
// a base64-encoded image or audio recording (the ASR sidecar accepts up to a
// 90-minute clip), so the cap is generous — its job is to stop an unbounded
// io.ReadAll from OOMing the host (GPU memory == system RAM on the DGX), not to
// tightly bound captures. The text-only chat stream uses the smaller
// maxMiniappChatStreamBodyBytes.
const maxMiniappRPCBodyBytes = 128 << 20 // 128 MiB

// handleMiniappRPC bridges native-client HTTP POSTs into the existing RPC
// dispatcher. It enforces client-token auth before dispatch and rejects any
// method outside the miniapp.* namespace so the broader RPC surface stays
// inaccessible to remote callers.
func (s *Handler) RPC(w http.ResponseWriter, r *http.Request) {
	identity, ok := nativeauth.Authenticate(w, r, s.logger)
	if !ok {
		return
	}
	// This is the dispatch point for the whole miniapp.* surface, including the
	// blocking chat.send turn and the capture RPCs (audio ASR can run minutes,
	// then an agent turn). Those legitimately outlast the global WriteTimeout —
	// and RPC handlers return a ResponseFrame, so they can't lift it themselves.
	// Lift it here; each operation is bounded by its own deadline (turn deadline,
	// ASR timeout), and responses are small JSON (no slow-read write stall). The
	// body cap above and the interactive-turn semaphore are this endpoint's real
	// DoS bounds.
	disableWriteDeadline(w)

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxMiniappRPCBodyBytes))
	if err != nil {
		status := http.StatusBadRequest
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			status = http.StatusRequestEntityTooLarge
		}
		s.writeJSON(w, status, map[string]any{
			"error": "read body: " + err.Error(),
		})
		return
	}
	if len(body) == 0 {
		s.writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "empty body",
		})
		return
	}

	var frame protocol.RequestFrame
	if err := json.Unmarshal(body, &frame); err != nil {
		s.writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "invalid frame: " + err.Error(),
		})
		return
	}
	if frame.ID == "" || frame.Method == "" {
		s.writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "frame missing id or method",
		})
		return
	}

	// Confine remote callers to the miniapp.* surface. Other domains are
	// reachable from in-process callers (Telegram pipeline, cron, etc.) but
	// should never be reachable from the native client over HTTP.
	if !strings.HasPrefix(frame.Method, "miniapp.") {
		s.writeJSON(w, http.StatusForbidden, map[string]any{
			"error": "method outside miniapp.* namespace",
		})
		return
	}

	ctx := clientauth.WithContext(r.Context(), identity)
	if s.dispatcher == nil {
		s.writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "RPC dispatcher not ready"})
		return
	}
	resp := s.dispatcher.Dispatch(ctx, &frame)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Server", "deneb-gateway")
	// Negotiated gzip (server_http_gzip.go): compresses large list/detail JSON for
	// clients that advertise gzip (Android OkHttp does, transparently). SSE handlers
	// are separate and intentionally never compressed.
	writeRPCJSON(w, r, resp, s.logger, frame.Method)
}

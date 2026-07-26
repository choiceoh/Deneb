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
	"mime"
	"net/http"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/nativeauth"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/nativepush"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// ClientTokenHeader is the public HTTP header used by native-client routes.
// Route middleware should reference this constant instead of duplicating the
// authentication package's wire name.
const ClientTokenHeader = clientauth.Header

// ClientKindHeader tags a client surface (e.g. "desktop") on SSE subscribe
// requests. Browser-context clients (Tauri webview, vite dev) send it from a
// CORS-enforced origin, so it must stay in the CORS allow-list (routes.go).
const ClientKindHeader = "X-Deneb-Client-Kind"

// Authenticator binds the native client authentication adapter to a logger for
// sibling HTTP surfaces that share the same token contract.
func Authenticator(logger *slog.Logger) func(http.ResponseWriter, *http.Request) (*clientauth.Identity, bool) {
	return func(w http.ResponseWriter, r *http.Request) (*clientauth.Identity, bool) {
		return nativeauth.Authenticate(w, r, logger)
	}
}

// Config supplies the gateway-owned dependencies used by native HTTP handlers.
type Config struct {
	Dispatcher                  *rpc.Dispatcher
	ChatHandler                 chatport.SyncStreamRunner
	PushHub                     *nativepush.Hub
	ShutdownContext             context.Context
	Logger                      *slog.Logger
	AttachmentFactory           func() (MailAttachmentClient, error)
	GroupwareAttachmentDownload GroupwareAttachmentDownload
}

// Handler serves the authenticated native-client HTTP surface.
type Handler struct {
	dispatcher                  *rpc.Dispatcher
	chatHandler                 chatport.SyncStreamRunner
	pushHub                     *nativepush.Hub
	shutdownContext             context.Context
	logger                      *slog.Logger
	attachmentFactory           func() (MailAttachmentClient, error)
	groupwareAttachmentDownload GroupwareAttachmentDownload
}

// New creates a native-client HTTP handler set.
func New(cfg Config) *Handler {
	shutdownContext := cfg.ShutdownContext
	if shutdownContext == nil {
		shutdownContext = context.Background()
	}
	return &Handler{
		dispatcher:                  cfg.Dispatcher,
		chatHandler:                 cfg.ChatHandler,
		pushHub:                     cfg.PushHub,
		shutdownContext:             shutdownContext,
		logger:                      cfg.Logger,
		attachmentFactory:           cfg.AttachmentFactory,
		groupwareAttachmentDownload: cfg.GroupwareAttachmentDownload,
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

// MailAttachmentClient retrieves one attachment from the configured mail source.
type MailAttachmentClient interface {
	GetAttachment(ctx context.Context, messageID, attachmentID string) ([]byte, error)
}

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

// handleMiniappGmailAttachment streams a mail attachment over a normal
// authenticated GET. This path exists because a browser opening a download
// link cannot attach a custom header; the native client puts the client token
// in the query string and this handler verifies it before touching mail storage.
func (s *Handler) GmailAttachment(w http.ResponseWriter, r *http.Request) {
	if _, ok := nativeauth.AuthenticateDownload(w, r, s.logger); !ok {
		return
	}
	// A large attachment over a slow link can outlast the global WriteTimeout.
	disableWriteDeadline(w)

	q := r.URL.Query()
	messageID := strings.TrimSpace(q.Get("messageId"))
	attachmentID := strings.TrimSpace(q.Get("attachmentId"))
	if messageID == "" || attachmentID == "" {
		s.writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "missing messageId or attachmentId",
		})
		return
	}

	factory := s.attachmentFactory
	if factory == nil {
		s.writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "mail attachment client unavailable"})
		return
	}
	client, err := factory()
	if err != nil || client == nil {
		if err != nil {
			s.writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"error": "mail attachment client unavailable: " + err.Error(),
			})
			return
		}
		s.writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": "mail attachment client unavailable",
		})
		return
	}

	data, err := client.GetAttachment(r.Context(), messageID, attachmentID)
	if err != nil {
		s.writeJSON(w, statusForMiniappGmailAttachmentError(err), map[string]any{
			"error": "mail attachment download failed: " + err.Error(),
		})
		return
	}

	filename := sanitizeAttachmentFilename(q.Get("filename"))
	contentType := safeAttachmentContentType(q.Get("mimeType"))
	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": filename})
	if disposition == "" {
		disposition = `attachment; filename="attachment"`
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", disposition)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Server", "deneb-gateway")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data) //nolint:gosec // G705: attachment bytes come from authenticated Gmail API response
}

func sanitizeAttachmentFilename(raw string) string {
	name := strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(func(r rune) rune {
		switch {
		case r < 32 || r == 127:
			return -1
		case r == '/' || r == '\\':
			return '_'
		default:
			return r
		}
	}, name)
	name = strings.Trim(name, " .")
	if name == "" {
		return "attachment"
	}
	runes := []rune(name)
	if len(runes) > 180 {
		name = string(runes[:180])
	}
	if name == "" || name == "." || name == ".." {
		return "attachment"
	}
	return name
}

func safeAttachmentContentType(raw string) string {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(raw))
	typeName, subtype, hasSubtype := strings.Cut(mediaType, "/")
	if err != nil || !hasSubtype || typeName == "" || subtype == "" {
		return "application/octet-stream"
	}
	return mediaType
}

func statusForMiniappGmailAttachmentError(err error) int {
	if err == nil {
		return http.StatusBadGateway
	}
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "404") || strings.Contains(text, "not found"):
		return http.StatusNotFound
	case strings.Contains(text, "403") || strings.Contains(text, "forbidden"):
		return http.StatusForbidden
	case strings.Contains(text, "400") || strings.Contains(text, "invalid"):
		return http.StatusBadRequest
	default:
		return http.StatusBadGateway
	}
}

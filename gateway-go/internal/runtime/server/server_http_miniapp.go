// server_http_miniapp.go — HTTP bridge for the native-client RPC surface.
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
//	  Dispatcher.Dispatch(ctx + synthetic *telegram.InitData, frame)
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

package server

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

type miniappGmailAttachmentClient interface {
	GetAttachment(ctx context.Context, messageID, attachmentID string) ([]byte, error)
}

var miniappGmailAttachmentClientFactory = func() (miniappGmailAttachmentClient, error) {
	return gmail.DefaultClient()
}

// handleMiniappRPC bridges native-client HTTP POSTs into the existing RPC
// dispatcher. It enforces client-token auth before dispatch and rejects any
// method outside the miniapp.* namespace so the broader RPC surface stays
// inaccessible to remote callers.
func (s *Server) handleMiniappRPC(w http.ResponseWriter, r *http.Request) {
	initData, ok := s.authenticateMiniappRequest(w, r)
	if !ok {
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.writeJSON(w, http.StatusBadRequest, map[string]any{
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

	ctx := telegram.WithInitDataContext(r.Context(), initData)
	resp := s.dispatcher.Dispatch(ctx, &frame)

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Server", "deneb-gateway")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.logger.Error("miniapp rpc encode response", "method", frame.Method, "error", err)
	}
}

// handleMiniappGmailAttachment streams a Gmail attachment over a normal
// authenticated GET. This path exists because a browser opening a download
// link cannot attach a custom header; the native client puts the client token
// in the query string and this handler verifies it before touching Gmail.
func (s *Server) handleMiniappGmailAttachment(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authenticateMiniappDownloadRequest(w, r); !ok {
		return
	}

	q := r.URL.Query()
	messageID := strings.TrimSpace(q.Get("messageId"))
	attachmentID := strings.TrimSpace(q.Get("attachmentId"))
	if messageID == "" || attachmentID == "" {
		s.writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "missing messageId or attachmentId",
		})
		return
	}

	client, err := miniappGmailAttachmentClientFactory()
	if err != nil || client == nil {
		if err != nil {
			s.writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"error": "gmail client unavailable: " + err.Error(),
			})
			return
		}
		s.writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": "gmail client unavailable",
		})
		return
	}

	data, err := client.GetAttachment(r.Context(), messageID, attachmentID)
	if err != nil {
		s.writeJSON(w, statusForMiniappGmailAttachmentError(err), map[string]any{
			"error": "gmail attachment download failed: " + err.Error(),
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

// authenticateMiniappRequest verifies the X-Deneb-Client-Token header against
// the stored client secret. On failure it writes the HTTP error and returns
// (nil, false); on success it returns the synthetic operator InitData and
// (data, true). The Telegram initData ("Authorization: tma <raw>") path was
// retired with the Mini App webview — the native client is the only caller.
func (s *Server) authenticateMiniappRequest(w http.ResponseWriter, r *http.Request) (*telegram.InitData, bool) {
	tok := strings.TrimSpace(r.Header.Get(clientauth.Header))
	if tok == "" {
		s.writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": "missing client token",
		})
		return nil, false
	}
	if !clientauth.Verify(tok) {
		s.writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": "invalid client token",
		})
		return nil, false
	}
	return syntheticOperatorInitData(), true
}

// authenticateMiniappDownloadRequest authenticates a download GET. A browser
// opening a download link cannot set the X-Deneb-Client-Token header, so the
// client token rides in the query string instead, verified the same
// constant-time way as the header path.
func (s *Server) authenticateMiniappDownloadRequest(w http.ResponseWriter, r *http.Request) (*telegram.InitData, bool) {
	tok := strings.TrimSpace(r.URL.Query().Get("clientToken"))
	if tok == "" {
		s.writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "missing client token"})
		return nil, false
	}
	if !clientauth.Verify(tok) {
		s.writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid client token"})
		return nil, false
	}
	return syntheticOperatorInitData(), true
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
	if err != nil || mediaType == "" {
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

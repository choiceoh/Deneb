// server_http_miniapp.go — HTTP bridge for the Telegram Mini App RPC surface.
//
// Pipeline:
//
//	POST /api/v1/miniapp/rpc
//	  Authorization: tma <raw initData>
//	  Body:           protocol.RequestFrame (miniapp.* method)
//	    │
//	    ▼
//	  initData verification (HMAC-SHA256, TTL window)
//	    │
//	    ▼
//	  Dispatcher.Dispatch(ctx + *telegram.InitData, frame)
//	    │
//	    ▼
//	  protocol.ResponseFrame JSON
//
// Authentication uses the Telegram Mini App convention from
// https://docs.telegram-mini-apps.com/platform/authorizing-user — the client
// sends "Authorization: tma <raw>" where <raw> is the verbatim
// Telegram.WebApp.initData query string.

package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// miniappAuthScheme is the HTTP Authorization scheme Telegram clients use
// when calling Mini App backends. The raw initData follows after a space.
const miniappAuthScheme = "tma"

type miniappGmailAttachmentClient interface {
	GetAttachment(ctx context.Context, messageID, attachmentID string) ([]byte, error)
}

var miniappGmailAttachmentClientFactory = func() (miniappGmailAttachmentClient, error) {
	return gmail.DefaultClient()
}

// handleMiniappRPC bridges HTTP POSTs from the Mini App frontend into the
// existing RPC dispatcher. It enforces initData auth before dispatch and
// rejects any method outside the miniapp.* namespace so the broader RPC
// surface stays inaccessible to browser-origin callers.
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

	// Confine browser-origin callers to the miniapp.* surface. Other domains
	// are reachable from in-process callers (Telegram pipeline, cron, etc.)
	// but should never be reachable from a Mini App webview.
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
// authenticated GET. This path exists because a Telegram WebView cannot attach
// custom Authorization headers when opening a download link; the Mini App puts
// the raw initData in the query string and this handler verifies it before
// touching Gmail.
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

// authenticateMiniappRequest verifies the Authorization header against the
// Telegram bot token. On failure it writes the HTTP error and returns
// (nil, false); on success it returns the parsed *InitData and (data, true).
func (s *Server) authenticateMiniappRequest(w http.ResponseWriter, r *http.Request) (*telegram.InitData, bool) {
	raw, err := extractMiniappAuthHeader(r.Header.Get("Authorization"))
	if err != nil {
		s.writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": err.Error(),
		})
		return nil, false
	}
	return s.verifyMiniappRawInitData(w, raw)
}

func (s *Server) authenticateMiniappDownloadRequest(w http.ResponseWriter, r *http.Request) (*telegram.InitData, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("initData"))
	if raw == "" {
		var err error
		raw, err = extractMiniappAuthHeader(r.Header.Get("Authorization"))
		if err != nil {
			s.writeJSON(w, http.StatusUnauthorized, map[string]any{
				"error": "missing initData",
			})
			return nil, false
		}
	}
	return s.verifyMiniappRawInitData(w, raw)
}

func (s *Server) verifyMiniappRawInitData(w http.ResponseWriter, raw string) (*telegram.InitData, bool) {
	if s.telegramPlug == nil {
		s.writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": "telegram plugin not configured",
		})
		return nil, false
	}
	cfg := s.telegramPlug.Config()
	if cfg == nil || strings.TrimSpace(cfg.BotToken) == "" {
		s.writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": "telegram bot token not configured",
		})
		return nil, false
	}

	data, err := telegram.VerifyInitData(raw, cfg.BotToken, telegram.DefaultInitDataTTL)
	if err != nil {
		// User-impacting failure is a normal client error here (forged or
		// stale launch), so don't log as Error. Sentinel errors expose
		// enough detail for the client to choose between "re-open the Mini
		// App" and "your bot token rotated".
		s.writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": err.Error(),
		})
		return nil, false
	}
	if data.User == nil {
		s.writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": telegram.ErrInitDataNoUser.Error(),
		})
		return nil, false
	}
	return data, true
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

// extractMiniappAuthHeader pulls the raw initData payload out of an
// "Authorization: tma <raw>" header. The scheme match is case-insensitive
// per RFC 7235.
func extractMiniappAuthHeader(header string) (string, error) {
	if header == "" {
		return "", errors.New("missing authorization header")
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 {
		return "", errors.New("malformed authorization header (want \"tma <initData>\")")
	}
	if !strings.EqualFold(parts[0], miniappAuthScheme) {
		return "", errors.New("authorization scheme must be \"tma\"")
	}
	raw := strings.TrimSpace(parts[1])
	if raw == "" {
		return "", errors.New("empty initData in authorization header")
	}
	return raw, nil
}

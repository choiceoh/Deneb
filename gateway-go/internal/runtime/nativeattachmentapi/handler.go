// Package nativeattachmentapi serves native-client mail attachment downloads.
package nativeattachmentapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/nativeauth"
	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
)

// AttachmentClient retrieves one attachment from the configured mail source.
type AttachmentClient interface {
	GetAttachment(ctx context.Context, messageID, attachmentID string) ([]byte, error)
}

// Config supplies the attachment download adapter's gateway-owned dependencies.
type Config struct {
	Logger            *slog.Logger
	AttachmentFactory func() (AttachmentClient, error)
}

// Handler serves authenticated native-client attachment downloads.
type Handler struct {
	logger            *slog.Logger
	attachmentFactory func() (AttachmentClient, error)
}

// New creates a native-client attachment download handler.
func New(cfg Config) *Handler {
	return &Handler{
		logger:            cfg.Logger,
		attachmentFactory: cfg.AttachmentFactory,
	}
}

// GmailAttachment streams a mail attachment over an authenticated GET. This
// path exists because a browser opening a download link cannot attach a custom
// header; the native client puts the client token in the query string.
func (h *Handler) GmailAttachment(w http.ResponseWriter, r *http.Request) {
	if _, ok := nativeauth.AuthenticateDownload(w, r, h.logger); !ok {
		return
	}
	// A large attachment over a slow link can outlast the global WriteTimeout.
	disableWriteDeadline(w)

	q := r.URL.Query()
	messageID := strings.TrimSpace(q.Get("messageId"))
	attachmentID := strings.TrimSpace(q.Get("attachmentId"))
	if messageID == "" || attachmentID == "" {
		h.writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "missing messageId or attachmentId",
		})
		return
	}

	factory := h.attachmentFactory
	if factory == nil {
		h.writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "mail attachment client unavailable"})
		return
	}
	client, err := factory()
	if err != nil || client == nil {
		if err != nil {
			h.writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"error": "mail attachment client unavailable: " + err.Error(),
			})
			return
		}
		h.writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": "mail attachment client unavailable",
		})
		return
	}

	data, err := client.GetAttachment(r.Context(), messageID, attachmentID)
	if err != nil {
		h.writeJSON(w, statusForMiniappGmailAttachmentError(err), map[string]any{
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
	_, _ = w.Write(data) //nolint:gosec // G705: attachment bytes come from authenticated mail repository response
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Server", "deneb-gateway")
	if status != http.StatusOK {
		w.WriteHeader(status)
	}
	if err := json.NewEncoder(w).Encode(value); err != nil && h.logger != nil {
		httputil.LogEncodeError(h.logger, "native attachment api: json encode error", err)
	}
}

func disableWriteDeadline(w http.ResponseWriter) {
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
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

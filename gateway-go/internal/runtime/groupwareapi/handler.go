// Package groupwareapi owns the groupware binary attachment HTTP adapter.
package groupwareapi

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

// AttachmentDownload fetches one Amaranth ECM file by doc id + selector.
type AttachmentDownload func(ctx context.Context, docID, attachment string) (filename, mimeType string, data []byte, err error)

// Config supplies the gateway-owned dependencies for groupware HTTP handlers.
type Config struct {
	Download AttachmentDownload
	Logger   *slog.Logger
}

// Handler serves authenticated groupware HTTP downloads for the native client.
type Handler struct {
	download AttachmentDownload
	logger   *slog.Logger
}

// New creates a groupware HTTP handler set.
func New(cfg Config) *Handler {
	return &Handler{
		download: cfg.Download,
		logger:   cfg.Logger,
	}
}

// ApprovalAttachment streams one Amaranth approval attachment over GET.
// Query auth mirrors Gmail attachment downloads because browser download links
// cannot set the native client token header.
//
// Params: docId, attachment (1-based index or filename), optional filename/mimeType.
func (h *Handler) ApprovalAttachment(w http.ResponseWriter, r *http.Request) {
	if _, ok := nativeauth.AuthenticateDownload(w, r, h.logger); !ok {
		return
	}
	disableWriteDeadline(w)

	if h.download == nil {
		h.writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "groupware attachment download unavailable"})
		return
	}

	q := r.URL.Query()
	docID := strings.TrimSpace(q.Get("docId"))
	attachment := strings.TrimSpace(q.Get("attachment"))
	if docID == "" || attachment == "" {
		h.writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "missing docId or attachment",
		})
		return
	}

	filename, mimeType, data, err := h.download(r.Context(), docID, attachment)
	if err != nil {
		h.writeJSON(w, statusForAttachmentError(err), map[string]any{
			"error": "groupware attachment download failed: " + err.Error(),
		})
		return
	}
	if len(data) == 0 {
		h.writeJSON(w, http.StatusNotFound, map[string]any{"error": "attachment empty"})
		return
	}

	if hint := strings.TrimSpace(q.Get("filename")); hint != "" {
		filename = hint
	}
	if hint := strings.TrimSpace(q.Get("mimeType")); hint != "" {
		mimeType = hint
	}
	filename = sanitizeAttachmentFilename(filename)
	contentType := safeAttachmentContentType(mimeType)
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
	_, _ = w.Write(data) //nolint:gosec // G705: bytes from authenticated Amaranth ECM download
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Server", "deneb-gateway")
	if status != http.StatusOK {
		w.WriteHeader(status)
	}
	if err := json.NewEncoder(w).Encode(value); err != nil && h.logger != nil {
		httputil.LogEncodeError(h.logger, "groupware api: json encode error", err)
	}
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

func statusForAttachmentError(err error) int {
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

func disableWriteDeadline(w http.ResponseWriter) {
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
}

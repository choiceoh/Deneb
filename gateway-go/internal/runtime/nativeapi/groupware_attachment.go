package nativeapi

import (
	"context"
	"mime"
	"net/http"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/nativeauth"
)

// GroupwareAttachmentDownload fetches one Amaranth ECM file by doc id + selector.
type GroupwareAttachmentDownload func(ctx context.Context, docID, attachment string) (filename, mimeType string, data []byte, err error)

// GroupwareApprovalAttachment streams one Amaranth attachment over GET.
// Query auth mirrors GmailAttachment (browser download links cannot set headers).
// Params: docId, attachment (1-based index or filename), optional filename/mimeType.
func (s *Handler) GroupwareApprovalAttachment(w http.ResponseWriter, r *http.Request) {
	if _, ok := nativeauth.AuthenticateDownload(w, r, s.logger); !ok {
		return
	}
	disableWriteDeadline(w)

	if s.groupwareAttachmentDownload == nil {
		s.writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "groupware attachment download unavailable"})
		return
	}

	q := r.URL.Query()
	docID := strings.TrimSpace(q.Get("docId"))
	attachment := strings.TrimSpace(q.Get("attachment"))
	if docID == "" || attachment == "" {
		s.writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "missing docId or attachment",
		})
		return
	}

	filename, mimeType, data, err := s.groupwareAttachmentDownload(r.Context(), docID, attachment)
	if err != nil {
		s.writeJSON(w, statusForMiniappGmailAttachmentError(err), map[string]any{
			"error": "groupware attachment download failed: " + err.Error(),
		})
		return
	}
	if len(data) == 0 {
		s.writeJSON(w, http.StatusNotFound, map[string]any{"error": "attachment empty"})
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

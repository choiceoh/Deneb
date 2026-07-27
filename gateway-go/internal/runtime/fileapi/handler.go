// Package fileapi owns the file-store download HTTP adapter.
package fileapi

import (
	"encoding/json"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/filestore"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/fileshare"
	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
)

// Handler serves local file-store downloads over the gateway HTTP surface.
type Handler struct {
	logger *slog.Logger
}

// New creates a file-store download handler.
func New(logger *slog.Logger) *Handler {
	return &Handler{logger: logger}
}

// Download streams a file from the local file store, with two auth modes:
// client token OR a path-scoped share signature. It uses ServeContent for
// Content-Length + Range so large files resume over a flaky mobile link.
//
//	GET /api/v1/files/download?path=<vpath>&clientToken=<tok>     (operator)
//	GET /api/v1/files/download?path=<vpath>&exp=<unix>&sig=<hmac> (share link)
func (h *Handler) Download(w http.ResponseWriter, r *http.Request) {
	virt := strings.TrimSpace(r.URL.Query().Get("path"))
	if virt == "" {
		h.writeJSON(w, http.StatusBadRequest, map[string]any{"error": "missing path"})
		return
	}
	if !h.authorizeDownload(w, r, virt) {
		return
	}
	store, err := filestore.DefaultLocalStore()
	if err != nil {
		h.writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "file store unavailable"})
		return
	}
	f, meta, err := store.Open(r.Context(), virt)
	if err != nil {
		h.writeJSON(w, http.StatusNotFound, map[string]any{"error": "file not found"})
		return
	}
	defer func() { _ = f.Close() }()

	// A large file over a slow link can outlast the global WriteTimeout.
	disableWriteDeadline(w)

	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": meta.Name})
	if disposition == "" {
		disposition = `attachment; filename="download"`
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", disposition)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Server", "deneb-gateway")

	modTime := time.Time{}
	if meta.ServerModified != "" {
		if t, perr := time.Parse(time.RFC3339, meta.ServerModified); perr == nil {
			modTime = t
		}
	}
	http.ServeContent(w, r, meta.Name, modTime, f)
}

func (h *Handler) authorizeDownload(w http.ResponseWriter, r *http.Request, virt string) bool {
	q := r.URL.Query()
	if tok := strings.TrimSpace(q.Get("clientToken")); tok != "" {
		if clientauth.Verify(tok) {
			return true
		}
		h.writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid client token"})
		return false
	}
	if sig := strings.TrimSpace(q.Get("sig")); sig != "" {
		exp, _ := strconv.ParseInt(q.Get("exp"), 10, 64)
		if fileshare.Verify(virt, exp, sig) {
			return true
		}
		h.writeJSON(w, http.StatusForbidden, map[string]any{"error": "invalid or expired share link"})
		return false
	}
	h.writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "missing client token or share signature"})
	return false
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Server", "deneb-gateway")
	if status != http.StatusOK {
		w.WriteHeader(status)
	}
	if err := json.NewEncoder(w).Encode(value); err != nil && h.logger != nil {
		httputil.LogEncodeError(h.logger, "file api: json encode error", err)
	}
}

func disableWriteDeadline(w http.ResponseWriter) {
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
}

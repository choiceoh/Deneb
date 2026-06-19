package server

import (
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/filestore"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/fileshare"
)

// File-store download endpoint — the local replacement for Dropbox shared
// links. The native browser opens files through here, and the chat tool's
// "share" action hands out signed links (see internal/infra/fileshare) to it.
//
//	GET /api/v1/files/download?path=<vpath>&clientToken=<tok>     (operator)
//	GET /api/v1/files/download?path=<vpath>&exp=<unix>&sig=<hmac> (share link)

// handleFilesDownload streams a file from the local file store, with two auth
// modes (client token OR a path-scoped share signature). It uses ServeContent
// for Content-Length + Range so large files resume over a flaky mobile link.
func (s *Server) handleFilesDownload(w http.ResponseWriter, r *http.Request) {
	virt := strings.TrimSpace(r.URL.Query().Get("path"))
	if virt == "" {
		s.writeJSON(w, http.StatusBadRequest, map[string]any{"error": "missing path"})
		return
	}
	if !s.authorizeFileDownload(w, r, virt) {
		return
	}
	store, err := filestore.DefaultLocalStore()
	if err != nil {
		s.writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "file store unavailable"})
		return
	}
	f, meta, err := store.Open(r.Context(), virt)
	if err != nil {
		s.writeJSON(w, http.StatusNotFound, map[string]any{"error": "file not found"})
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

// authorizeFileDownload permits the request when it carries a valid client
// token OR a valid share signature scoped to virt. On failure it writes the
// 401/403 and returns false.
func (s *Server) authorizeFileDownload(w http.ResponseWriter, r *http.Request, virt string) bool {
	q := r.URL.Query()
	if tok := strings.TrimSpace(q.Get("clientToken")); tok != "" {
		if clientauth.Verify(tok) {
			return true
		}
		s.writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid client token"})
		return false
	}
	if sig := strings.TrimSpace(q.Get("sig")); sig != "" {
		exp, _ := strconv.ParseInt(q.Get("exp"), 10, 64)
		if fileshare.Verify(virt, exp, sig) {
			return true
		}
		s.writeJSON(w, http.StatusForbidden, map[string]any{"error": "invalid or expired share link"})
		return false
	}
	s.writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "missing client token or share signature"})
	return false
}

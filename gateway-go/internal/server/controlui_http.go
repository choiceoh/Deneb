// Package server — Control UI SPA serving.
//
// Serves the pre-built Lit-based Control UI from ui/dist/ (or a configured root).
// Handles SPA routing (non-file GET requests fall back to index.html), security
// headers, bootstrap config endpoint, and path traversal protection.
package server

import (
	"encoding/json"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// ControlUIHandler serves the Control UI SPA and its bootstrap config.
type ControlUIHandler struct {
	basePath string // URL base path (e.g. "/" or "/ui/")
	root     string // filesystem root for UI assets
	version  string
	enabled  bool
	logger   *slog.Logger
}

// NewControlUIHandler creates a handler for Control UI SPA serving.
// basePath is the URL prefix (normalized to start and end with "/").
// root is the filesystem directory containing the built UI assets.
// If root is empty or does not exist, the handler gracefully returns false
// for all requests.
func NewControlUIHandler(basePath, root, version string, enabled bool, logger *slog.Logger) *ControlUIHandler {
	// Normalize basePath to always start and end with "/".
	bp := strings.TrimSpace(basePath)
	if bp == "" {
		bp = "/"
	}
	if !strings.HasPrefix(bp, "/") {
		bp = "/" + bp
	}
	if !strings.HasSuffix(bp, "/") {
		bp = bp + "/"
	}

	return &ControlUIHandler{
		basePath: bp,
		root:     root,
		version:  version,
		enabled:  enabled,
		logger:   logger,
	}
}

// controlUIBootstrapConfig is the JSON payload for /__deneb/control-ui-config.json.
type controlUIBootstrapConfig struct {
	BasePath       string `json:"basePath"`
	AssistantName  string `json:"assistantName"`
	ServerVersion  string `json:"serverVersion,omitempty"`
}

// Handle processes Control UI requests. Returns true if the request was handled,
// false if the request does not match and should be passed to the next handler.
func (h *ControlUIHandler) Handle(w http.ResponseWriter, r *http.Request) bool {
	if !h.enabled {
		return false
	}

	// Bootstrap config endpoint — always available when enabled.
	if r.Method == http.MethodGet && r.URL.Path == "/__deneb/control-ui-config.json" {
		h.serveBootstrapConfig(w)
		return true
	}

	// Avatar placeholder — returns 404 for now.
	avatarPrefix := h.basePath + "avatar/"
	if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, avatarPrefix) {
		http.NotFound(w, r)
		return true
	}

	// Only handle GET/HEAD under our basePath.
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	if !strings.HasPrefix(r.URL.Path, h.basePath) && r.URL.Path+"/" != h.basePath {
		return false
	}

	// If root directory is not configured or missing, return false so the
	// next handler (e.g. handleRoot) can respond.
	if h.root == "" {
		return false
	}
	if _, err := os.Stat(h.root); err != nil {
		h.logger.Debug("control UI root not found, skipping", "root", h.root, "error", err)
		return false
	}

	// Resolve the relative path within the UI root.
	relPath := strings.TrimPrefix(r.URL.Path, h.basePath)
	if relPath == "" {
		relPath = "index.html"
	}

	// Path safety: reject traversal attempts and null bytes.
	if !isSafePath(relPath) {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return true
	}

	// Resolve to an absolute filesystem path within root.
	absPath := filepath.Join(h.root, filepath.FromSlash(relPath))

	// Verify the resolved path is still within root (defense in depth).
	cleanRoot, _ := filepath.Abs(h.root)
	cleanAbs, _ := filepath.Abs(absPath)
	if !strings.HasPrefix(cleanAbs, cleanRoot+string(filepath.Separator)) && cleanAbs != cleanRoot {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return true
	}

	// Check if the file exists. If not, and the path has no file extension,
	// serve index.html (SPA routing).
	info, err := os.Stat(absPath)
	if err != nil || info.IsDir() {
		// SPA fallback: non-file paths serve index.html.
		if !hasFileExtension(relPath) {
			h.serveFile(w, r, filepath.Join(h.root, "index.html"))
			return true
		}
		http.NotFound(w, r)
		return true
	}

	h.serveFile(w, r, absPath)
	return true
}

// serveBootstrapConfig responds with the control UI bootstrap configuration.
func (h *ControlUIHandler) serveBootstrapConfig(w http.ResponseWriter) {
	h.setSecurityHeaders(w)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")

	cfg := controlUIBootstrapConfig{
		BasePath:      strings.TrimSuffix(h.basePath, "/"),
		AssistantName: "Deneb",
		ServerVersion: h.version,
	}
	// If basePath is "/", send empty string (UI expects "" for root).
	if cfg.BasePath == "" {
		cfg.BasePath = ""
	}

	if err := json.NewEncoder(w).Encode(cfg); err != nil {
		h.logger.Error("failed to encode control UI config", "error", err)
	}
}

// serveFile serves a single file with security headers and correct MIME type.
func (h *ControlUIHandler) serveFile(w http.ResponseWriter, r *http.Request, path string) {
	h.setSecurityHeaders(w)

	// Set Content-Type based on extension, since http.ServeFile may not
	// always detect it correctly for all asset types.
	ext := strings.ToLower(filepath.Ext(path))
	if ct := mimeTypeForExt(ext); ct != "" {
		w.Header().Set("Content-Type", ct)
	}

	http.ServeFile(w, r, path)
}

// setSecurityHeaders applies standard security headers to responses.
func (h *ControlUIHandler) setSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
}

// isSafePath rejects paths containing traversal sequences or null bytes.
func isSafePath(p string) bool {
	if strings.Contains(p, "\x00") {
		return false
	}
	// Check each path component for ".." traversal.
	for _, part := range strings.Split(p, "/") {
		if part == ".." {
			return false
		}
	}
	return true
}

// hasFileExtension returns true if the path's last segment has a file extension.
func hasFileExtension(p string) bool {
	base := filepath.Base(p)
	return strings.Contains(base, ".") && !strings.HasPrefix(base, ".")
}

// mimeTypeForExt returns the MIME type for common web asset extensions.
// Falls back to Go's mime.TypeByExtension for less common types.
func mimeTypeForExt(ext string) string {
	// Explicit map for common SPA asset types to avoid OS-level MIME
	// database inconsistencies.
	switch ext {
	case ".html":
		return "text/html; charset=utf-8"
	case ".js", ".mjs":
		return "application/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".ico":
		return "image/x-icon"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".woff":
		return "font/woff"
	case ".woff2":
		return "font/woff2"
	case ".ttf":
		return "font/ttf"
	case ".map":
		return "application/json"
	default:
		return mime.TypeByExtension(ext)
	}
}

// Package controlui serves the Lit-based web control UI and its API endpoints.
//
// This mirrors src/gateway/dashboard/control-ui.ts from the TypeScript codebase.
// The control UI is a single-page app served from a static root directory with
// API endpoints for bootstrap config and avatar resolution.
package controlui

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// Config holds the control UI server configuration.
type Config struct {
	// Root is the filesystem path to the control UI assets.
	// Falls back to dist-runtime/ if empty.
	Root string

	// BasePath is the URL path prefix (e.g., "/ui"). Defaults to "/".
	BasePath string

	// AgentID is the default agent ID for bootstrap config.
	AgentID string

	// AssistantName is the display name for the assistant.
	AssistantName string

	// AssistantAvatar is the avatar URL or path.
	AssistantAvatar string

	// Version is the runtime service version.
	Version string

	// AvatarDir is the directory containing agent avatar files.
	AvatarDir string
}

// Handler serves the control UI static files and API.
type Handler struct {
	cfg    Config
	logger *slog.Logger
	mux    *http.ServeMux
}

// New creates a new control UI handler.
func New(cfg Config, logger *slog.Logger) *Handler {
	if cfg.BasePath == "" {
		cfg.BasePath = "/"
	}
	if cfg.AgentID == "" {
		cfg.AgentID = "default"
	}
	if cfg.AssistantName == "" {
		cfg.AssistantName = "Deneb"
	}
	if cfg.Version == "" {
		cfg.Version = "unknown"
	}

	h := &Handler{cfg: cfg, logger: logger, mux: http.NewServeMux()}
	h.registerRoutes()
	return h
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

func (h *Handler) registerRoutes() {
	base := strings.TrimRight(h.cfg.BasePath, "/")

	h.mux.HandleFunc(base+"/api/control-ui/bootstrap", h.handleBootstrap)
	h.mux.HandleFunc(base+"/control-ui/avatar/", h.handleAvatar)
	h.mux.HandleFunc(base+"/", h.handleStatic)
}

// securityHeaders applies security headers to all control UI responses.
func securityHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy",
		"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; "+
			"img-src 'self' data: https:; connect-src 'self' ws: wss:; "+
			"font-src 'self'; frame-ancestors 'none'")
}

// handleBootstrap returns the bootstrap configuration for the control UI SPA.
func (h *Handler) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	securityHeaders(w)
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Type", "application/json")

	resp := map[string]any{
		"agentId": h.cfg.AgentID,
		"assistantIdentity": map[string]string{
			"name":   h.cfg.AssistantName,
			"avatar": h.cfg.AssistantAvatar,
		},
		"runtimeServiceVersion": h.cfg.Version,
	}

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		h.logger.Error("bootstrap encode error", "error", err)
	}
}

// agentIDPattern validates agent ID format.
var agentIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

// handleAvatar resolves and serves agent avatar images.
func (h *Handler) handleAvatar(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	securityHeaders(w)

	// Extract agent ID from path: /control-ui/avatar/{agentId}
	agentID := path.Base(r.URL.Path)
	if !agentIDPattern.MatchString(agentID) {
		http.Error(w, "invalid agent ID", http.StatusBadRequest)
		return
	}

	// Check if meta query param is set (return JSON with avatar URL).
	if r.URL.Query().Get("meta") == "1" {
		avatar := h.resolveAvatarMeta(agentID)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(avatar)
		return
	}

	// Try to serve avatar file directly.
	if h.cfg.AvatarDir != "" {
		for _, ext := range []string{".png", ".jpg", ".jpeg", ".webp", ".svg"} {
			avatarPath := filepath.Join(h.cfg.AvatarDir, agentID+ext)
			if info, err := os.Stat(avatarPath); err == nil && !info.IsDir() {
				contentType := mime.TypeByExtension(ext)
				if contentType == "" {
					contentType = "application/octet-stream"
				}
				w.Header().Set("Content-Type", contentType)
				w.Header().Set("Cache-Control", "public, max-age=3600")
				http.ServeFile(w, r, avatarPath)
				return
			}
		}
	}

	http.NotFound(w, r)
}

func (h *Handler) resolveAvatarMeta(agentID string) map[string]any {
	if h.cfg.AvatarDir != "" {
		for _, ext := range []string{".png", ".jpg", ".jpeg", ".webp", ".svg"} {
			avatarPath := filepath.Join(h.cfg.AvatarDir, agentID+ext)
			if info, err := os.Stat(avatarPath); err == nil && !info.IsDir() {
				return map[string]any{
					"kind":     "local",
					"filePath": avatarPath,
				}
			}
		}
	}
	return map[string]any{
		"kind":   "none",
		"reason": "no avatar found for agent " + agentID,
	}
}

// staticExtensions are file extensions served as static assets (not SPA fallback).
var staticExtensions = map[string]bool{
	".js": true, ".mjs": true, ".css": true, ".json": true,
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
	".webp": true, ".svg": true, ".ico": true, ".woff": true,
	".woff2": true, ".ttf": true, ".eot": true, ".map": true,
	".txt": true, ".xml": true, ".webmanifest": true,
}

// contentTypes maps extensions to content types for common static assets.
var contentTypes = map[string]string{
	".html": "text/html; charset=utf-8",
	".js":   "application/javascript; charset=utf-8",
	".mjs":  "application/javascript; charset=utf-8",
	".css":  "text/css; charset=utf-8",
	".json": "application/json; charset=utf-8",
	".svg":  "image/svg+xml",
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".ico":  "image/x-icon",
	".woff": "font/woff",
	".woff2": "font/woff2",
}

// handleStatic serves static files from the control UI root directory.
// Implements SPA fallback: non-static-extension paths serve index.html.
func (h *Handler) handleStatic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	securityHeaders(w)

	root := h.cfg.Root
	if root == "" {
		http.Error(w, "control UI not configured", http.StatusServiceUnavailable)
		return
	}

	// Check if root directory exists.
	if _, err := os.Stat(root); os.IsNotExist(err) {
		http.Error(w, "control UI assets not found — run 'pnpm ui:build' to generate them", http.StatusServiceUnavailable)
		return
	}

	reqPath := r.URL.Path
	base := strings.TrimRight(h.cfg.BasePath, "/")
	if base != "" {
		reqPath = strings.TrimPrefix(reqPath, base)
	}
	if reqPath == "" || reqPath == "/" {
		reqPath = "/index.html"
	}

	// Clean path to prevent directory traversal.
	cleanPath := filepath.Clean(reqPath)
	if strings.Contains(cleanPath, "..") {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	filePath := filepath.Join(root, cleanPath)
	ext := filepath.Ext(cleanPath)

	// Check if file exists.
	info, err := os.Stat(filePath)
	if err != nil || info.IsDir() {
		// SPA fallback: serve index.html for non-static paths.
		if staticExtensions[ext] {
			http.NotFound(w, r)
			return
		}
		filePath = filepath.Join(root, "index.html")
		ext = ".html"
		if _, err := os.Stat(filePath); err != nil {
			http.Error(w, "control UI index.html not found", http.StatusServiceUnavailable)
			return
		}
	}

	// Set content type.
	if ct, ok := contentTypes[ext]; ok {
		w.Header().Set("Content-Type", ct)
	}

	// Cache: no-cache for HTML, long cache for hashed assets.
	if ext == ".html" {
		w.Header().Set("Cache-Control", "no-cache")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}

	http.ServeFile(w, r, filePath)
}

// RootState describes the state of the control UI root directory.
type RootState string

const (
	RootBundled  RootState = "bundled"
	RootResolved RootState = "resolved"
	RootInvalid  RootState = "invalid"
	RootMissing  RootState = "missing"
)

// ResolveRoot checks the control UI root directory and returns its state.
func ResolveRoot(root string) RootState {
	if root == "" {
		return RootMissing
	}
	info, err := os.Stat(root)
	if err != nil {
		return RootMissing
	}
	if !info.IsDir() {
		return RootInvalid
	}
	// Check for index.html.
	indexPath := filepath.Join(root, "index.html")
	if _, err := os.Stat(indexPath); err != nil {
		return RootInvalid
	}
	return RootResolved
}

// DiscoverRoot attempts to find the control UI root directory.
// Checks common locations in order: explicit config, dist-runtime, ui/dist.
func DiscoverRoot(candidates []string) string {
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		expanded := os.ExpandEnv(candidate)
		if state := ResolveRoot(expanded); state == RootResolved {
			return expanded
		}
	}
	return ""
}

// ListAssets lists all files in the control UI root for diagnostics.
func ListAssets(root string) ([]string, error) {
	if root == "" {
		return nil, fmt.Errorf("no root configured")
	}
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(root, path)
			files = append(files, rel)
		}
		return nil
	})
	return files, err
}

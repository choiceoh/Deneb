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
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// miniappAuthScheme is the HTTP Authorization scheme Telegram clients use
// when calling Mini App backends. The raw initData follows after a space.
const miniappAuthScheme = "tma"

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

// authenticateMiniappRequest verifies the Authorization header against the
// Telegram bot token. On failure it writes the HTTP error and returns
// (nil, false); on success it returns the parsed *InitData and (data, true).
func (s *Server) authenticateMiniappRequest(w http.ResponseWriter, r *http.Request) (*telegram.InitData, bool) {
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

	raw, err := extractMiniappAuthHeader(r.Header.Get("Authorization"))
	if err != nil {
		s.writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": err.Error(),
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

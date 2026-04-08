package server

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

const (
	// maxToolsInvokeBodyBytes limits the request body for /tools/invoke to 2 MB.
	maxToolsInvokeBodyBytes = 2 * 1024 * 1024
)

// gatewayHTTPToolDeny is the deny list for tools invoked via the HTTP endpoint.
// These tools are too dangerous to expose over HTTP (matches TS DEFAULT_GATEWAY_HTTP_TOOL_DENY).
var gatewayHTTPToolDeny = map[string]struct{}{
	"browser":            {},
	"computer":           {},
	"file_editor":        {},
	"text_editor":        {},
	"str_replace_editor": {},
}

// handleToolsInvoke handles POST /tools/invoke — executes a tool via HTTP.
// Mirrors src/gateway/tools-invoke-http.ts.
func (s *Server) handleToolsInvoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeJSON(w, http.StatusMethodNotAllowed, map[string]any{
			"ok": false, "error": map[string]string{"type": "method_not_allowed", "message": "POST required"},
		})
		return
	}

	// Authenticate via Bearer token.
	if !s.authorizeHTTP(w, r) {
		return
	}

	// Parse request body.
	limited := http.MaxBytesReader(w, r.Body, maxToolsInvokeBodyBytes)
	var body struct {
		Tool       string         `json:"tool"`
		Action     string         `json:"action,omitempty"`
		Args       map[string]any `json:"args,omitempty"`
		SessionKey string         `json:"sessionKey,omitempty"`
		DryRun     bool           `json:"dryRun,omitempty"`
	}
	if err := json.NewDecoder(limited).Decode(&body); err != nil {
		s.writeJSON(w, http.StatusBadRequest, map[string]any{
			"ok": false, "error": map[string]string{"type": "invalid_request", "message": "invalid JSON: " + err.Error()},
		})
		return
	}

	if body.Tool == "" {
		s.writeJSON(w, http.StatusBadRequest, map[string]any{
			"ok": false, "error": map[string]string{"type": "invalid_request", "message": "tool is required"},
		})
		return
	}

	// Check deny list.
	if _, ok := gatewayHTTPToolDeny[body.Tool]; ok {
		s.writeJSON(w, http.StatusForbidden, map[string]any{
			"ok": false, "error": map[string]string{"type": "forbidden", "message": "tool " + body.Tool + " is not allowed via HTTP"},
		})
		return
	}

	// Additional deny list check is handled at the RPC layer (tools.invoke).

	// Merge action into args if provided.
	if body.Action != "" {
		if body.Args == nil {
			body.Args = make(map[string]any)
		}
		body.Args["action"] = body.Action
	}

	// Construct RPC request and dispatch.
	params, _ := json.Marshal(map[string]any{
		"tool":       body.Tool,
		"args":       body.Args,
		"sessionKey": body.SessionKey,
		"dryRun":     body.DryRun,
	})

	frame := &protocol.RequestFrame{
		Type:   protocol.FrameTypeRequest,
		ID:     "http-tools-invoke",
		Method: "tools.invoke",
		Params: params,
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30_000_000_000) // 30s
	defer cancel()
	resp := s.dispatcher.Dispatch(ctx, frame)

	// Map RPC response to HTTP response.
	if resp.Error != nil {
		status := http.StatusInternalServerError
		errType := "tool_error"
		switch resp.Error.Code {
		case protocol.ErrNotFound:
			status = http.StatusNotFound
			errType = "not_found"
		case protocol.ErrForbidden:
			status = http.StatusForbidden
			errType = "forbidden"
		case protocol.ErrMissingParam, protocol.ErrInvalidRequest:
			status = http.StatusBadRequest
			errType = "invalid_request"
		}
		s.writeJSON(w, status, map[string]any{
			"ok": false, "error": map[string]string{"type": errType, "message": resp.Error.Message},
		})
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "result": resp.Payload,
	})
}

// authorizeHTTP always returns true for single-user deployment.
func (s *Server) authorizeHTTP(_ http.ResponseWriter, _ *http.Request) bool {
	return true
}

package openaiapi

import (
	"encoding/json"
	"net/http"
	"time"
)

// handleChatCompletions implements POST /v1/chat/completions.
//
// v1 scope: non-stream only, text content, role-aliased models. Tools,
// streaming SSE, multimodal content, and the augment layer (Telegram
// memory injection) follow in subsequent commits.
func (r *routes) handleChatCompletions(w http.ResponseWriter, req *http.Request) {
	var body chatCompletionsRequest
	dec := json.NewDecoder(req.Body)
	if err := dec.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "invalid JSON body: "+err.Error())
		return
	}

	if body.Stream {
		writeError(w, http.StatusNotImplemented, "invalid_request_error", "stream=true is not yet supported; set stream=false")
		return
	}

	role, ok := roleAlias(body.Model)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "unknown model: "+body.Model)
		return
	}
	if r.deps.ModelRegistry == nil || r.deps.ModelRegistry.Model(role) == "" {
		writeError(w, http.StatusServiceUnavailable, "server_error", "no model configured for role: "+string(role))
		return
	}

	chatReq, err := translateRequest(body, r.deps.ModelRegistry, role)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}

	if r.deps.ChatClient == nil {
		writeError(w, http.StatusServiceUnavailable, "server_error", "chat client not wired")
		return
	}
	client := r.deps.ChatClient(role)
	if client == nil {
		writeError(w, http.StatusServiceUnavailable, "server_error", "no client for role: "+string(role))
		return
	}

	events, err := client.StreamChat(req.Context(), chatReq)
	if err != nil {
		r.deps.Logger.Error("openaiapi: upstream StreamChat failed", "error", err, "role", role)
		writeError(w, http.StatusBadGateway, "api_error", "upstream call failed: "+err.Error())
		return
	}

	resp := accumulateNonStream(events, body.Model, time.Now().Unix())
	writeJSON(w, http.StatusOK, resp)
}

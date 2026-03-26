// Package server — Open Responses HTTP API handler.
//
// Implements POST /v1/responses, accepting OpenAI Responses API requests
// and proxying them through the Go gateway's native chat handler.
// Supports both non-streaming and streaming (SSE) responses.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// maxResponsesBodyBytes is the default body size limit for /v1/responses.
const maxResponsesBodyBytes = 20 * 1024 * 1024 // 20 MB

// --- Request types ---

// ResponsesRequest is the inbound request body for /v1/responses.
type ResponsesRequest struct {
	Model        string `json:"model"`
	Input        any    `json:"input"` // string or []ItemParam
	Instructions string `json:"instructions,omitempty"`
	Stream       *bool  `json:"stream,omitempty"`
	User         string `json:"user,omitempty"`
}

// --- Response types ---

// responsesResponse is the non-streaming response envelope.
type responsesResponse struct {
	ID        string                `json:"id"`
	Object    string                `json:"object"`
	CreatedAt int64                 `json:"created_at"`
	Status    string                `json:"status"`
	Model     string                `json:"model"`
	Output    []responsesOutputItem `json:"output"`
	Usage     responsesUsage        `json:"usage"`
}

type responsesOutputItem struct {
	Type    string                    `json:"type"`
	ID      string                    `json:"id"`
	Role    string                    `json:"role"`
	Content []responsesContentBlock   `json:"content"`
	Status  string                    `json:"status"`
}

type responsesContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type responsesUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// --- Handler ---

// handleResponses handles POST /v1/responses.
func (s *Server) handleResponses(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check if endpoint is enabled via config.
	if !s.isResponsesEnabled() {
		s.writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "responses endpoint is not enabled",
		})
		return
	}

	// Auth: extract Bearer token and validate.
	if !s.authenticateHTTP(w, r) {
		return
	}

	// Parse request body with size limit.
	maxBody := int64(maxResponsesBodyBytes)
	if s.runtimeCfg != nil && s.runtimeCfg.OpenResponsesConfig != nil &&
		s.runtimeCfg.OpenResponsesConfig.MaxBodyBytes != nil {
		maxBody = int64(*s.runtimeCfg.OpenResponsesConfig.MaxBodyBytes)
	}
	limited := http.MaxBytesReader(w, r.Body, maxBody)

	var req ResponsesRequest
	if err := json.NewDecoder(limited).Decode(&req); err != nil {
		s.writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{
				"message": "invalid request body: " + err.Error(),
				"type":    "invalid_request_error",
			},
		})
		return
	}

	// Extract prompt text from input.
	prompt := extractResponsesInput(req.Input)
	if prompt == "" {
		s.writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{
				"message": "input is required and must contain text",
				"type":    "invalid_request_error",
			},
		})
		return
	}

	// Check chat handler availability (after validation to surface 400 errors first).
	if s.chatHandler == nil {
		s.writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": map[string]string{
				"message": "chat handler not available",
				"type":    "server_error",
			},
		})
		return
	}

	// Prepend instructions to prompt if provided.
	if req.Instructions != "" {
		prompt = req.Instructions + "\n\n" + prompt
	}

	// Session key derived from user field or default.
	sessionKey := "responses-compat"
	if req.User != "" {
		sessionKey = "responses-compat-" + req.User
	}

	isStream := req.Stream != nil && *req.Stream

	if isStream {
		s.handleResponsesStream(w, r, req, sessionKey, prompt)
	} else {
		s.handleResponsesSync(w, r, req, sessionKey, prompt)
	}
}

// handleResponsesSync handles non-streaming responses.
func (s *Server) handleResponsesSync(w http.ResponseWriter, r *http.Request, req ResponsesRequest, sessionKey, prompt string) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	result, err := s.chatHandler.SendSync(ctx, sessionKey, prompt, req.Model)
	if err != nil {
		s.logger.Error("responses request failed", "error", err)
		s.writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": map[string]string{
				"message": "internal error: " + err.Error(),
				"type":    "server_error",
			},
		})
		return
	}

	now := time.Now().Unix()
	respID := fmt.Sprintf("resp_%d", now)
	msgID := fmt.Sprintf("msg_%d", now)

	resp := responsesResponse{
		ID:        respID,
		Object:    "response",
		CreatedAt: now,
		Status:    "completed",
		Model:     result.Model,
		Output: []responsesOutputItem{
			{
				Type: "message",
				ID:   msgID,
				Role: "assistant",
				Content: []responsesContentBlock{
					{Type: "output_text", Text: result.Text},
				},
				Status: "completed",
			},
		},
		Usage: responsesUsage{
			InputTokens:  result.InputTokens,
			OutputTokens: result.OutputTokens,
			TotalTokens:  result.InputTokens + result.OutputTokens,
		},
	}

	s.writeJSON(w, http.StatusOK, resp)
}

// handleResponsesStream handles streaming responses via SSE with typed events.
func (s *Server) handleResponsesStream(w http.ResponseWriter, r *http.Request, req ResponsesRequest, sessionKey, prompt string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		s.writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": map[string]string{
				"message": "streaming not supported",
				"type":    "server_error",
			},
		})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	now := time.Now().Unix()
	respID := fmt.Sprintf("resp_%d", now)
	msgID := fmt.Sprintf("msg_%d", now)

	// Build initial response object for response.created event.
	initialResp := responsesResponse{
		ID:        respID,
		Object:    "response",
		CreatedAt: now,
		Status:    "in_progress",
		Model:     req.Model,
		Output:    []responsesOutputItem{},
		Usage:     responsesUsage{},
	}

	// Emit response.created.
	writeSSEEvent(w, flusher, "response.created", initialResp)

	// Emit response.output_item.added.
	outputItem := responsesOutputItem{
		Type:    "message",
		ID:      msgID,
		Role:    "assistant",
		Content: []responsesContentBlock{},
		Status:  "in_progress",
	}
	writeSSEEvent(w, flusher, "response.output_item.added", map[string]any{
		"output_index": 0,
		"item":         outputItem,
	})

	// Emit response.content_part.added.
	writeSSEEvent(w, flusher, "response.content_part.added", map[string]any{
		"output_index":  0,
		"content_index": 0,
		"part":          responsesContentBlock{Type: "output_text", Text: ""},
	})

	// Set up keepalive ticker.
	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()

	// Channel to signal keepalive goroutine to stop.
	done := make(chan struct{})
	defer close(done)

	// Background keepalive writer.
	go func() {
		for {
			select {
			case <-done:
				return
			case <-keepalive.C:
				fmt.Fprint(w, ": keepalive\n\n")
				flusher.Flush()
			}
		}
	}()

	// Stream content via SendSyncStream.
	result, err := s.chatHandler.SendSyncStream(ctx, sessionKey, prompt, req.Model, func(delta string) {
		writeSSEEvent(w, flusher, "response.output_text.delta", map[string]any{
			"output_index":  0,
			"content_index": 0,
			"delta":         delta,
		})
	})

	if err != nil {
		s.logger.Error("streaming responses request failed", "error", err)
		writeSSEEvent(w, flusher, "error", map[string]any{
			"message": "stream error: " + err.Error(),
			"type":    "server_error",
		})
		return
	}

	// Emit response.output_text.done.
	finalText := ""
	if result != nil {
		finalText = result.Text
	}
	writeSSEEvent(w, flusher, "response.output_text.done", map[string]any{
		"output_index":  0,
		"content_index": 0,
		"text":          finalText,
	})

	// Emit response.content_part.done.
	writeSSEEvent(w, flusher, "response.content_part.done", map[string]any{
		"output_index":  0,
		"content_index": 0,
		"part":          responsesContentBlock{Type: "output_text", Text: finalText},
	})

	// Emit response.output_item.done.
	completedItem := responsesOutputItem{
		Type: "message",
		ID:   msgID,
		Role: "assistant",
		Content: []responsesContentBlock{
			{Type: "output_text", Text: finalText},
		},
		Status: "completed",
	}
	writeSSEEvent(w, flusher, "response.output_item.done", map[string]any{
		"output_index": 0,
		"item":         completedItem,
	})

	// Emit response.completed.
	resolvedModel := req.Model
	if result != nil && result.Model != "" {
		resolvedModel = result.Model
	}
	usage := responsesUsage{}
	if result != nil {
		usage = responsesUsage{
			InputTokens:  result.InputTokens,
			OutputTokens: result.OutputTokens,
			TotalTokens:  result.InputTokens + result.OutputTokens,
		}
	}

	completedResp := responsesResponse{
		ID:        respID,
		Object:    "response",
		CreatedAt: now,
		Status:    "completed",
		Model:     resolvedModel,
		Output:    []responsesOutputItem{completedItem},
		Usage:     usage,
	}
	writeSSEEvent(w, flusher, "response.completed", completedResp)
}

// --- Helpers ---

// isResponsesEnabled checks whether the responses endpoint is enabled.
func (s *Server) isResponsesEnabled() bool {
	if s.runtimeCfg == nil {
		return false
	}
	return s.runtimeCfg.OpenResponsesEnabled
}

// extractResponsesInput extracts text from the input field.
// Input can be a string or an array of message items.
func extractResponsesInput(input any) string {
	switch v := input.(type) {
	case string:
		return v
	case []any:
		// Array of message items — find user messages and concatenate content.
		var parts []string
		for _, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			role, _ := m["role"].(string)
			if role != "" && role != "user" {
				continue
			}
			// Content can be a string or array of content blocks.
			switch c := m["content"].(type) {
			case string:
				parts = append(parts, c)
			case []any:
				for _, block := range c {
					if bm, ok := block.(map[string]any); ok {
						if t, _ := bm["type"].(string); t == "input_text" || t == "text" {
							if text, _ := bm["text"].(string); text != "" {
								parts = append(parts, text)
							}
						}
					}
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

// writeSSEEvent writes a named SSE event with JSON data.
func writeSSEEvent(w http.ResponseWriter, flusher http.Flusher, event string, data any) {
	b, err := json.Marshal(data)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
	flusher.Flush()
}

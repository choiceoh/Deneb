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

	"github.com/choiceoh/deneb/gateway-go/internal/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/shortid"
)

// maxResponsesBodyBytes is the default body size limit for /v1/responses.
const maxResponsesBodyBytes = 20 * 1024 * 1024 // 20 MB

// --- Request types ---

// ResponsesRequest is the inbound request body for /v1/responses.
type ResponsesRequest struct {
	Model           string          `json:"model"`
	Input           any             `json:"input"` // string or []ItemParam
	Instructions    string          `json:"instructions,omitempty"`
	Stream          *bool           `json:"stream,omitempty"`
	User            string          `json:"user,omitempty"`
	MaxOutputTokens *int            `json:"max_output_tokens,omitempty"`
	Temperature     *float64        `json:"temperature,omitempty"`
	TopP            *float64        `json:"top_p,omitempty"`
	Tools           json.RawMessage `json:"tools,omitempty"`
	ToolChoice      json.RawMessage `json:"tool_choice,omitempty"`
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
	Type    string                  `json:"type"`
	ID      string                  `json:"id"`
	Role    string                  `json:"role"`
	Content []responsesContentBlock `json:"content"`
	Status  string                  `json:"status"`
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
		writeOpenAIError(w, s, http.StatusNotFound, "responses endpoint is not enabled", "not_found")
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
		writeOpenAIError(w, s, http.StatusBadRequest, "invalid request body: "+err.Error(), "invalid_request_error")
		return
	}

	// Validate model is non-empty.
	if req.Model == "" {
		writeOpenAIError(w, s, http.StatusBadRequest, "model is required", "invalid_request_error")
		return
	}

	// Extract prompt text from input.
	prompt := extractResponsesInput(req.Input)
	if prompt == "" {
		writeOpenAIError(w, s, http.StatusBadRequest, "input is required and must contain text", "invalid_request_error")
		return
	}

	// Check chat handler availability (after validation to surface 400 errors first).
	if s.chatHandler == nil {
		writeOpenAIError(w, s, http.StatusServiceUnavailable, "chat handler not available", "server_error")
		return
	}

	// Build sync options from request parameters.
	opts := buildResponsesSyncOptions(req)

	// Prepend instructions to prompt if provided.
	if req.Instructions != "" {
		opts.SystemPrompt = req.Instructions
	}

	// Session key derived from user field or default.
	sessionKey := "responses-compat"
	if req.User != "" {
		sessionKey = "responses-compat-" + req.User
	}

	isStream := req.Stream != nil && *req.Stream

	if isStream {
		s.handleResponsesStream(w, r, req, sessionKey, prompt, opts)
	} else {
		s.handleResponsesSync(w, r, req, sessionKey, prompt, opts)
	}
}

// handleResponsesSync handles non-streaming responses.
func (s *Server) handleResponsesSync(w http.ResponseWriter, r *http.Request, req ResponsesRequest, sessionKey, prompt string, opts *chat.SyncOptions) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	result, err := s.chatHandler.SendSync(ctx, sessionKey, prompt, req.Model, opts)
	if err != nil {
		s.logger.Error("responses request failed", "error", err)
		writeOpenAIError(w, s, http.StatusInternalServerError, "internal error: "+err.Error(), "server_error")
		return
	}

	respID := "resp_" + shortid.New("rs")
	msgID := "msg_" + shortid.New("ms")

	resp := responsesResponse{
		ID:        respID,
		Object:    "response",
		CreatedAt: time.Now().Unix(),
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
func (s *Server) handleResponsesStream(w http.ResponseWriter, r *http.Request, req ResponsesRequest, sessionKey, prompt string, opts *chat.SyncOptions) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeOpenAIError(w, s, http.StatusInternalServerError, "streaming not supported", "server_error")
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
	respID := "resp_" + shortid.New("rs")
	msgID := "msg_" + shortid.New("ms")

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
	result, err := s.chatHandler.SendSyncStream(ctx, sessionKey, prompt, req.Model, opts, func(delta string) {
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

// buildResponsesSyncOptions creates SyncOptions from a Responses API request.
func buildResponsesSyncOptions(req ResponsesRequest) *chat.SyncOptions {
	opts := &chat.SyncOptions{
		Temperature: req.Temperature,
		TopP:        req.TopP,
		MaxTokens:   req.MaxOutputTokens,
	}

	// Tool choice (pass through as-is).
	if len(req.ToolChoice) > 0 {
		var tc any
		if json.Unmarshal(req.ToolChoice, &tc) == nil {
			opts.ToolChoice = tc
		}
	}

	return opts
}

// isResponsesEnabled checks whether the responses endpoint is enabled.
func (s *Server) isResponsesEnabled() bool {
	if s.runtimeCfg == nil {
		return false
	}
	return s.runtimeCfg.OpenResponsesEnabled
}

// extractResponsesInput extracts text from the input field.
// Input can be a string or an array of item objects (message, function_call_output, etc.).
func extractResponsesInput(input any) string {
	switch v := input.(type) {
	case string:
		return strings.TrimSpace(v)
	case []any:
		var parts []string
		for _, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			itemType, _ := m["type"].(string)
			switch itemType {
			case "message":
				// Extract text from content (string or array of content blocks).
				if content, ok := m["content"].(string); ok {
					parts = append(parts, content)
				} else if contentArr, ok := m["content"].([]any); ok {
					for _, part := range contentArr {
						if pm, ok := part.(map[string]any); ok {
							partType, _ := pm["type"].(string)
							if partType == "input_text" || partType == "text" {
								if text, ok := pm["text"].(string); ok {
									parts = append(parts, text)
								}
							}
						}
					}
				}
			case "function_call_output":
				if output, ok := m["output"].(string); ok {
					parts = append(parts, output)
				}
			default:
				// Legacy format: items without an explicit type but with role+content.
				role, _ := m["role"].(string)
				if role != "" && role != "user" {
					continue
				}
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
		}
		return strings.TrimSpace(strings.Join(parts, "\n"))
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

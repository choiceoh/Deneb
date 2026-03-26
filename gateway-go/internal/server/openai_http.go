// Package server — OpenAI Chat Completions HTTP API handler.
//
// Implements POST /v1/chat/completions, accepting OpenAI-compatible requests
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

	"github.com/choiceoh/deneb/gateway-go/internal/auth"
)

// maxChatCompletionBodyBytes is the default body size limit for /v1/chat/completions.
const maxChatCompletionBodyBytes = 20 * 1024 * 1024 // 20 MB

// --- Request types ---

// OpenAIChatRequest is the inbound request body for /v1/chat/completions.
type OpenAIChatRequest struct {
	Model    string          `json:"model"`
	Messages []OpenAIMessage `json:"messages"`
	Stream   bool            `json:"stream,omitempty"`
	User     string          `json:"user,omitempty"`
}

// OpenAIMessage represents a single message in the chat completion request.
type OpenAIMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"` // string or []ContentPart
	Name    string `json:"name,omitempty"`
}

// OpenAIContentPart is a typed content block within a multi-part message.
type OpenAIContentPart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// --- Response types ---

// openAIChatResponse is the non-streaming response envelope.
type openAIChatResponse struct {
	ID      string             `json:"id"`
	Object  string             `json:"object"`
	Created int64              `json:"created"`
	Model   string             `json:"model"`
	Choices []openAIChatChoice `json:"choices"`
	Usage   openAIUsage        `json:"usage"`
}

type openAIChatChoice struct {
	Index        int              `json:"index"`
	Message      *openAIRespMsg   `json:"message,omitempty"`
	Delta        *openAIRespDelta `json:"delta,omitempty"`
	FinishReason *string          `json:"finish_reason"`
}

type openAIRespMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIRespDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// --- Handler ---

// handleChatCompletions handles POST /v1/chat/completions.
func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check if endpoint is enabled via config.
	if !s.isChatCompletionsEnabled() {
		s.writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "chat completions endpoint is not enabled",
		})
		return
	}

	// Auth: extract Bearer token and validate.
	if !s.authenticateHTTP(w, r) {
		return
	}

	// Parse request body with size limit.
	maxBody := int64(maxChatCompletionBodyBytes)
	if s.runtimeCfg != nil && s.runtimeCfg.OpenAIChatCompletionsConfig != nil &&
		s.runtimeCfg.OpenAIChatCompletionsConfig.MaxBodyBytes != nil {
		maxBody = int64(*s.runtimeCfg.OpenAIChatCompletionsConfig.MaxBodyBytes)
	}
	limited := http.MaxBytesReader(w, r.Body, maxBody)

	var req OpenAIChatRequest
	if err := json.NewDecoder(limited).Decode(&req); err != nil {
		s.writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{
				"message": "invalid request body: " + err.Error(),
				"type":    "invalid_request_error",
			},
		})
		return
	}

	// Validate model is non-empty.
	if req.Model == "" {
		s.writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{
				"message": "model is required",
				"type":    "invalid_request_error",
			},
		})
		return
	}

	if len(req.Messages) == 0 {
		s.writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{
				"message": "messages array is required and must not be empty",
				"type":    "invalid_request_error",
			},
		})
		return
	}

	// Extract user prompt from the last user message.
	prompt := extractUserPrompt(req.Messages)

	// If no text prompt but images are present, use a placeholder.
	if prompt == "" && hasImageContent(req.Messages) {
		prompt = "User sent image(s) with no text."
	}

	if prompt == "" {
		s.writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]string{
				"message": "no user message found in messages array",
				"type":    "invalid_request_error",
			},
		})
		return
	}

	// Extract system/developer prompt and prepend if present.
	systemPrompt := extractSystemPrompt(req.Messages)
	if systemPrompt != "" {
		prompt = systemPrompt + "\n\n" + prompt
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

	// Use a session key derived from the user field, or a default.
	sessionKey := "openai-compat"
	if req.User != "" {
		sessionKey = "openai-compat-" + req.User
	}

	// Allow overriding session key via X-Deneb-Session header.
	if sessionHeader := r.Header.Get("X-Deneb-Session"); sessionHeader != "" {
		sessionKey = sessionHeader
	}

	if req.Stream {
		s.handleChatCompletionsStream(w, r, req, sessionKey, prompt)
	} else {
		s.handleChatCompletionsSync(w, r, req, sessionKey, prompt)
	}
}

// handleChatCompletionsSync handles non-streaming chat completions.
func (s *Server) handleChatCompletionsSync(w http.ResponseWriter, r *http.Request, req OpenAIChatRequest, sessionKey, prompt string) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	result, err := s.chatHandler.SendSync(ctx, sessionKey, prompt, req.Model)
	if err != nil {
		s.logger.Error("chat completion failed", "error", err)
		s.writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": map[string]string{
				"message": "internal error: " + err.Error(),
				"type":    "server_error",
			},
		})
		return
	}

	now := time.Now().Unix()
	completionID := fmt.Sprintf("chatcmpl_%d", now)
	finishReason := "stop"

	resp := openAIChatResponse{
		ID:      completionID,
		Object:  "chat.completion",
		Created: now,
		Model:   result.Model,
		Choices: []openAIChatChoice{
			{
				Index: 0,
				Message: &openAIRespMsg{
					Role:    "assistant",
					Content: result.Text,
				},
				FinishReason: &finishReason,
			},
		},
		Usage: openAIUsage{
			PromptTokens:     result.InputTokens,
			CompletionTokens: result.OutputTokens,
			TotalTokens:      result.InputTokens + result.OutputTokens,
		},
	}

	s.writeJSON(w, http.StatusOK, resp)
}

// handleChatCompletionsStream handles streaming chat completions via SSE.
func (s *Server) handleChatCompletionsStream(w http.ResponseWriter, r *http.Request, req OpenAIChatRequest, sessionKey, prompt string) {
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
	completionID := fmt.Sprintf("chatcmpl_%d", now)

	// Send initial chunk with role.
	writeSSEData(w, flusher, openAIChatResponse{
		ID:      completionID,
		Object:  "chat.completion.chunk",
		Created: now,
		Model:   req.Model,
		Choices: []openAIChatChoice{
			{
				Index: 0,
				Delta: &openAIRespDelta{Role: "assistant"},
			},
		},
	})

	// Stream content chunks via SendSyncStream.
	result, err := s.chatHandler.SendSyncStream(ctx, sessionKey, prompt, req.Model, func(delta string) {
		writeSSEData(w, flusher, openAIChatResponse{
			ID:      completionID,
			Object:  "chat.completion.chunk",
			Created: now,
			Model:   req.Model,
			Choices: []openAIChatChoice{
				{
					Index: 0,
					Delta: &openAIRespDelta{Content: delta},
				},
			},
		})
	})

	if err != nil {
		s.logger.Error("streaming chat completion failed", "error", err)
		// Stream already started; can't change status code. Send error as SSE event.
		writeSSEData(w, flusher, map[string]any{
			"error": map[string]string{
				"message": "stream error: " + err.Error(),
				"type":    "server_error",
			},
		})
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
		return
	}

	// Send final chunk with finish_reason and usage.
	finishReason := "stop"
	finalModel := req.Model
	if result != nil && result.Model != "" {
		finalModel = result.Model
	}
	writeSSEData(w, flusher, openAIChatResponse{
		ID:      completionID,
		Object:  "chat.completion.chunk",
		Created: now,
		Model:   finalModel,
		Choices: []openAIChatChoice{
			{
				Index:        0,
				Delta:        &openAIRespDelta{},
				FinishReason: &finishReason,
			},
		},
	})

	fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

// --- Helpers ---

// isChatCompletionsEnabled checks whether the chat completions endpoint is enabled.
func (s *Server) isChatCompletionsEnabled() bool {
	if s.runtimeCfg == nil {
		return false
	}
	return s.runtimeCfg.OpenAIChatCompletionsEnabled
}

// authenticateHTTP validates the Bearer token on an HTTP request.
// Returns true if authenticated, false if it wrote an error response.
func (s *Server) authenticateHTTP(w http.ResponseWriter, r *http.Request) bool {
	if s.authValidator == nil {
		// No-auth mode: allow all requests.
		return true
	}

	token := extractBearerToken(r)
	if token == "" {
		s.writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": map[string]string{
				"message": "missing Authorization header with Bearer token",
				"type":    "authentication_error",
			},
		})
		return false
	}

	claims, err := s.authValidator.ValidateToken(token)
	if err != nil {
		s.writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": map[string]string{
				"message": "invalid token: " + err.Error(),
				"type":    "authentication_error",
			},
		})
		return false
	}

	// Verify the token has at least write scope.
	scopes := claims.Scopes
	if scopes == nil {
		scopes = auth.DefaultScopes(claims.Role)
	}
	hasWrite := false
	for _, sc := range scopes {
		if sc == auth.ScopeWrite || sc == auth.ScopeAdmin {
			hasWrite = true
			break
		}
	}
	if !hasWrite {
		s.writeJSON(w, http.StatusForbidden, map[string]any{
			"error": map[string]string{
				"message": "insufficient permissions",
				"type":    "authentication_error",
			},
		})
		return false
	}

	return true
}

// extractUserPrompt extracts the text from the last user message in the messages array.
// Normalizes deprecated "function" role to "tool".
func extractUserPrompt(messages []OpenAIMessage) string {
	// Walk from the end to find the last user message.
	for i := len(messages) - 1; i >= 0; i-- {
		role := messages[i].Role
		if role == "function" {
			role = "tool"
		}
		if role == "user" {
			text := extractMessageText(messages[i])
			if text != "" {
				return text
			}
		}
	}
	return ""
}

// extractSystemPrompt collects all system/developer messages into a combined prompt.
func extractSystemPrompt(messages []OpenAIMessage) string {
	var parts []string
	for _, msg := range messages {
		if msg.Role == "system" || msg.Role == "developer" {
			text := extractMessageText(msg)
			if text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, "\n")
}

// hasImageContent checks if any message contains image_url content parts.
func hasImageContent(messages []OpenAIMessage) bool {
	for _, msg := range messages {
		if parts, ok := msg.Content.([]any); ok {
			for _, item := range parts {
				if m, ok := item.(map[string]any); ok {
					if t, _ := m["type"].(string); t == "image_url" {
						return true
					}
				}
			}
		}
	}
	return false
}

// extractMessageText extracts text from a message content field.
// Content can be a plain string or an array of content parts.
// Handles both "text" and "input_text" part types.
func extractMessageText(msg OpenAIMessage) string {
	switch c := msg.Content.(type) {
	case string:
		return c
	case []any:
		var parts []string
		for _, item := range c {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			partType, _ := m["type"].(string)
			if partType == "text" || partType == "input_text" {
				if text, ok := m["text"].(string); ok {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// writeSSEData marshals v as JSON and writes it as an SSE data event.
func writeSSEData(w http.ResponseWriter, flusher http.Flusher, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}

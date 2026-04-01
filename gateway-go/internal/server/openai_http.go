// Package server — OpenAI Chat Completions HTTP API handler.
//
// Implements POST /v1/chat/completions, accepting OpenAI-compatible requests
// and proxying them through the Go gateway's native chat handler.
// Supports both non-streaming and streaming (SSE) responses.
//
// Also implements GET /v1/models for model discovery.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/auth"
	"github.com/choiceoh/deneb/gateway-go/internal/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/shortid"
)

// maxChatCompletionBodyBytes is the default body size limit for /v1/chat/completions.
const maxChatCompletionBodyBytes = 20 * 1024 * 1024 // 20 MB

// --- Request types ---

// OpenAIChatRequest is the inbound request body for /v1/chat/completions.
type OpenAIChatRequest struct {
	Model               string          `json:"model"`
	Messages            []OpenAIMessage `json:"messages"`
	Stream              bool            `json:"stream,omitempty"`
	User                string          `json:"user,omitempty"`
	Temperature         *float64        `json:"temperature,omitempty"`
	TopP                *float64        `json:"top_p,omitempty"`
	MaxTokens           *int            `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int            `json:"max_completion_tokens,omitempty"`
	FrequencyPenalty    *float64        `json:"frequency_penalty,omitempty"`
	PresencePenalty     *float64        `json:"presence_penalty,omitempty"`
	Stop                json.RawMessage `json:"stop,omitempty"` // string or []string
	Seed                *int            `json:"seed,omitempty"` // deterministic sampling
	ResponseFormat      json.RawMessage `json:"response_format,omitempty"`
	Tools               json.RawMessage `json:"tools,omitempty"`       // preserved for pass-through
	ToolChoice          json.RawMessage `json:"tool_choice,omitempty"` // "auto", "none", etc.
	ParallelToolCalls   *bool           `json:"parallel_tool_calls,omitempty"`
	StreamOptions       *StreamOptions  `json:"stream_options,omitempty"`
}

// StreamOptions controls streaming behavior.
type StreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// OpenAIMessage represents a single message in the chat completion request.
type OpenAIMessage struct {
	Role       string          `json:"role"`
	Content    any             `json:"content"` // string or []ContentPart
	Name       string          `json:"name,omitempty"`
	ToolCalls  json.RawMessage `json:"tool_calls,omitempty"`   // assistant tool calls
	ToolCallID string          `json:"tool_call_id,omitempty"` // tool result reference
}

// OpenAIContentPart is a typed content block within a multi-part message.
type OpenAIContentPart struct {
	Type     string       `json:"type"`
	Text     string       `json:"text,omitempty"`
	ImageURL *oaiImageURL `json:"image_url,omitempty"`
}

type oaiImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

// --- Response types ---

// openAIChatResponse is the non-streaming response envelope.
type openAIChatResponse struct {
	ID                string             `json:"id"`
	Object            string             `json:"object"`
	Created           int64              `json:"created"`
	Model             string             `json:"model"`
	SystemFingerprint string             `json:"system_fingerprint,omitempty"`
	Choices           []openAIChatChoice `json:"choices"`
	Usage             *openAIUsageResp   `json:"usage,omitempty"`
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

type openAIUsageResp struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// --- OpenAI error response ---

type openAIErrorResponse struct {
	Error openAIErrorBody `json:"error"`
}

type openAIErrorBody struct {
	Message string  `json:"message"`
	Type    string  `json:"type"`
	Param   *string `json:"param"`
	Code    *string `json:"code"`
}

func writeOpenAIError(w http.ResponseWriter, s *Server, status int, msg, errType string) {
	s.writeJSON(w, status, openAIErrorResponse{
		Error: openAIErrorBody{
			Message: msg,
			Type:    errType,
		},
	})
}

// --- Models types ---

type openAIModelsResponse struct {
	Object string           `json:"object"` // "list"
	Data   []openAIModelObj `json:"data"`
}

type openAIModelObj struct {
	ID      string `json:"id"`
	Object  string `json:"object"` // "model"
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// --- Handlers ---

// handleChatCompletions handles POST /v1/chat/completions.
func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check if endpoint is enabled via config.
	if !s.isChatCompletionsEnabled() {
		writeOpenAIError(w, s, http.StatusNotFound, "chat completions endpoint is not enabled", "not_found")
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
		writeOpenAIError(w, s, http.StatusBadRequest, "invalid request body: "+err.Error(), "invalid_request_error")
		return
	}

	// Validate model is non-empty.
	if req.Model == "" {
		writeOpenAIError(w, s, http.StatusBadRequest, "model is required", "invalid_request_error")
		return
	}

	if len(req.Messages) == 0 {
		writeOpenAIError(w, s, http.StatusBadRequest, "messages array is required and must not be empty", "invalid_request_error")
		return
	}

	// Convert messages to validate user content before checking handler availability,
	// so 400 errors surface before 503.
	systemPrompt, llmMessages, lastUserText := convertOpenAIMessages(req.Messages)

	if lastUserText == "" {
		writeOpenAIError(w, s, http.StatusBadRequest, "at least one user message is required", "invalid_request_error")
		return
	}

	// Check chat handler availability (after validation to surface 400 errors first).
	if s.chatHandler == nil {
		writeOpenAIError(w, s, http.StatusServiceUnavailable, "chat handler not available", "server_error")
		return
	}

	// Build sync options from request parameters.
	opts := buildSyncOptions(req, systemPrompt, llmMessages)

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
		s.handleChatCompletionsStream(w, r, req, sessionKey, lastUserText, opts)
	} else {
		s.handleChatCompletionsSync(w, r, req, sessionKey, lastUserText, opts)
	}
}

// handleChatCompletionsSync handles non-streaming chat completions.
func (s *Server) handleChatCompletionsSync(w http.ResponseWriter, r *http.Request, req OpenAIChatRequest, sessionKey, prompt string, opts *chat.SyncOptions) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	result, err := s.chatHandler.SendSync(ctx, sessionKey, prompt, req.Model, opts)
	if err != nil {
		s.logger.Error("chat completion failed", "error", err)
		writeOpenAIError(w, s, http.StatusInternalServerError, "internal error: "+err.Error(), "server_error")
		return
	}

	completionID := "chatcmpl-" + shortid.New("cc")
	finishReason := mapStopReason(result.StopReason)

	resp := openAIChatResponse{
		ID:      completionID,
		Object:  "chat.completion",
		Created: time.Now().Unix(),
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
		Usage: &openAIUsageResp{
			PromptTokens:     result.InputTokens,
			CompletionTokens: result.OutputTokens,
			TotalTokens:      result.InputTokens + result.OutputTokens,
		},
	}

	s.writeJSON(w, http.StatusOK, resp)
}

// handleChatCompletionsStream handles streaming chat completions via SSE.
func (s *Server) handleChatCompletionsStream(w http.ResponseWriter, r *http.Request, req OpenAIChatRequest, sessionKey, prompt string, opts *chat.SyncOptions) {
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

	completionID := "chatcmpl-" + shortid.New("cc")
	now := time.Now().Unix()

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
	includeUsage := req.StreamOptions != nil && req.StreamOptions.IncludeUsage
	result, err := s.chatHandler.SendSyncStream(ctx, sessionKey, prompt, req.Model, opts, func(delta string) {
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
		writeSSEData(w, flusher, openAIErrorResponse{
			Error: openAIErrorBody{
				Message: "stream error: " + err.Error(),
				Type:    "server_error",
			},
		})
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
		return
	}

	// Send final chunk with finish_reason.
	finishReason := "stop"
	if result != nil {
		finishReason = mapStopReason(result.StopReason)
	}
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

	// Send usage chunk if requested via stream_options.
	if includeUsage && result != nil {
		writeSSEData(w, flusher, openAIChatResponse{
			ID:      completionID,
			Object:  "chat.completion.chunk",
			Created: now,
			Model:   finalModel,
			Choices: []openAIChatChoice{},
			Usage: &openAIUsageResp{
				PromptTokens:     result.InputTokens,
				CompletionTokens: result.OutputTokens,
				TotalTokens:      result.InputTokens + result.OutputTokens,
			},
		})
	}

	fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

// handleModels handles GET /v1/models.
func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Auth: extract Bearer token and validate.
	if !s.authenticateHTTP(w, r) {
		return
	}

	models := s.getAvailableModels()
	s.writeJSON(w, http.StatusOK, openAIModelsResponse{
		Object: "list",
		Data:   models,
	})
}

// getAvailableModels returns available models as OpenAI model objects.
func (s *Server) getAvailableModels() []openAIModelObj {
	var models []openAIModelObj
	seen := make(map[string]bool)

	// Pull from model role registry if available.
	if s.modelRegistry != nil {
		for _, cfg := range s.modelRegistry.ConfiguredModels() {
			fullID := cfg.Model
			if cfg.ProviderID != "" {
				fullID = cfg.ProviderID + "/" + cfg.Model
			}
			if seen[fullID] {
				continue
			}
			seen[fullID] = true
			owner := cfg.ProviderID
			if owner == "" {
				owner = "deneb"
			}
			models = append(models, openAIModelObj{
				ID:      fullID,
				Object:  "model",
				Created: 0,
				OwnedBy: owner,
			})
		}
	}

	// Fallback: return a single model from default config.
	if len(models) == 0 && s.chatHandler != nil {
		models = append(models, openAIModelObj{
			ID:      "default",
			Object:  "model",
			Created: 0,
			OwnedBy: "deneb",
		})
	}

	return models
}

// --- Helpers ---

// buildSyncOptions creates SyncOptions from an OpenAI chat request.
func buildSyncOptions(req OpenAIChatRequest, systemPrompt string, messages []llm.Message) *chat.SyncOptions {
	opts := &chat.SyncOptions{
		Temperature:      req.Temperature,
		TopP:             req.TopP,
		FrequencyPenalty: req.FrequencyPenalty,
		PresencePenalty:  req.PresencePenalty,
	}

	// Resolve max tokens: prefer max_completion_tokens over max_tokens.
	if req.MaxCompletionTokens != nil {
		opts.MaxTokens = req.MaxCompletionTokens
	} else if req.MaxTokens != nil {
		opts.MaxTokens = req.MaxTokens
	}

	// Parse stop sequences (string or []string).
	if len(req.Stop) > 0 {
		var stopStr string
		if json.Unmarshal(req.Stop, &stopStr) == nil {
			opts.Stop = []string{stopStr}
		} else {
			var stopArr []string
			if json.Unmarshal(req.Stop, &stopArr) == nil {
				opts.Stop = stopArr
			}
		}
	}

	// Parse response format.
	if len(req.ResponseFormat) > 0 {
		var rf llm.ResponseFormat
		if json.Unmarshal(req.ResponseFormat, &rf) == nil && rf.Type != "" {
			opts.ResponseFormat = &rf
		}
	}

	// Tool choice (pass through as-is).
	if len(req.ToolChoice) > 0 {
		var tc any
		if json.Unmarshal(req.ToolChoice, &tc) == nil {
			opts.ToolChoice = tc
		}
	}

	// Set conversation messages if multi-turn.
	if len(messages) > 0 {
		opts.Messages = messages
	}

	// Set system prompt.
	if systemPrompt != "" {
		opts.SystemPrompt = systemPrompt
	}

	return opts
}

// convertOpenAIMessages converts OpenAI-format messages to internal llm.Message format.
// Returns extracted system prompt, converted messages, and the last user message text.
func convertOpenAIMessages(messages []OpenAIMessage) (systemPrompt string, llmMsgs []llm.Message, lastUserText string) {
	var systemParts []string

	for _, msg := range messages {
		role := msg.Role
		// Normalize deprecated "function" role to "tool".
		if role == "function" {
			role = "tool"
		}

		switch role {
		case "system", "developer":
			text := extractMessageText(msg)
			if text != "" {
				systemParts = append(systemParts, text)
			}

		case "user":
			text := extractMessageText(msg)
			if text != "" {
				lastUserText = text
			}

			// Check for multipart content with images.
			blocks := extractContentBlocks(msg)
			if len(blocks) > 0 {
				llmMsgs = append(llmMsgs, llm.NewBlockMessage("user", blocks))
			} else if text != "" {
				llmMsgs = append(llmMsgs, llm.NewTextMessage("user", text))
			}

		case "assistant":
			text := extractMessageText(msg)
			// Check for tool calls.
			var toolCalls []assistantToolCall
			if len(msg.ToolCalls) > 0 {
				_ = json.Unmarshal(msg.ToolCalls, &toolCalls)
			}

			if len(toolCalls) > 0 {
				// Build content blocks: text + tool_use blocks.
				var blocks []llm.ContentBlock
				if text != "" {
					blocks = append(blocks, llm.ContentBlock{Type: "text", Text: text})
				}
				for _, tc := range toolCalls {
					input := json.RawMessage("{}")
					if tc.Function.Arguments != "" {
						input = json.RawMessage(tc.Function.Arguments)
					}
					blocks = append(blocks, llm.ContentBlock{
						Type:  "tool_use",
						ID:    tc.ID,
						Name:  tc.Function.Name,
						Input: input,
					})
				}
				llmMsgs = append(llmMsgs, llm.NewBlockMessage("assistant", blocks))
			} else if text != "" {
				llmMsgs = append(llmMsgs, llm.NewTextMessage("assistant", text))
			}

		case "tool":
			// Tool result message.
			block := llm.ContentBlock{
				Type:      "tool_result",
				ToolUseID: msg.ToolCallID,
				Content:   extractMessageText(msg),
			}
			llmMsgs = append(llmMsgs, llm.NewBlockMessage("user", []llm.ContentBlock{block}))
		}
	}

	systemPrompt = strings.Join(systemParts, "\n")
	return
}

// assistantToolCall represents a tool call in an assistant message.
type assistantToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// extractContentBlocks parses multipart content into llm.ContentBlock slice.
// Returns nil if content is a plain string (caller should use extractMessageText).
func extractContentBlocks(msg OpenAIMessage) []llm.ContentBlock {
	parts, ok := msg.Content.([]any)
	if !ok {
		return nil
	}

	var blocks []llm.ContentBlock
	hasNonText := false

	for _, item := range parts {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		partType, _ := m["type"].(string)
		switch partType {
		case "text":
			if text, _ := m["text"].(string); text != "" {
				blocks = append(blocks, llm.ContentBlock{Type: "text", Text: text})
			}
		case "input_text":
			if text, _ := m["text"].(string); text != "" {
				blocks = append(blocks, llm.ContentBlock{Type: "text", Text: text})
			}
		case "image_url":
			hasNonText = true
			if imgObj, ok := m["image_url"].(map[string]any); ok {
				url, _ := imgObj["url"].(string)
				detail, _ := imgObj["detail"].(string)
				if url != "" {
					blocks = append(blocks, llm.ContentBlock{
						Type: "image_url",
						ImageURL: &llm.ImageURL{
							URL:    url,
							Detail: detail,
						},
					})
				}
			}
		}
	}

	// Only return blocks if there's non-text content (images).
	// Plain text content is handled by extractMessageText.
	if !hasNonText {
		return nil
	}
	return blocks
}

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
		writeOpenAIError(w, s, http.StatusUnauthorized, "missing Authorization header with Bearer token", "authentication_error")
		return false
	}

	claims, err := s.authValidator.ValidateToken(token)
	if err != nil {
		writeOpenAIError(w, s, http.StatusUnauthorized, "invalid token: "+err.Error(), "authentication_error")
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
		writeOpenAIError(w, s, http.StatusForbidden, "insufficient permissions", "authentication_error")
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

// mapStopReason maps internal stop reasons to OpenAI finish_reason values.
func mapStopReason(stopReason string) string {
	switch stopReason {
	case "end_turn", "":
		return "stop"
	case "max_tokens":
		return "length"
	case "tool_use":
		return "tool_calls"
	case "content_filtered":
		return "content_filter"
	default:
		return "stop"
	}
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

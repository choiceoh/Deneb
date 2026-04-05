// Package server — Anthropic Messages API proxy (Claudeneb).
//
// Implements POST /v1/messages so Claude Desktop can use Deneb as its backend
// via ANTHROPIC_BASE_URL. Receives Anthropic format from Claude Desktop,
// converts to OpenAI format, sends to the configured OpenAI-compatible backend
// (local AI, z.ai, vLLM), converts the response back to Anthropic SSE.
//
// No Anthropic API key needed. Deneb context (SOUL.md, MEMORY.md) is injected
// into the system prompt. Claude Code's local tools work client-side as normal.
package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/chat/prompt"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

const maxAnthropicMessagesBodyBytes = 20 * 1024 * 1024

// defaultOpenAIURL is the fallback when CLAUDENEB_OPENAI_URL is not set.
const defaultOpenAIURL = "http://127.0.0.1:30000/v1"

// --- Request types ---

type anthropicMessagesRequest struct {
	Model         string             `json:"model"`
	Messages      []anthropicMessage `json:"messages"`
	System        json.RawMessage    `json:"system,omitempty"`
	MaxTokens     int                `json:"max_tokens"`
	Stream        *bool              `json:"stream,omitempty"`
	Temperature   *float64           `json:"temperature,omitempty"`
	TopP          *float64           `json:"top_p,omitempty"`
	TopK          *int               `json:"top_k,omitempty"`
	StopSequences []string           `json:"stop_sequences,omitempty"`
	Tools         json.RawMessage    `json:"tools,omitempty"`
	ToolChoice    json.RawMessage    `json:"tool_choice,omitempty"`
	Thinking      json.RawMessage    `json:"thinking,omitempty"`
}

type anthropicMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// --- Error response ---

type anthropicErrorResponse struct {
	Type  string             `json:"type"`
	Error anthropicErrorBody `json:"error"`
}

type anthropicErrorBody struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

func writeAnthropicError(w http.ResponseWriter, s *Server, status int, msg, errType string) {
	s.writeJSON(w, status, anthropicErrorResponse{
		Type:  "error",
		Error: anthropicErrorBody{Type: errType, Message: msg},
	})
}

// --- Handler ---

func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	if !s.isAnthropicMessagesEnabled() {
		writeAnthropicError(w, s, http.StatusNotFound, "messages endpoint is not enabled", "not_found")
		return
	}

	// Accept any non-empty token (Claude Desktop always sends one).
	token := r.Header.Get("x-api-key")
	if token == "" {
		token = extractBearerToken(r)
	}
	if token == "" {
		writeAnthropicError(w, s, http.StatusUnauthorized, "missing authentication", "authentication_error")
		return
	}

	maxBody := int64(maxAnthropicMessagesBodyBytes)
	if s.runtimeCfg != nil && s.runtimeCfg.AnthropicMessagesConfig != nil &&
		s.runtimeCfg.AnthropicMessagesConfig.MaxBodyBytes != nil {
		maxBody = int64(*s.runtimeCfg.AnthropicMessagesConfig.MaxBodyBytes)
	}
	rawBody, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		writeAnthropicError(w, s, http.StatusBadRequest, "failed to read request body", "invalid_request_error")
		return
	}

	var req anthropicMessagesRequest
	if err := json.Unmarshal(rawBody, &req); err != nil {
		writeAnthropicError(w, s, http.StatusBadRequest, "invalid request body: "+err.Error(), "invalid_request_error")
		return
	}
	if req.Model == "" {
		writeAnthropicError(w, s, http.StatusBadRequest, "model is required", "invalid_request_error")
		return
	}
	if len(req.Messages) == 0 {
		writeAnthropicError(w, s, http.StatusBadRequest, "messages is required", "invalid_request_error")
		return
	}

	// Inject Deneb context into the system prompt.
	injectedSystem := s.injectDenebSystem(req.System)

	// Build llm.ChatRequest (Anthropic → internal format).
	chatReq := llm.ChatRequest{
		Model:         resolveOpenAIModel(req.Model),
		MaxTokens:     req.MaxTokens,
		System:        injectedSystem,
		Stream:        true,
		Temperature:   req.Temperature,
		TopP:          req.TopP,
		TopK:          req.TopK,
		StopSequences: req.StopSequences,
	}

	for _, m := range req.Messages {
		chatReq.Messages = append(chatReq.Messages, llm.Message{
			Role: m.Role, Content: m.Content,
		})
	}

	if len(req.Tools) > 0 {
		var tools []llm.Tool
		if json.Unmarshal(req.Tools, &tools) == nil {
			chatReq.Tools = tools
		}
	}
	if len(req.ToolChoice) > 0 {
		var tc any
		if json.Unmarshal(req.ToolChoice, &tc) == nil {
			chatReq.ToolChoice = tc
		}
	}
	if len(req.Thinking) > 0 {
		var thinking llm.ThinkingConfig
		if json.Unmarshal(req.Thinking, &thinking) == nil {
			chatReq.Thinking = &thinking
		}
	}

	// Send to OpenAI-compatible backend via llm.Client.StreamChat().
	// StreamChat handles Anthropic→OpenAI conversion and returns Anthropic-style StreamEvents.
	openaiURL := os.Getenv("CLAUDENEB_OPENAI_URL")
	if openaiURL == "" {
		openaiURL = defaultOpenAIURL
	}
	openaiKey := os.Getenv("CLAUDENEB_OPENAI_KEY")

	client := llm.NewClient(
		strings.TrimRight(openaiURL, "/"),
		openaiKey,
		llm.WithLogger(s.logger),
		llm.WithRetry(3, 500*time.Millisecond, 30*time.Second),
	)

	events, err := client.StreamChat(r.Context(), chatReq)
	if err != nil {
		s.logger.Error("OpenAI backend request failed", "error", err, "url", openaiURL)
		writeAnthropicError(w, s, http.StatusBadGateway, "backend request failed: "+err.Error(), "api_error")
		return
	}

	isStream := req.Stream != nil && *req.Stream
	if !isStream {
		s.writeNonStreamingResponse(w, events, req.Model)
		return
	}

	// Stream Anthropic SSE to Claude Desktop.
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeAnthropicError(w, s, http.StatusInternalServerError, "streaming not supported", "api_error")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	for ev := range events {
		if ev.Type == "error" {
			fmt.Fprintf(w, "event: error\ndata: %s\n\n", ev.Payload)
			flusher.Flush()
			return
		}
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, ev.Payload)
		flusher.Flush()
	}
}

// writeNonStreamingResponse collects stream events into a single Anthropic JSON response.
func (s *Server) writeNonStreamingResponse(w http.ResponseWriter, events <-chan llm.StreamEvent, model string) {
	var (
		text         strings.Builder
		inputTokens  int
		outputTokens int
		stopReason   = "end_turn"
	)

	for ev := range events {
		switch ev.Type {
		case "message_start":
			var ms llm.MessageStart
			if json.Unmarshal(ev.Payload, &ms) == nil {
				inputTokens = ms.Message.Usage.InputTokens
				if ms.Message.Model != "" {
					model = ms.Message.Model
				}
			}
		case "content_block_delta":
			var cbd llm.ContentBlockDelta
			if json.Unmarshal(ev.Payload, &cbd) == nil {
				text.WriteString(cbd.Delta.Text)
			}
		case "message_delta":
			var md llm.MessageDelta
			if json.Unmarshal(ev.Payload, &md) == nil {
				outputTokens = md.Usage.OutputTokens
				if md.Delta.StopReason != "" {
					stopReason = md.Delta.StopReason
				}
			}
		}
	}

	s.writeJSON(w, http.StatusOK, map[string]any{
		"id":          "msg_claudeneb",
		"type":        "message",
		"role":        "assistant",
		"model":       model,
		"content":     []map[string]string{{"type": "text", "text": text.String()}},
		"stop_reason": stopReason,
		"usage":       map[string]int{"input_tokens": inputTokens, "output_tokens": outputTokens},
	})
}

// --- Deneb context injection ---

func (s *Server) injectDenebSystem(existingSystem json.RawMessage) json.RawMessage {
	denebCtx := s.buildDenebContextString()
	if denebCtx == "" {
		return existingSystem
	}
	result := llm.AppendSystemText(existingSystem, denebCtx)
	if result == nil {
		return llm.SystemString(denebCtx)
	}
	return result
}

func (s *Server) buildDenebContextString() string {
	workspaceDir, _ := os.Getwd()
	files := prompt.LoadContextFiles(workspaceDir)
	if len(files) == 0 {
		return ""
	}
	return prompt.FormatContextFilesForPrompt(files)
}

// --- Helpers ---

func (s *Server) isAnthropicMessagesEnabled() bool {
	return s.runtimeCfg != nil && s.runtimeCfg.AnthropicMessagesEnabled
}

// resolveOpenAIModel maps Anthropic model names to the backend model.
// CLAUDENEB_MODEL env var overrides everything.
func resolveOpenAIModel(model string) string {
	if m := os.Getenv("CLAUDENEB_MODEL"); m != "" {
		return m
	}
	return model
}

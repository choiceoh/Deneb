package openaiapi

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
)

// defaultMaxTokens is applied when the OpenAI request omits max_tokens.
// Anthropic-style upstream requires the field; OpenAI clients commonly
// leave it unset.
const defaultMaxTokens = 4096

// roleAlias resolves an OpenAI request "model" value to a Deneb model
// role. Only the deneb-* alias set is recognized; raw provider model
// names are not yet supported at this layer.
func roleAlias(model string) (modelrole.Role, bool) {
	for _, a := range modelAliases {
		if a.ID == model {
			return a.Role, true
		}
	}
	return "", false
}

// translateRequest converts an OpenAI chat completions request into
// Deneb's internal Anthropic-style llm.ChatRequest.
//
// Scope (v1):
//   - Text-only content (string or array-of-text-parts).
//   - System messages collapsed into ChatRequest.System.
//   - Tools, tool_calls, tool results, images, response_format are NOT
//     yet translated — they arrive in a follow-up commit. Presence of
//     unsupported fields is tolerated; the translator drops them.
func translateRequest(body chatCompletionsRequest, reg ModelRegistry, role modelrole.Role) (llm.ChatRequest, error) {
	if len(body.Messages) == 0 {
		return llm.ChatRequest{}, fmt.Errorf("messages: must contain at least one message")
	}

	var systemParts []string
	var msgs []llm.Message
	for i, m := range body.Messages {
		text, err := extractText(m.Content)
		if err != nil {
			return llm.ChatRequest{}, fmt.Errorf("messages[%d].content: %w", i, err)
		}
		switch m.Role {
		case "system":
			if text != "" {
				systemParts = append(systemParts, text)
			}
		case "user", "assistant":
			msgs = append(msgs, llm.NewTextMessage(m.Role, text))
		case "tool":
			// Tool results require translation to Anthropic tool_result
			// content blocks; deferred to the tools commit. Skip silently
			// rather than reject so a request with tool history doesn't
			// 400 outright — the model may still produce a reasonable
			// response from the user/assistant context.
			continue
		default:
			return llm.ChatRequest{}, fmt.Errorf("messages[%d].role: unsupported role %q", i, m.Role)
		}
	}

	if len(msgs) == 0 {
		return llm.ChatRequest{}, fmt.Errorf("messages: must contain at least one user or assistant message")
	}

	maxTokens := defaultMaxTokens
	if body.MaxTokens != nil && *body.MaxTokens > 0 {
		maxTokens = *body.MaxTokens
	}

	chatReq := llm.ChatRequest{
		Model:       reg.Model(role),
		Messages:    msgs,
		System:      llm.SystemString(strings.Join(systemParts, "\n\n")),
		MaxTokens:   maxTokens,
		Stream:      true, // upstream is always streamed; non-stream is assembled here
		Temperature: body.Temperature,
		TopP:        body.TopP,
	}
	if stops := normalizeStop(body.Stop); len(stops) > 0 {
		chatReq.StopSequences = stops
	}
	return chatReq, nil
}

// extractText pulls a plain text representation out of an OpenAI
// content field. Accepts string form and array-of-parts form (text
// parts only — non-text parts are silently dropped in v1).
func extractText(raw any) (string, error) {
	if raw == nil {
		return "", nil
	}
	if s, ok := raw.(string); ok {
		return s, nil
	}
	// Array form: re-marshal then decode as parts. The any-typed field
	// arrives as []interface{} from json.Decoder; round-tripping is the
	// least surprising way to extract typed parts without a custom
	// UnmarshalJSON on chatMessage.
	buf, err := json.Marshal(raw)
	if err != nil {
		return "", fmt.Errorf("re-marshal: %w", err)
	}
	var parts []chatContentPart
	if err := json.Unmarshal(buf, &parts); err != nil {
		return "", fmt.Errorf("expected string or array of content parts: %w", err)
	}
	var b strings.Builder
	for _, p := range parts {
		if p.Type == "text" {
			b.WriteString(p.Text)
		}
	}
	return b.String(), nil
}

type chatContentPart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// normalizeStop converts OpenAI's stop field (string or []string) into
// Anthropic-style stop_sequences.
func normalizeStop(raw any) []string {
	switch v := raw.(type) {
	case nil:
		return nil
	case string:
		if v == "" {
			return nil
		}
		return []string{v}
	case []any:
		out := make([]string, 0, len(v))
		for _, e := range v {
			if s, ok := e.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// accumulateNonStream drains an llm.StreamEvent channel and assembles
// a single OpenAI non-stream chat completion response. The channel is
// closed by the upstream client at message_stop.
func accumulateNonStream(events <-chan llm.StreamEvent, requestModel string, created int64) chatCompletionResponse {
	var (
		msgID        string
		contentBuf   strings.Builder
		stopReason   string
		inputTokens  int
		outputTokens int
	)

	for ev := range events {
		switch ev.Type {
		case "message_start":
			var p llm.MessageStart
			if err := json.Unmarshal(ev.Payload, &p); err != nil {
				continue
			}
			msgID = p.Message.ID
			inputTokens = p.Message.Usage.InputTokens
			if p.Message.Usage.OutputTokens > outputTokens {
				outputTokens = p.Message.Usage.OutputTokens
			}
		case "content_block_delta":
			var p llm.ContentBlockDelta
			if err := json.Unmarshal(ev.Payload, &p); err != nil {
				continue
			}
			if p.Delta.Type == "text_delta" {
				contentBuf.WriteString(p.Delta.Text)
			}
			// input_json_delta (tool args) intentionally ignored in v1 —
			// tool call surfacing arrives in the tools commit.
		case "message_delta":
			var p llm.MessageDelta
			if err := json.Unmarshal(ev.Payload, &p); err != nil {
				continue
			}
			if p.Delta.StopReason != "" {
				stopReason = p.Delta.StopReason
			}
			if p.Usage.OutputTokens > 0 {
				outputTokens = p.Usage.OutputTokens
			}
		}
	}

	return chatCompletionResponse{
		ID:      msgIDOrFallback(msgID),
		Object:  "chat.completion",
		Created: created,
		Model:   requestModel,
		Choices: []chatChoice{{
			Index: 0,
			Message: chatResponseMessage{
				Role:    "assistant",
				Content: contentBuf.String(),
			},
			FinishReason: mapStopReason(stopReason),
		}},
		Usage: chatUsage{
			PromptTokens:     inputTokens,
			CompletionTokens: outputTokens,
			TotalTokens:      inputTokens + outputTokens,
		},
	}
}

// msgIDOrFallback ensures the response carries a non-empty id even if
// the upstream provider omitted message_start.id. Some OpenAI clients
// reject responses with an empty id.
func msgIDOrFallback(id string) string {
	if id != "" {
		return id
	}
	return "chatcmpl-deneb"
}

// mapStopReason translates Anthropic stop_reason values into OpenAI
// finish_reason values.
func mapStopReason(s string) string {
	switch s {
	case "max_tokens":
		return "length"
	case "tool_use":
		return "tool_calls"
	case "":
		return "stop"
	default:
		return "stop"
	}
}

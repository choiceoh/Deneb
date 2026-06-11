// openai_complete.go — single-turn Complete path of the LLM client:
// mode dispatch, streaming-reuse for Anthropic, and the non-streaming
// OpenAI-compatible /chat/completions request/response handling.
// Split from openai.go (pure move, no behavior change).
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// Complete sends a single-turn request and returns the assistant text.
// Intended for lightweight tasks (thread titles, classifiers).
//
// Dispatches by client API mode:
//   - openai: non-streaming POST /chat/completions
//   - anthropic: streaming POST /v1/messages, concatenated text deltas
//
// The streaming reuse for anthropic keeps a single wire path; the upstream
// HTTP cost is the same and the caller still sees a synchronous string.
func (c *Client) Complete(ctx context.Context, req ChatRequest) (string, error) {
	if c.apiMode == APIModeAnthropic {
		return c.completeViaStream(ctx, req)
	}
	return c.completeOpenAI(ctx, req)
}

// completeViaStream consumes the streaming chat as a one-shot Complete,
// concatenating text deltas. Used for Anthropic-mode clients where
// /v1/messages does not have a non-streaming sibling endpoint.
func (c *Client) completeViaStream(ctx context.Context, req ChatRequest) (string, error) {
	events, err := c.StreamChat(ctx, req)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	for ev := range events {
		if ev.Type != "content_block_delta" {
			continue
		}
		var cbd ContentBlockDelta
		if json.Unmarshal(ev.Payload, &cbd) != nil {
			continue
		}
		if cbd.Delta.Type == "text_delta" {
			sb.WriteString(cbd.Delta.Text)
		}
	}
	out := strings.TrimSpace(sb.String())
	out = jsonutil.StripThinkingTags(out)
	out = jsonutil.StripThinkingPreamble(out)
	return strings.TrimSpace(out), nil
}

// completeOpenAI sends a non-streaming request to an OpenAI-compatible
// /chat/completions endpoint and returns the full response text.
func (c *Client) completeOpenAI(ctx context.Context, req ChatRequest) (string, error) {
	oaiReq := openAIRequest{
		Model:     req.Model,
		Stream:    false,
		MaxTokens: req.MaxTokens,
	}

	// System prompt → system message.
	if systemText := ExtractSystemText(req.System); systemText != "" {
		oaiReq.Messages = append(oaiReq.Messages, openAIMessage{
			Role:    "system",
			Content: systemText,
		})
	}

	// User messages (text only — title generation doesn't need multimodal).
	for _, m := range req.Messages {
		var text string
		if err := json.Unmarshal(m.Content, &text); err == nil {
			oaiReq.Messages = append(oaiReq.Messages, openAIMessage{
				Role:    m.Role,
				Content: text,
			})
		}
	}

	body, err := json.Marshal(oaiReq)
	if err != nil {
		return "", fmt.Errorf("marshal openai request: %w", err)
	}

	// Merge ExtraBody fields (e.g., local AI's chat_template_kwargs, timeout).
	if len(req.ExtraBody) > 0 {
		body, err = mergeJSONFields(body, req.ExtraBody)
		if err != nil {
			return "", fmt.Errorf("merge extra body: %w", err)
		}
	}

	url := c.baseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	c.setOpenAIBearerAuth(httpReq)
	c.applyHeaders(httpReq)

	respBody, err := c.DoStream(ctx, httpReq)
	if err != nil {
		return "", err
	}
	defer respBody.Close()

	data, err := io.ReadAll(io.LimitReader(respBody, 64*1024))
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
				Refusal string `json:"refusal"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}
	msg := resp.Choices[0].Message
	if strings.TrimSpace(msg.Content) == "" && msg.Refusal != "" {
		// A refusal arrives on `refusal` with content null. Returning "" with
		// a nil error would let background callers (wiki dreamer/verify/merge)
		// treat the refusal as a successful empty result.
		return "", fmt.Errorf("model refused: %s", truncateForLog(msg.Refusal, 200))
	}
	// Strip reasoning model artifacts (<think> tags, "Thinking Process:" preamble)
	// that leak into the content field of some local models (DeepSeek-R1, QwQ, etc.).
	content := strings.TrimSpace(msg.Content)
	content = jsonutil.StripThinkingTags(content)
	content = jsonutil.StripThinkingPreamble(content)
	return strings.TrimSpace(content), nil
}

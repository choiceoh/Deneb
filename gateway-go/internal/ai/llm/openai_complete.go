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

	// Honor caller sampling/thinking parameters (temperature, top_p, stop,
	// reasoning_effort mapping). Previously dropped on this path, so e.g. a
	// deterministic temperature=0 classifier silently ran at server default.
	applySamplingParams(&oaiReq, &req)

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
				Content          string `json:"content"`
				Refusal          string `json:"refusal"`
				Reasoning        string `json:"reasoning"`         // vLLM reasoning-parser output
				ReasoningContent string `json:"reasoning_content"` // DeepSeek/OpenRouter spelling
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}
	choice := resp.Choices[0]
	msg := choice.Message
	if strings.TrimSpace(msg.Content) == "" {
		if msg.Refusal != "" {
			// A refusal arrives on `refusal` with content null. Returning "" with
			// a nil error would let background callers (wiki dreamer/verify/merge)
			// treat the refusal as a successful empty result.
			return "", fmt.Errorf("model refused: %s", truncateForLog(msg.Refusal, 200))
		}
		// Reasoning models can burn the whole output budget on the reasoning
		// channel and finish with content null — observed live on
		// deepseek-v4-flash (server default thinking) with small max_tokens.
		// Treat as an error: "" with nil error reads as a successful empty
		// result to background callers, which silently drops their work.
		if reasoning := strings.TrimSpace(msg.Reasoning + msg.ReasoningContent); reasoning != "" || choice.FinishReason == "length" {
			return "", fmt.Errorf(
				"empty content (finish_reason=%s, reasoning_chars=%d): reasoning consumed the output budget — raise MaxTokens or disable thinking",
				choice.FinishReason, len(reasoning))
		}
	}
	// Strip reasoning model artifacts (<think> tags, "Thinking Process:" preamble)
	// that leak into the content field of some local models (DeepSeek-R1, QwQ, etc.).
	content := strings.TrimSpace(msg.Content)
	content = jsonutil.StripThinkingTags(content)
	content = jsonutil.StripThinkingPreamble(content)
	return strings.TrimSpace(content), nil
}

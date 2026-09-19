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
//
// Mirrors completeOpenAI's failure semantics: a mid-stream error event or an
// empty answer whose budget was eaten by the reasoning channel must surface as
// an error — "" (or partial text) with a nil error reads as a successful
// result to background callers (wiki dreamer/verify/merge, gmail analysis
// fallback), which then silently persist truncated or empty work.
func (c *Client) completeViaStream(ctx context.Context, req ChatRequest) (string, error) {
	events, err := c.StreamChat(ctx, req)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	stopReason := ""
	thinkingChars := 0
	for ev := range events {
		switch ev.Type {
		case "content_block_delta":
			var cbd ContentBlockDelta
			if json.Unmarshal(ev.Payload.Bytes(), &cbd) != nil {
				continue
			}
			switch cbd.Delta.Type {
			case "text_delta":
				sb.WriteString(cbd.Delta.Text)
			case "thinking_delta":
				// Anthropic-native puts the chunk in `thinking`; translated
				// streams use `text`. Counted only for the empty-content
				// diagnostic below, never emitted.
				thinkingChars += len(cbd.Delta.Thinking) + len(cbd.Delta.Text)
			}
		case "message_delta":
			var md MessageDelta
			if json.Unmarshal(ev.Payload.Bytes(), &md) == nil && md.Delta.StopReason != "" {
				stopReason = md.Delta.StopReason
			}
		case "error":
			// Mid-stream error (overload, premature_end, provider fault):
			// whatever text accumulated so far is partial — fail the call
			// instead of returning it as a complete answer.
			return "", fmt.Errorf("stream error event: %s", truncateForLog(ev.Payload.String(), 300))
		}
	}
	out := strings.TrimSpace(sb.String())
	out = jsonutil.StripThinkingTags(out)
	out = jsonutil.StripThinkingPreamble(out)
	out = strings.TrimSpace(out)
	if out == "" && (stopReason == "max_tokens" || thinkingChars > 0) {
		// A reasoning model can burn the whole output budget in the thinking
		// channel and finish with no text (observed live on deepseek-v4-flash
		// via the OpenAI path; same trap here).
		return "", fmt.Errorf(
			"empty content (stop_reason=%s, thinking_chars=%d): reasoning consumed the output budget — raise MaxTokens or disable thinking",
			stopReason, thinkingChars,
		)
	}
	return out, nil
}

// completeOpenAI sends a non-streaming request to an OpenAI-compatible
// /chat/completions endpoint and returns the full response text.
func (c *Client) completeOpenAI(ctx context.Context, req ChatRequest) (string, error) {
	comp, err := c.postCompletion(ctx, req, 0)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(comp.content) == "" {
		if comp.refusal != "" {
			// A refusal arrives on `refusal` with content null. Returning "" with
			// a nil error would let background callers (wiki dreamer/verify/merge)
			// treat the refusal as a successful empty result.
			return "", fmt.Errorf("model refused: %s", truncateForLog(comp.refusal, 200))
		}
		// Reasoning models can burn the whole output budget on the reasoning
		// channel and finish with content null — observed live on
		// deepseek-v4-flash (server default thinking) with small max_tokens.
		// Treat as an error: "" with nil error reads as a successful empty
		// result to background callers, which silently drops their work.
		if reasoning := strings.TrimSpace(comp.reasoning); reasoning != "" || comp.finishReason == "length" {
			return "", fmt.Errorf(
				"empty content (finish_reason=%s, reasoning_chars=%d): reasoning consumed the output budget — raise MaxTokens or disable thinking",
				comp.finishReason, len(reasoning),
			)
		}
	}
	// A length-capped answer is a FAILURE, not a short success: the truncated
	// tail is exactly what structured-output callers (the skill evolver's JSON
	// rewrite) need, and returning it with a nil error made the parse step the
	// first place the damage surfaced (live 2026-07-04: content cut mid-string
	// at 6049 chars, reasoning had eaten the rest of the 12K budget). The
	// usage split rides in the error so the caller's log answers "budget or
	// reasoning?" without a reproduction round-trip.
	if comp.finishReason == "length" {
		return "", fmt.Errorf(
			"output truncated at max_tokens (completion_tokens=%d, reasoning_tokens=%d, content_chars=%d): raise MaxTokens or disable thinking",
			comp.usage.CompletionTokens, comp.usage.reasoningTokens(), len(comp.content),
		)
	}

	// Strip reasoning model artifacts (<think> tags, "Thinking Process:" preamble)
	// that leak into the content field of some local models (DeepSeek-R1, QwQ, etc.).
	content := strings.TrimSpace(comp.content)
	content = jsonutil.StripThinkingTags(content)
	content = jsonutil.StripThinkingPreamble(content)
	return strings.TrimSpace(content), nil
}

// TokenProb is one alternative a server reported for a generated token.
type TokenProb struct {
	Token   string
	Logprob float64 // natural log
}

// FirstToken is CompleteFirstToken's result.
type FirstToken struct {
	// Text is the one generated token, surrounding whitespace trimmed.
	Text string
	// Top lists the alternatives the server saw at that position, most likely
	// first. Nil when the server reports no logprobs — OpenRouter's hosted
	// models answer without them, and an Anthropic-mode client has none.
	Top   []TokenProb
	Usage TokenUsage
}

// CompleteFirstToken generates exactly one token and returns it together with
// the top-n alternatives the server weighed at that position — the
// distribution behind a one-word verdict, not only the word. MaxTokens is
// forced to 1, so stopping at the budget is the request itself rather than a
// truncation. A refusal, a token spent on reasoning, or a blank token is still
// an error, as in Complete.
func (c *Client) CompleteFirstToken(ctx context.Context, req ChatRequest, topN int) (FirstToken, error) {
	req.MaxTokens = 1
	if c.apiMode == APIModeAnthropic {
		text, err := c.completeViaStream(ctx, req)
		if err == nil && text == "" {
			err = fmt.Errorf("empty first token")
		}
		return FirstToken{Text: text}, err
	}
	if topN < 1 {
		topN = 1
	}
	comp, err := c.postCompletion(ctx, req, topN)
	if err != nil {
		return FirstToken{}, err
	}
	text := strings.TrimSpace(comp.content)
	if text == "" {
		switch {
		case comp.refusal != "":
			return FirstToken{}, fmt.Errorf("model refused: %s", truncateForLog(comp.refusal, 200))
		case strings.TrimSpace(comp.reasoning) != "":
			return FirstToken{}, fmt.Errorf("empty first token: the token went to reasoning — disable thinking for this call")
		default:
			return FirstToken{}, fmt.Errorf("empty first token (finish_reason=%s)", comp.finishReason)
		}
	}
	input, cached := comp.usage.splitPromptTokens()
	return FirstToken{
		Text: text,
		Top:  comp.firstTokenTop,
		Usage: TokenUsage{
			InputTokens:          input,
			OutputTokens:         comp.usage.CompletionTokens,
			CacheReadInputTokens: cached,
		},
	}, nil
}

// openAICompletion is the first choice of a non-streaming /chat/completions
// reply plus its usage — decoded once, judged by each caller.
type openAICompletion struct {
	content       string
	refusal       string
	reasoning     string // vLLM "reasoning" + DeepSeek/OpenRouter "reasoning_content"
	finishReason  string
	usage         openAIUsage
	firstTokenTop []TokenProb
}

// postCompletion sends req as a non-streaming /chat/completions call and
// decodes the first choice. topLogprobs > 0 also asks for every generated
// token's top alternatives and keeps the first token's; 0 leaves the request
// body exactly as it was before logprobs existed.
func (c *Client) postCompletion(ctx context.Context, req ChatRequest, topLogprobs int) (openAICompletion, error) {
	oaiReq := openAIRequest{
		Model:     req.Model,
		Stream:    false,
		MaxTokens: req.MaxTokens,
		Seed:      req.Seed,
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
		if err := json.Unmarshal(m.Content.Bytes(), &text); err == nil {
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
	c.applyReasoningParam(&oaiReq, &req)
	if topLogprobs > 0 {
		oaiReq.Logprobs = true
		oaiReq.TopLogprobs = topLogprobs
	}

	body, err := json.Marshal(oaiReq)
	if err != nil {
		return openAICompletion{}, fmt.Errorf("marshal openai request: %w", err)
	}

	// Merge ExtraBody fields (e.g., local AI's chat_template_kwargs, timeout).
	if len(req.ExtraBody) > 0 {
		body, err = mergeJSONFields(body, req.ExtraBody)
		if err != nil {
			return openAICompletion{}, fmt.Errorf("merge extra body: %w", err)
		}
	}

	url := c.baseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return openAICompletion{}, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	c.setOpenAIBearerAuth(httpReq)
	c.applyHeaders(httpReq)

	respBody, err := c.doStream(ctx, httpReq, req.Model)
	if err != nil {
		return openAICompletion{}, err
	}
	defer respBody.Close()

	// 1 MiB, not 64 KiB: a 12K-token completion plus a reasoning_content
	// channel easily exceeds 64 KiB, and a limit-truncated envelope fails
	// json.Unmarshal with a misleading "decode response" error.
	data, err := io.ReadAll(io.LimitReader(respBody, 1<<20))
	if err != nil {
		return openAICompletion{}, fmt.Errorf("read response: %w", err)
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
			Logprobs     *struct {
				Content []struct {
					TopLogprobs []struct {
						Token   string  `json:"token"`
						Logprob float64 `json:"logprob"`
					} `json:"top_logprobs"`
				} `json:"content"`
			} `json:"logprobs"`
		} `json:"choices"`
		Usage openAIUsage `json:"usage"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return openAICompletion{}, fmt.Errorf("decode response: %w", err)
	}
	if len(resp.Choices) == 0 {
		return openAICompletion{}, fmt.Errorf("no choices in response")
	}
	choice := resp.Choices[0]
	comp := openAICompletion{
		content:      choice.Message.Content,
		refusal:      choice.Message.Refusal,
		reasoning:    choice.Message.Reasoning + choice.Message.ReasoningContent,
		finishReason: choice.FinishReason,
		usage:        resp.Usage,
	}
	if lp := choice.Logprobs; lp != nil && len(lp.Content) > 0 {
		for _, alt := range lp.Content[0].TopLogprobs {
			comp.firstTokenTop = append(comp.firstTokenTop, TokenProb{Token: alt.Token, Logprob: alt.Logprob})
		}
	}
	return comp, nil
}

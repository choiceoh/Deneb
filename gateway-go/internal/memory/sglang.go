// sglang.go — Shared SGLang helpers for the memory package.
package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

// callSglang sends a streaming chat request to the local SGLang model and collects the full response.
func callSglang(ctx context.Context, client *llm.Client, model, system, user string, maxTokens int) (string, error) {
	events, err := client.StreamChatOpenAI(ctx, llm.ChatRequest{
		Model:     model,
		Messages:  []llm.Message{llm.NewTextMessage("user", user)},
		System:    llm.SystemString(system),
		MaxTokens: maxTokens,
		Stream:    true,
	})
	if err != nil {
		return "", err
	}
	if events == nil {
		return "", fmt.Errorf("sglang: nil event channel")
	}
	return collectStream(ctx, events)
}

// collectStream gathers all text deltas from an OpenAI-compatible streaming response.
func collectStream(ctx context.Context, events <-chan llm.StreamEvent) (string, error) {
	var sb strings.Builder
	for {
		select {
		case <-ctx.Done():
			if sb.Len() > 0 {
				return strings.TrimSpace(sb.String()), nil
			}
			return "", ctx.Err()
		case ev, ok := <-events:
			if !ok {
				return strings.TrimSpace(sb.String()), nil
			}
			if ev.Type == "content_block_delta" {
				var delta struct {
					Delta struct {
						Text string `json:"text"`
					} `json:"delta"`
				}
				if json.Unmarshal(ev.Payload, &delta) == nil && delta.Delta.Text != "" {
					sb.WriteString(delta.Delta.Text)
				}
			}
		}
	}
}

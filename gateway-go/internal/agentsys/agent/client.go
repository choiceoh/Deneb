// client.go — LLMStreamer interface for the agent executor.
// *llm.Client satisfies this interface; it can also be implemented by test doubles.
package agent

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// LLMStreamer abstracts the streaming LLM API used by RunAgent.
// *llm.Client implements this interface directly.
type LLMStreamer interface {
	// StreamChat calls the OpenAI-compatible streaming chat API.
	StreamChat(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamEvent, error)
	// Complete calls the OpenAI-compatible non-streaming chat API.
	Complete(ctx context.Context, req llm.ChatRequest) (string, error)
}

// executor.go — Public entrypoint and shared execution limits.
package agent

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/tokenest"
	"github.com/choiceoh/deneb/gateway-go/internal/core/agentlog"
)

// toolHeartbeatInterval is how often OnToolProgress fires while a single tool
// call is still executing. It stays below the typing-indicator TTL.
var toolHeartbeatInterval = 10 * time.Second

// deliverableNarrationMaxRunes separates brief tool-progress narration from
// answer content accumulated into AgentResult.DeliverableText.
const deliverableNarrationMaxRunes = 300

var (
	// ErrToolCallLimit is returned before any call in an over-limit model turn
	// reaches tool hooks or execution.
	ErrToolCallLimit = errors.New("agent tool-call attempt limit exceeded")
	// ErrInvalidStopShape is returned when a strict run's stop reason disagrees
	// with whether the model emitted tool calls.
	ErrInvalidStopShape = errors.New("agent invalid stop shape")
)

// RunAgent executes the shared LLM → tool-call → repeat loop. The runner keeps
// each stage's mutable accounting together while this entrypoint retains the
// stable API used by chat and autoreply.
func RunAgent(
	ctx context.Context,
	cfg AgentConfig,
	messages []llm.Message,
	client LLMStreamer,
	tools ToolExecutor,
	hooks StreamHooks,
	logger *slog.Logger,
	runLog *agentlog.RunLogger,
) (*AgentResult, error) {
	runner, err := newAgentRunner(ctx, cfg, messages, client, tools, hooks, logger, runLog)
	if err != nil {
		return nil, err
	}
	defer runner.close()
	return runner.run()
}

// generatedOutputTokenCharge counts the complete structured assistant payload,
// not only user-visible text. When provider usage is absent, the local fallback
// is floored at one token per serialized byte because bytes/4 is not a hard
// upper bound across tokenizers.
func generatedOutputTokenCharge(blocks []llm.ContentBlock, providerTokens int) int {
	if len(blocks) == 0 {
		if providerTokens > 0 {
			return providerTokens
		}
		return 0
	}
	content := llm.NewBlockMessage("assistant", blocks).Content
	estimate := tokenest.EstimateBytesUncalibrated(content)
	if providerTokens <= 0 && len(content) > estimate {
		estimate = len(content)
	}
	if providerTokens > estimate {
		return providerTokens
	}
	return estimate
}

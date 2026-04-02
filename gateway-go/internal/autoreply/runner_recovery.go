// runner_recovery.go — Agent error recovery chain for DefaultAgentRunner.
//
// runAgentWithRecovery implements the 6-step recovery strategy:
//  1. Transient HTTP retry  (502/503/521/429 → wait 2.5s, retry once)
//  2. Context overflow      → error payload + onSessionReset callback
//  3. Billing error         → error payload
//  4. Role ordering         → error payload + onSessionReset callback
//  5. Model fallback        → retry with each fallback model in order
//  6. Final unrecoverable   → classified Korean error message
package autoreply

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/model"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/pipeline"
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

// runAgentWithRecovery executes agent.RunAgent and applies the full error recovery
// chain. Returns (agentResult, done):
//   - done=true:  a terminal error payload was appended to result; caller should return.
//   - done=false: agentResult is the successful outcome (or nil for a clean abort).
func (r *DefaultAgentRunner) runAgentWithRecovery(
	ctx context.Context,
	agentCfg agent.AgentConfig,
	messages []llm.Message,
	client agent.LLMStreamer,
	toolExec agent.ToolExecutor,
	cfg *AgentTurnConfig,
	result *AgentTurnResult,
	startedAt time.Time,
	logger *slog.Logger,
) (*agent.AgentResult, bool) {
	agentResult, runErr := agent.RunAgent(ctx, agentCfg, messages, client, toolExec, agent.StreamHooks{}, logger, nil)

	if runErr == nil {
		return agentResult, false
	}

	errMsg := runErr.Error()

	// 1. Transient HTTP retry (502/503/521/429 → wait 2.5s, retry once).
	if IsTransientHTTPError(errMsg) {
		logger.Warn("transient HTTP error, retrying", "error", errMsg, "session", cfg.SessionKey)
		select {
		case <-ctx.Done():
			result.WasAborted = true
			result.DurationMs = time.Since(startedAt).Milliseconds()
			return nil, true
		case <-time.After(TransientRetryDelayMs * time.Millisecond):
		}
		agentResult, runErr = agent.RunAgent(ctx, agentCfg, messages, client, toolExec, agent.StreamHooks{}, logger, nil)
	}

	// 2. Context overflow → auto-recovery.
	if runErr != nil && IsContextOverflowError(runErr.Error()) {
		if r.onSessionReset != nil {
			r.onSessionReset(cfg.SessionKey, "context_overflow")
		}
		result.Payloads = append(result.Payloads, types.ReplyPayload{Text: ContextOverflowMessage, IsError: true})
		result.DurationMs = time.Since(startedAt).Milliseconds()
		return nil, true
	}

	// 3. Billing error.
	if runErr != nil && IsBillingError(runErr.Error()) {
		result.Payloads = append(result.Payloads, types.ReplyPayload{Text: BillingErrorMessage, IsError: true})
		result.DurationMs = time.Since(startedAt).Milliseconds()
		return nil, true
	}

	// 4. Role ordering → session reset.
	if runErr != nil && IsRoleOrderingError(runErr.Error()) {
		if r.onSessionReset != nil {
			r.onSessionReset(cfg.SessionKey, "role_ordering")
		}
		result.Payloads = append(result.Payloads, types.ReplyPayload{Text: RoleOrderingMessage, IsError: true})
		result.DurationMs = time.Since(startedAt).Milliseconds()
		return nil, true
	}

	// 5. Try fallback models if available.
	if runErr != nil && len(cfg.FallbackModels) > 0 {
		for i, fallback := range cfg.FallbackModels {
			logger.Info("trying fallback model", "model", fallback, "attempt", i+1, "session", cfg.SessionKey)
			parts := pipeline.SplitProviderModel(fallback)
			if parts[0] != "" {
				cfg.Provider = parts[0]
			}
			cfg.Model = parts[1]
			agentCfg.Model = cfg.Model
			result.FallbackActive = true
			result.FallbackAttempts = append(result.FallbackAttempts, model.FallbackAttempt{
				Provider: cfg.Provider,
				Model:    cfg.Model,
				Error:    runErr.Error(),
			})
			agentResult, runErr = agent.RunAgent(ctx, agentCfg, messages, client, toolExec, agent.StreamHooks{}, logger, nil)
			if runErr == nil {
				result.ModelUsed = cfg.Model
				result.ProviderUsed = cfg.Provider
				return agentResult, false
			}
		}
	}

	// 6. Final error — no recovery possible.
	if runErr != nil {
		errText := runErr.Error()
		// Replace raw HTTP error strings with specific Korean messages for the user,
		// but preserve the original error in result.Error for debugging/logging.
		userText := errText
		if specific := ClassifyErrorMessage(errText); specific != "" {
			userText = specific
		}
		result.Error = runErr
		result.Payloads = append(result.Payloads, types.ReplyPayload{
			Text:    fmt.Sprintf("⚠️ Agent failed: %s", strings.TrimRight(userText, ".")),
			IsError: true,
		})
		logger.Error("agent failed (unrecoverable)",
			"error", errText,
			"session", cfg.SessionKey,
			"model", cfg.Model,
			"provider", cfg.Provider,
		)
		result.DurationMs = time.Since(startedAt).Milliseconds()
		return nil, true
	}

	return agentResult, false
}

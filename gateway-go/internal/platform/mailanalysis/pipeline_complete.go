package mailanalysis

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// synthesisFallbackBudget is the fresh budget for a tool-less stage-2 retry
// after the parent deadline is already spent (agent synthesis hung on a dead
// primary). Matches chat's stallFallbackBudget so a wedged turn cannot run
// unbounded, but a healthy fallback still has room to answer.
const synthesisFallbackBudget = 90 * time.Second

// SynthesisEndpoint is one stage-2 model the tool-less completion may try.
// Production walks RoleMain's FallbackChain (main → main2 → coding → …).
type SynthesisEndpoint struct {
	Client        *llm.Client
	Model         string
	ThinkingKwarg string
}

func resolveSynthesisEndpoints(deps PipelineDeps) []SynthesisEndpoint {
	if deps.SynthesisEndpointsFn != nil {
		if eps := deps.SynthesisEndpointsFn(); len(eps) > 0 {
			return eps
		}
	}
	if len(deps.SynthesisEndpoints) > 0 {
		return deps.SynthesisEndpoints
	}
	if deps.LLMClient != nil && strings.TrimSpace(deps.MainModel) != "" {
		return []SynthesisEndpoint{{
			Client:        deps.LLMClient,
			Model:         deps.MainModel,
			ThinkingKwarg: deps.ThinkingKwarg,
		}}
	}
	return nil
}

func hasStage2LLM(deps PipelineDeps) bool {
	return deps.AgentSynthesisFn != nil || len(resolveSynthesisEndpoints(deps)) > 0
}

func usableSynthesisCount(eps []SynthesisEndpoint) int {
	n := 0
	for _, ep := range eps {
		if ep.Client != nil && strings.TrimSpace(ep.Model) != "" {
			n++
		}
	}
	return n
}

func skipPrimaryAfterAgent(ctx context.Context, err error, out string) bool {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	if err == nil {
		return strings.TrimSpace(out) == ""
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "incomplete") || strings.Contains(msg, "timeout")
}

func recordSynthesisFailure(onFailure func(string), model string, callErr error) {
	if onFailure == nil || strings.TrimSpace(model) == "" {
		return
	}
	if callErr != nil && errors.Is(callErr, context.Canceled) {
		return
	}
	onFailure(model)
}

// completeSynthesis runs a tool-less stage-2 completion, walking the configured
// endpoint chain. A spent parent deadline is recovered with a bounded budget
// so a dead primary cannot cancel the fallback the way StreamChat-on-Main did.
func completeSynthesis(ctx context.Context, deps PipelineDeps, system, user string, maxTok int) (string, error) {
	return completeSynthesisChain(ctx, deps, system, user, maxTok, false)
}

func completeSynthesisChain(ctx context.Context, deps PipelineDeps, system, user string, maxTok int, skipPrimary bool) (string, error) {
	eps := resolveSynthesisEndpoints(deps)
	if usableSynthesisCount(eps) == 0 {
		return "", fmt.Errorf("analysis LLM client is required")
	}
	return streamSynthesis(ctx, eps, system, user, maxTok, deps.DeepThinking, deps.MainModel, deps.Logger, skipPrimary, deps.RecordModelFailure)
}

func streamSynthesis(ctx context.Context, eps []SynthesisEndpoint, system, user string, maxTok int, deepThinking bool, primaryModel string, logger *slog.Logger, skipPrimary bool, onFailure func(string)) (string, error) {
	if usableSynthesisCount(eps) == 0 {
		return "", fmt.Errorf("analysis LLM client is required")
	}
	if logger == nil {
		logger = slog.Default()
	}

	recovered := false
	if err := ctx.Err(); err != nil {
		if !errors.Is(err, context.DeadlineExceeded) {
			return "", err
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.WithoutCancel(ctx), synthesisFallbackBudget)
		defer cancel()
		recovered = true
		logger.Warn("mail analysis: stage-2 deadline spent; retrying tool-less completion on the fallback chain")
	}

	skipPrimary = skipPrimary || recovered
	canSkipPrimary := skipPrimary && strings.TrimSpace(primaryModel) != "" && usableSynthesisCount(eps) > 1

	var lastErr error
	tried := 0
	for _, ep := range eps {
		if ep.Client == nil || strings.TrimSpace(ep.Model) == "" {
			continue
		}
		if canSkipPrimary && ep.Model == primaryModel {
			logger.Warn("mail analysis: skipping primary model after stage-2 failure", "model", ep.Model)
			recordSynthesisFailure(onFailure, ep.Model, fmt.Errorf("primary skipped after stage-2 failure"))
			continue
		}
		if ctx.Err() != nil {
			lastErr = ctx.Err()
			break
		}
		tried++
		text, err := streamOneSynthesis(ctx, ep, system, user, maxTok, deepThinking)
		if err == nil && strings.TrimSpace(text) != "" {
			if tried > 1 || recovered || skipPrimary {
				logger.Warn("mail analysis: stage-2 fell back to another model", "model", ep.Model)
			}
			return text, nil
		}
		if err != nil {
			lastErr = err
			if strings.Contains(err.Error(), "empty LLM response") {
				lastErr = fmt.Errorf("LLM 응답이 비어있습니다")
			}
		} else {
			lastErr = fmt.Errorf("LLM 응답이 비어있습니다")
		}
		recordSynthesisFailure(onFailure, ep.Model, lastErr)
		logger.Warn("mail analysis: stage-2 model failed, trying next", "model", ep.Model, "error", lastErr)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("analysis LLM client is required")
	}
	return "", lastErr
}

func streamOneSynthesis(ctx context.Context, ep SynthesisEndpoint, system, user string, maxTok int, deepThinking bool) (string, error) {
	thinking := &llm.ThinkingConfig{Type: "disabled", TemplateKwarg: ep.ThinkingKwarg}
	if deepThinking {
		thinking = analysisThinking(ep.Client, maxTok)
		if thinking != nil && thinking.Type == "disabled" && ep.ThinkingKwarg != "" {
			thinking.TemplateKwarg = ep.ThinkingKwarg
		}
	}
	events, err := ep.Client.StreamChat(ctx, llm.ChatRequest{
		Model:     ep.Model,
		Messages:  []llm.Message{llm.NewTextMessage("user", user)},
		System:    llm.SystemString(system),
		MaxTokens: maxTok,
		Stream:    true,
		Thinking:  thinking,
	})
	if err != nil {
		return "", fmt.Errorf("final analysis LLM call failed: %w", err)
	}
	return collectStreamText(ctx, events)
}

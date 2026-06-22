// model_panel.go — research-panel fan-out engine behind the research_panel tool
// (deep-research skill). One prompt → every healthy model the wormhole router
// serves, in parallel → each model's raw answer back. Synthesis is NOT done here:
// the skill drives the main model as the aggregator (Mixture-of-Agents 2406.04692
// / OpenRouter Fusion shape), weighting answers by capability + cross-family
// agreement, because weak proposers drag a naive aggregate (Self-MoA 2502.00674).
package server

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
)

const (
	// panelMaxConcurrency bounds in-flight model calls. The local main model
	// shares the GPU (vLLM queues the rest), cloud members are parallel-friendly;
	// 6 keeps wall-clock low without flooding wormhole. (Not an OOM guard — vLLM
	// pre-budgets its KV pool; this is just politeness to the router.)
	panelMaxConcurrency = 6
	// panelPerModelTimeout drops a slow/stuck model so it never gates the batch.
	// Well under the 5-min turn deadline so the synthesizer still gets to run.
	panelPerModelTimeout = 90 * time.Second
	// panelMaxModels caps the panel — MoA's gains flatten past ~6-8 proposers and
	// the synthesizer's context fills with redundant text beyond that.
	panelMaxModels = 8
	// panelMaxTokens bounds each proposer's answer; the panel wants a substantive
	// take, not a full report (the synthesizer writes the final answer).
	panelMaxTokens = 2048
)

// consultModelPanel fans `prompt` (with shared `system`) out to `models` — or,
// when models is empty, to every currently-healthy model the wormhole router
// serves — in parallel, and returns each model's answer. Failures/timeouts are
// recorded per-answer (Err set, Answer empty) and never block the batch.
//
// Wired into CoreToolDeps.ConsultPanel; reached by the research_panel tool.
func (s *Server) consultModelPanel(ctx context.Context, system, prompt string, models []string) []toolctx.PanelAnswer {
	reg := s.modelRegistry
	if reg == nil {
		return nil
	}
	// The lightweight role fronts the wormhole router (an OpenAI-compatible
	// endpoint that multiplexes every served model by name), so this one client
	// can call any model the router serves just by changing ChatRequest.Model.
	client := reg.Client(modelrole.RoleLightweight)
	if client == nil {
		return nil
	}
	if len(models) == 0 {
		models = s.panelHealthyModels(ctx)
	}
	if len(models) > panelMaxModels {
		models = models[:panelMaxModels]
	}
	if len(models) == 0 {
		return nil
	}

	answers := make([]toolctx.PanelAnswer, len(models))
	sem := make(chan struct{}, panelMaxConcurrency)
	var wg sync.WaitGroup
	for i, m := range models {
		wg.Add(1)
		go func(idx int, model string) {
			defer func() {
				if r := recover(); r != nil && s.logger != nil {
					s.logger.Error("panic in model panel call", "model", model, "panic", r)
				}
				wg.Done()
			}()
			sem <- struct{}{}
			defer func() { <-sem }()

			cctx, cancel := context.WithTimeout(ctx, panelPerModelTimeout)
			defer cancel()
			start := time.Now()
			txt, err := client.Complete(cctx, llm.ChatRequest{
				Model:     model,
				System:    llm.SystemString(system),
				Messages:  []llm.Message{llm.NewTextMessage("user", prompt)},
				MaxTokens: panelMaxTokens,
			})
			ans := toolctx.PanelAnswer{Model: model, Family: panelModelFamily(model), Ms: time.Since(start).Milliseconds()}
			switch {
			case err != nil:
				ans.Err = err.Error()
			case strings.TrimSpace(txt) == "":
				ans.Err = "empty answer"
			default:
				ans.Answer = strings.TrimSpace(txt)
			}
			answers[idx] = ans
		}(i, m)
	}
	wg.Wait()
	return answers
}

// panelHealthyModels lists the models the wormhole router currently serves,
// minus those the role-health circuit breaker has tripped — the same health
// signal the native model picker shows as its 200/red dots.
func (s *Server) panelHealthyModels(ctx context.Context) []string {
	reg := s.modelRegistry
	if reg == nil {
		return nil
	}
	baseURL := strings.TrimSpace(reg.BaseURL(modelrole.RoleLightweight))
	if baseURL == "" {
		return nil
	}
	served, err := modelrole.DiscoverServedVllmModels(ctx, baseURL, reg.APIKey(modelrole.RoleLightweight))
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("model panel: served-model discovery failed", "error", err)
		}
		return nil
	}
	out := make([]string, 0, len(served))
	for _, m := range served {
		if reg.ModelUnhealthy(m) {
			continue
		}
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

// panelModelFamily collapses a served model name to a coarse provider/family so
// the synthesizer can weight agreement ACROSS families more than within one
// (same-family models share blind spots — the echo-chamber failure mode).
func panelModelFamily(model string) string {
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "deepseek"), strings.Contains(m, "dsv"):
		return "deepseek"
	case strings.Contains(m, "qwen"):
		return "qwen"
	case strings.Contains(m, "glm"):
		return "glm"
	case strings.Contains(m, "mimo"):
		return "mimo"
	case strings.Contains(m, "kimi"):
		return "kimi"
	case strings.Contains(m, "gemma"):
		return "gemma"
	case strings.Contains(m, "gpt"), strings.Contains(m, "openai"):
		return "openai"
	case strings.Contains(m, "claude"), strings.Contains(m, "anthropic"):
		return "anthropic"
	case strings.Contains(m, "gemini"):
		return "gemini"
	default:
		return m
	}
}

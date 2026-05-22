// Package server — model quick-change UI for the Telegram inline keyboard.
//
// The /models command renders a provider-grouped inline keyboard so the
// operator can switch the default model with one tap. Sections cover:
//
//   - 역할      role-based models from the registry (main/lightweight/fallback)
//   - Z.ai      curated cloud GLM models
//   - 로컬 vLLM  models auto-discovered from the local vLLM server
//   - 로컬 AI    models auto-discovered from the local AI server
//   - OpenRouter curated cloud models (shown only when an API key is set)
//
// Local sections reflect what each server actually serves (auto-discovered),
// so the keyboard never offers a model that would 404. Discovery results are
// cached briefly so re-rendering the keyboard on every button tap stays fast.
package server

import (
	"context"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/handlers"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
)

const (
	// maxModelsPerProvider caps how many discovered models one provider
	// section may show, keeping the keyboard scannable.
	maxModelsPerProvider = 6
	// localDiscoveryTimeout bounds a single local /models probe.
	localDiscoveryTimeout = 3 * time.Second
	// localModelCacheTTL keeps discovered local models warm so re-rendering
	// the keyboard (every model-switch tap rebuilds it) does not re-probe.
	localModelCacheTTL = 5 * time.Minute
)

// zaiQuickModels is the curated Z.ai model set offered in /models.
var zaiQuickModels = []string{"glm-5-turbo", "glm-5.1"}

// openrouterQuickModels is the curated OpenRouter model set offered in /models.
// Shown only when OPENROUTER_API_KEY is configured.
var openrouterQuickModels = []string{
	"anthropic/claude-opus-4.7",
	"anthropic/claude-sonnet-4.6",
	"google/gemini-3.1-pro",
}

// modelEntry describes a single model button in the /models keyboard.
type modelEntry struct {
	provider string // provider ID (zai, vllm, localai, openrouter)
	label    string // button label
	fullID   string // full model ID sent to the LLM + callback (provider/model)
	display  string // short display name (no provider prefix)
}

// modelSection is a titled group of model buttons in the /models keyboard.
type modelSection struct {
	title   string
	entries []modelEntry
}

// localModelCache memoizes auto-discovered local provider models so the
// keyboard re-renders instantly after the first probe.
var localModelCache struct {
	mu      sync.Mutex
	models  map[string][]string
	builtAt time.Time
}

// shortModelName strips the provider prefix from a model name.
func shortModelName(model string) string {
	if idx := strings.LastIndex(model, "/"); idx >= 0 && idx < len(model)-1 {
		return model[idx+1:]
	}
	return model
}

// callbackFits reports whether a model-switch callback for fullID stays within
// Telegram's 64-byte callback_data limit. Oversized IDs are dropped rather than
// truncated (a truncated model ID would silently switch to the wrong model).
func callbackFits(fullID string) bool {
	return len(telegram.ActionModelSwitch)+1+len(fullID) <= telegram.MaxCallbackData
}

// curatedEntries builds model entries for a fixed provider/model list.
func curatedEntries(provider string, models []string) []modelEntry {
	entries := make([]modelEntry, 0, len(models))
	for _, m := range models {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		short := shortModelName(m)
		entries = append(entries, modelEntry{
			provider: provider,
			label:    short,
			fullID:   provider + "/" + m,
			display:  short,
		})
	}
	return entries
}

// registryRoleEntries builds the role-based section (main/lightweight/fallback).
func registryRoleEntries(reg *modelrole.Registry) []modelEntry {
	if reg == nil {
		return nil
	}
	roles := []struct {
		role  modelrole.Role
		label string
	}{
		{modelrole.RoleMain, "main"},
		{modelrole.RoleLightweight, "lightweight"},
		{modelrole.RoleFallback, "fallback"},
	}
	var entries []modelEntry
	for _, r := range roles {
		cfg := reg.Config(r.role)
		if cfg.Model == "" {
			continue
		}
		entries = append(entries, modelEntry{
			provider: cfg.ProviderID,
			label:    r.label + ": " + shortModelName(cfg.Model),
			fullID:   reg.FullModelID(r.role),
			display:  shortModelName(cfg.Model),
		})
	}
	return entries
}

// assembleModelSections builds the ordered /models keyboard sections. Pure —
// all IO (local discovery, env lookup) is resolved by the caller and passed in.
// Entries are de-duplicated globally by full model ID (first occurrence wins),
// so a model surfaced as a role does not repeat in its provider section.
func assembleModelSections(
	roles []modelEntry,
	discovered map[string][]string,
	openrouterEnabled bool,
) []modelSection {
	seen := make(map[string]struct{})
	var sections []modelSection

	add := func(title string, entries []modelEntry) {
		var kept []modelEntry
		for _, e := range entries {
			if e.fullID == "" || !callbackFits(e.fullID) {
				continue
			}
			if _, dup := seen[e.fullID]; dup {
				continue
			}
			seen[e.fullID] = struct{}{}
			kept = append(kept, e)
		}
		if len(kept) > 0 {
			sections = append(sections, modelSection{title: title, entries: kept})
		}
	}

	add("역할", roles)
	add("Z.ai", curatedEntries("zai", zaiQuickModels))
	add("로컬 vLLM", curatedEntries("vllm", discovered["vllm"]))
	add("로컬 AI", curatedEntries("localai", discovered["localai"]))
	if openrouterEnabled {
		add("OpenRouter", curatedEntries("openrouter", openrouterQuickModels))
	}
	return sections
}

// discoverProviderModels probes an OpenAI-compatible /models endpoint and
// returns up to maxModelsPerProvider served model ids. Returns nil on any
// failure so the provider section is simply omitted from the keyboard.
func discoverProviderModels(ctx context.Context, baseURL string) []string {
	if strings.TrimSpace(baseURL) == "" {
		return nil
	}
	ids, err := modelrole.DiscoverServedVllmModels(ctx, baseURL)
	if err != nil || len(ids) == 0 {
		return nil
	}
	if len(ids) > maxModelsPerProvider {
		ids = ids[:maxModelsPerProvider]
	}
	return ids
}

// discoverLocalModels probes the local vLLM and AI servers concurrently and
// returns provider→served-models. Results are cached for localModelCacheTTL.
func (p *InboundProcessor) discoverLocalModels() map[string][]string {
	localModelCache.mu.Lock()
	if localModelCache.models != nil && time.Since(localModelCache.builtAt) < localModelCacheTTL {
		cached := localModelCache.models
		localModelCache.mu.Unlock()
		return cached
	}
	localModelCache.mu.Unlock()

	vllmURL := modelrole.DefaultVllmBaseURL
	if reg := p.server.modelRegistry; reg != nil {
		if u := reg.BaseURL(modelrole.RoleLightweight); u != "" {
			vllmURL = u
		}
	}
	targets := []struct {
		provider string
		baseURL  string
	}{
		{"vllm", vllmURL},
		{"localai", modelrole.DefaultLocalAIBaseURL},
	}

	results := make([][]string, len(targets))
	var wg sync.WaitGroup
	for i, t := range targets {
		wg.Add(1)
		go func(idx int, provider, baseURL string) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					p.logger.Error("panic in model discovery probe", "provider", provider, "panic", r)
				}
			}()
			ctx, cancel := context.WithTimeout(p.server.ShutdownCtx(), localDiscoveryTimeout)
			defer cancel()
			results[idx] = discoverProviderModels(ctx, baseURL)
		}(i, t.provider, t.baseURL)
	}
	wg.Wait()

	out := make(map[string][]string, len(targets))
	for i, t := range targets {
		if len(results[i]) > 0 {
			out[t.provider] = results[i]
		}
	}

	localModelCache.mu.Lock()
	localModelCache.models = out
	localModelCache.builtAt = time.Now()
	localModelCache.mu.Unlock()
	return out
}

// quickChangeModels returns the ordered sections for the /models keyboard.
func (p *InboundProcessor) quickChangeModels() []modelSection {
	roles := registryRoleEntries(p.server.modelRegistry)
	discovered := p.discoverLocalModels()
	openrouter := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY")) != ""
	return assembleModelSections(roles, discovered, openrouter)
}

// currentModel resolves the model ID currently in effect for new runs.
func (p *InboundProcessor) currentModel() string {
	m := p.chatHandler.DefaultModel()
	if m == "" && p.server.modelRegistry != nil {
		m = p.server.modelRegistry.FullModelID(modelrole.RoleMain)
	}
	return m
}

// buildModelKeyboard builds a provider-grouped inline keyboard. Each section
// gets a non-actionable header row followed by 2-column model button rows.
func buildModelKeyboard(sections []modelSection, currentModel string) *telegram.InlineKeyboardMarkup {
	var rows [][]telegram.InlineKeyboardButton
	for _, sec := range sections {
		if len(sec.entries) == 0 {
			continue
		}
		rows = append(rows, []telegram.InlineKeyboardButton{{
			Text:         "— " + sec.title + " —",
			CallbackData: telegram.ActionNoop + ":",
		}})
		var row []telegram.InlineKeyboardButton
		for i, e := range sec.entries {
			label := e.label
			if e.fullID == currentModel {
				label = "✓ " + label
			}
			row = append(row, telegram.InlineKeyboardButton{
				Text:         label,
				CallbackData: telegram.ActionModelSwitch + ":" + e.fullID,
			})
			if len(row) == 2 || i == len(sec.entries)-1 {
				rows = append(rows, row)
				row = nil
			}
		}
	}
	if len(rows) == 0 {
		return nil
	}
	return telegram.BuildInlineKeyboard(rows)
}

// modelsMessageText renders the header text for the /models message.
func modelsMessageText(currentModel string) string {
	if currentModel == "" {
		return "🤖 <b>모델 퀵체인지</b>\n\n현재: <code>(미설정)</code>"
	}
	return "🤖 <b>모델 퀵체인지</b>\n\n현재: <code>" + currentModel + "</code>"
}

// handleModelsCommand sends a model quick-change message with an inline keyboard.
func (p *InboundProcessor) handleModelsCommand(chatID string) {
	sections := p.quickChangeModels()
	if len(sections) == 0 {
		p.sendCommandReply(chatID, &handlers.CommandResult{Reply: "사용 가능한 모델이 없습니다.", SkipAgent: true})
		return
	}

	client := p.server.telegramPlug.Client()
	if client == nil {
		return
	}

	id, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil {
		return
	}

	currentModel := p.currentModel()
	keyboard := buildModelKeyboard(sections, currentModel)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := telegram.SendText(ctx, client, id, modelsMessageText(currentModel), telegram.SendOptions{
		ParseMode: "HTML",
		Keyboard:  keyboard,
	}); err != nil {
		p.logger.Warn("failed to send models command reply", "error", err)
	}
}

// handleModelSwitchCallback processes a model quick-change button press.
func (p *InboundProcessor) handleModelSwitchCallback(cb *telegram.CallbackQuery, chatID, fullModelID string) {
	client := p.server.telegramPlug.Client()
	if client == nil {
		return
	}

	// Apply model change.
	p.chatHandler.SetDefaultModel(fullModelID)

	// Persist to deneb.json so the choice survives restarts.
	safego.GoWithSlog(p.logger, "persist-model-choice", func() {
		cfgPath := config.ResolveConfigPath()
		if err := config.PersistDefaultModel(cfgPath, fullModelID, p.logger); err != nil {
			p.logger.Warn("failed to persist model choice", "model", fullModelID, "error", err)
		}
	})

	// Acknowledge with a toast.
	ackCtx, ackCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer ackCancel()
	if err := telegram.AnswerCallbackQuery(ackCtx, client, cb.ID, "✓ "+shortModelName(fullModelID)); err != nil {
		p.logger.Warn("failed to answer model switch callback", "error", err)
	}

	// Edit the original message to move the checkmark to the new model.
	id, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil {
		return
	}

	sections := p.quickChangeModels()
	keyboard := buildModelKeyboard(sections, fullModelID)

	editCtx, editCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer editCancel()
	if _, err := telegram.EditMessageText(editCtx, client, id, cb.Message.MessageID, modelsMessageText(fullModelID), "HTML", keyboard); err != nil {
		p.logger.Warn("failed to edit model switch message", "error", err)
	}
}

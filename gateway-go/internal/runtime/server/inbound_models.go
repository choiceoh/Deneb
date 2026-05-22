// Package server — model quick-change UI for the Telegram inline keyboard.
//
// The /models command renders a provider-grouped inline keyboard so the
// operator can switch the default model with one tap. The provider list is
// driven by deneb.json's models.providers map — every provider the operator
// has configured is "connected" and gets its own section. Each section lists
// the models declared in config, augmented (for providers on a loopback host)
// by whatever the local server actually serves via its /models endpoint.
//
// A leading 역할 section surfaces the registry's role-based models
// (main/lightweight/fallback) as semantic shortcuts. Local discovery results
// are cached briefly so re-rendering the keyboard on every button tap is fast.
package server

import (
	"context"
	"encoding/json"
	"net/url"
	"sort"
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
	// maxModelsPerProvider caps how many models one provider section may
	// show, keeping the keyboard scannable. Config-declared models come
	// first (operator priority), then auto-discovered extras.
	maxModelsPerProvider = 6
	// localDiscoveryTimeout bounds a single local /models probe.
	localDiscoveryTimeout = 3 * time.Second
	// localModelCacheTTL keeps discovered local models warm so re-rendering
	// the keyboard (every model-switch tap rebuilds it) does not re-probe.
	localModelCacheTTL = 5 * time.Minute
)

// modelEntry describes a single model button in the /models keyboard.
type modelEntry struct {
	provider string // provider ID (zai, vllm, openrouter, ...)
	label    string // button label
	fullID   string // full model ID sent to the LLM + callback (provider/model)
	display  string // short display name (no provider prefix)
}

// modelSection is a titled group of model buttons in the /models keyboard.
type modelSection struct {
	title   string
	entries []modelEntry
}

// providerSpec is one provider configured in deneb.json's models.providers.
type providerSpec struct {
	name    string   // provider key (zai, vllm, openrouter, ...)
	baseURL string   // OpenAI-compatible endpoint, may be empty
	models  []string // model ids declared in config (+ discovered, after merge)
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

// providerDisplayName returns a human-friendly label for a provider key.
func providerDisplayName(name string) string {
	switch name {
	case "zai":
		return "Z.ai"
	case "vllm":
		return "vLLM"
	case "localai":
		return "LocalAI"
	case "openrouter":
		return "OpenRouter"
	case "anthropic":
		return "Anthropic"
	case "openai":
		return "OpenAI"
	case "google":
		return "Google"
	case "kimi":
		return "Kimi Code"
	case "mimo":
		return "MiMo"
	case "mimo-plan":
		return "MiMo Token Plan"
	}
	return name
}

// builtinSubscriptionProviders lists the coding-subscription providers that
// work without a deneb.json models.providers entry — base URL and
// credentials resolve from built-in defaults — so they are always offered
// in the /models keyboard.
func builtinSubscriptionProviders() []providerSpec {
	return []providerSpec{
		{name: "kimi", models: []string{"kimi-for-coding"}},
		{name: "mimo-plan", models: []string{"mimo-v2.5-pro"}},
	}
}

// appendBuiltinSubscriptionProviders adds the built-in coding-subscription
// providers, skipping any the operator already declared in deneb.json so
// their explicit config wins.
func appendBuiltinSubscriptionProviders(configured []providerSpec) []providerSpec {
	have := make(map[string]struct{}, len(configured))
	for _, pv := range configured {
		have[pv.name] = struct{}{}
	}
	for _, b := range builtinSubscriptionProviders() {
		if _, ok := have[b.name]; !ok {
			configured = append(configured, b)
		}
	}
	return configured
}

// isLocalURL reports whether a base URL points at a loopback host, i.e. a
// local model server that is worth probing for auto-discovery.
func isLocalURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := u.Hostname()
	switch host {
	case "localhost", "127.0.0.1", "0.0.0.0", "::1":
		return true
	}
	return strings.HasPrefix(host, "127.")
}

// effectiveBaseURL returns the provider's base URL, falling back to the known
// default for local providers when config omits it.
func effectiveBaseURL(spec providerSpec) string {
	if spec.baseURL != "" {
		return spec.baseURL
	}
	switch spec.name {
	case "vllm":
		return modelrole.DefaultVllmBaseURL
	case "localai":
		return modelrole.DefaultLocalAIBaseURL
	}
	return ""
}

// loadConfiguredProviders reads models.providers from deneb.json. Every
// configured provider is treated as "connected" and surfaced in /models.
// Providers are returned sorted by name for a stable keyboard layout.
func loadConfiguredProviders() []providerSpec {
	snapshot, err := config.LoadConfigFromDefaultPath()
	if err != nil || snapshot == nil || !snapshot.Valid || snapshot.Raw == "" {
		return nil
	}
	var root struct {
		Models struct {
			Providers map[string]struct {
				BaseURL string `json:"baseUrl"`
				Models  []struct {
					ID string `json:"id"`
				} `json:"models"`
			} `json:"providers"`
		} `json:"models"`
	}
	if err := json.Unmarshal([]byte(snapshot.Raw), &root); err != nil {
		return nil
	}
	specs := make([]providerSpec, 0, len(root.Models.Providers))
	for name, pc := range root.Models.Providers {
		spec := providerSpec{name: name, baseURL: strings.TrimSpace(pc.BaseURL)}
		for _, m := range pc.Models {
			if id := strings.TrimSpace(m.ID); id != "" {
				spec.models = append(spec.models, id)
			}
		}
		specs = append(specs, spec)
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].name < specs[j].name })
	return specs
}

// mergeModels concatenates configured + discovered model ids, de-duplicating
// while preserving order (config-declared models first).
func mergeModels(configured, discovered []string) []string {
	seen := make(map[string]struct{}, len(configured)+len(discovered))
	var out []string
	for _, group := range [][]string{configured, discovered} {
		for _, m := range group {
			m = strings.TrimSpace(m)
			if m == "" {
				continue
			}
			if _, dup := seen[m]; dup {
				continue
			}
			seen[m] = struct{}{}
			out = append(out, m)
		}
	}
	return out
}

// providerEntries builds the model buttons for one provider section.
func providerEntries(spec providerSpec) []modelEntry {
	entries := make([]modelEntry, 0, len(spec.models))
	for _, m := range spec.models {
		short := shortModelName(m)
		entries = append(entries, modelEntry{
			provider: spec.name,
			label:    short,
			fullID:   spec.name + "/" + m,
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

// assembleModelSections builds the ordered /models keyboard sections: a 역할
// section followed by one section per configured provider. Pure — all IO
// (config load, local discovery) is resolved by the caller. Entries are
// de-duplicated globally by full model ID (first occurrence wins), so a model
// surfaced as a role does not repeat in its provider section.
func assembleModelSections(roles []modelEntry, providers []providerSpec) []modelSection {
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
	for _, pv := range providers {
		add(providerDisplayName(pv.name), providerEntries(pv))
	}
	return sections
}

// discoverProviderModels probes an OpenAI-compatible /models endpoint and
// returns up to maxModelsPerProvider served model ids. Returns nil on any
// failure so the provider section falls back to config-declared models only.
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

// discoverLocalModels probes the /models endpoint of every configured provider
// on a loopback host, concurrently, and returns provider→served-models.
// Results are cached for localModelCacheTTL.
func (p *InboundProcessor) discoverLocalModels(providers []providerSpec) map[string][]string {
	localModelCache.mu.Lock()
	if localModelCache.models != nil && time.Since(localModelCache.builtAt) < localModelCacheTTL {
		cached := localModelCache.models
		localModelCache.mu.Unlock()
		return cached
	}
	localModelCache.mu.Unlock()

	type target struct {
		name    string
		baseURL string
	}
	var targets []target
	for _, pv := range providers {
		if base := effectiveBaseURL(pv); isLocalURL(base) {
			targets = append(targets, target{name: pv.name, baseURL: base})
		}
	}

	out := make(map[string][]string)
	if len(targets) > 0 {
		results := make([][]string, len(targets))
		var wg sync.WaitGroup
		for i, t := range targets {
			wg.Add(1)
			go func(idx int, name, baseURL string) {
				defer wg.Done()
				defer func() {
					if r := recover(); r != nil {
						p.logger.Error("panic in model discovery probe", "provider", name, "panic", r)
					}
				}()
				ctx, cancel := context.WithTimeout(p.server.ShutdownCtx(), localDiscoveryTimeout)
				defer cancel()
				results[idx] = discoverProviderModels(ctx, baseURL)
			}(i, t.name, t.baseURL)
		}
		wg.Wait()
		for i, t := range targets {
			if len(results[i]) > 0 {
				out[t.name] = results[i]
			}
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
	providers := appendBuiltinSubscriptionProviders(loadConfiguredProviders())
	discovered := p.discoverLocalModels(providers)
	for i := range providers {
		providers[i].models = mergeModels(providers[i].models, discovered[providers[i].name])
		if len(providers[i].models) > maxModelsPerProvider {
			providers[i].models = providers[i].models[:maxModelsPerProvider]
		}
	}
	return assembleModelSections(roles, providers)
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

package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// DefaultTurnDeadline is the end-to-end budget for processing one user turn.
const DefaultTurnDeadline = 5 * time.Minute

// maxModelsPerProvider caps the models shown per provider in the model picker
// so the list stays navigable on a phone screen.
const maxModelsPerProvider = 8

// localDiscoveryTimeout bounds a single local /models probe.
const localDiscoveryTimeout = 3 * time.Second

// localModelCacheTTL keeps discovered local models warm so re-rendering the
// model list does not re-probe on every request.
const localModelCacheTTL = 5 * time.Minute

const (
	miniappModelHealthOnline  = "online"
	miniappModelHealthOffline = "offline"
	miniappModelHealthUnknown = "unknown"
	miniappModelHealthTimeout = 1500 * time.Millisecond
	// customModelProbeTimeout bounds the /models probe used to auto-detect a
	// newly-added custom model's context window. Best-effort: a miss just
	// omits contextWindow.
	customModelProbeTimeout = 3 * time.Second
)

// modelEntry is one selectable model in the miniapp model picker.
type modelEntry struct {
	provider string // provider ID (zai, vllm, openrouter, ...)
	label    string // button label
	fullID   string // full model ID sent to the LLM + callback (provider/model)
	display  string // short display name (no provider prefix)
}

// modelSection is a titled group of model entries in the model picker.
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
// model list re-renders instantly after the first probe.
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

type miniappModelSnapshot struct {
	sections []modelSection
	health   map[string]string
}

// providerModelProbe captures what a health probe learned about one provider.
//
//	checked   — a probe was attempted for this provider
//	reachable — the endpoint answered (any HTTP status) or local discovery
//	            returned models; false means a network failure / provider down
//	listed    — we obtained a parseable served-model list, so "model not in
//	            models" is a meaningful offline signal. When false (e.g. an
//	            Anthropic-format endpoint without /models) reachability alone
//	            decides the dot, never a false "offline".
type providerModelProbe struct {
	checked   bool
	reachable bool
	listed    bool
	models    []string
}

// miniappProbeClient performs the per-provider /models reachability probes.
// The per-request context (miniappModelHealthTimeout) bounds each call; the
// client timeout is a backstop for a stuck connection.
var miniappProbeClient = &http.Client{Timeout: miniappModelHealthTimeout}

func (s *Server) miniappModelMethods() map[string]rpcutil.HandlerFunc {
	return handlerminiapp.ModelMethods(handlerminiapp.ModelDeps{
		CurrentModel: s.currentMiniappModel,
		RoleModels:   s.roleMiniappModels,
		ListModels:   s.listMiniappModels,
		SetModel:     s.setMiniappModel,
		AddModel:     s.addMiniappCustomModel,
		DeleteModel:  s.deleteMiniappCustomModel,
	})
}

func (s *Server) currentMiniappModel() string {
	if s.chatHandler != nil {
		if m := s.chatHandler.DefaultModel(); m != "" {
			return m
		}
	}
	if s.modelRegistry != nil {
		return s.modelRegistry.FullModelID(modelrole.RoleMain)
	}
	return ""
}

func (s *Server) listMiniappModels(ctx context.Context) ([]handlerminiapp.ModelSection, error) {
	current := s.currentMiniappModel()
	snapshot := s.miniappModelSnapshot(ctx)

	out := make([]handlerminiapp.ModelSection, 0, len(snapshot.sections))
	for _, section := range snapshot.sections {
		models := make([]handlerminiapp.ModelOption, 0, len(section.entries))
		for _, entry := range section.entries {
			models = append(models, handlerminiapp.ModelOption{
				ID:       entry.fullID,
				Label:    entry.label,
				Provider: entry.provider,
				Display:  entry.display,
				Health:   snapshot.health[entry.fullID],
				Current:  entry.fullID == current,
				Custom:   isMiniappCustomProvider(entry.provider),
			})
		}
		if len(models) > 0 {
			out = append(out, handlerminiapp.ModelSection{
				Title:  section.title,
				Models: models,
			})
		}
	}
	if out == nil {
		out = []handlerminiapp.ModelSection{}
	}
	return out, nil
}

func (s *Server) setMiniappModel(ctx context.Context, role, requested string) (string, error) {
	role = strings.TrimSpace(role)
	if role == "" {
		role = "main"
	}
	switch role {
	case "main", "lightweight", "fallback":
	default:
		return "", rpcerr.InvalidRequest("unknown model role: " + role)
	}

	modelID := strings.TrimSpace(requested)
	if s.modelRegistry != nil {
		if resolved, _, ok := s.modelRegistry.ResolveModel(modelID); ok {
			modelID = resolved
		}
	}

	allowed := false
	for _, section := range s.miniappModelSections(ctx) {
		for _, entry := range section.entries {
			if entry.fullID == modelID {
				allowed = true
				break
			}
		}
		if allowed {
			break
		}
	}
	if !allowed {
		return "", rpcerr.Newf(protocol.ErrNotFound, "model not available: %s", requested)
	}

	cfgPath := config.ResolveConfigPath()
	if err := config.PersistRoleModel(cfgPath, role, modelID, s.logger); err != nil {
		return "", rpcerr.WrapDependencyFailed("persist role model", err)
	}

	// Apply in-memory so the change takes effect without a gateway restart.
	switch role {
	case "main":
		if s.chatHandler == nil {
			return "", rpcerr.Unavailable("chat handler is not ready")
		}
		s.chatHandler.SetDefaultModel(modelID)
	default:
		if s.modelRegistry == nil {
			return "", rpcerr.Unavailable("model registry is not ready")
		}
		s.modelRegistry.SetRoleModelID(modelrole.Role(role), modelID)
	}
	return modelID, nil
}

// roleMiniappModels reports the model bound to each registry role for the
// per-role picker (main/lightweight/fallback). Main reflects the live
// chat-handler default when a /model switch changed it this session.
func (s *Server) roleMiniappModels() []handlerminiapp.RoleModel {
	if s.modelRegistry == nil {
		return nil
	}
	roleList := []modelrole.Role{modelrole.RoleMain, modelrole.RoleLightweight, modelrole.RoleFallback}
	out := make([]handlerminiapp.RoleModel, 0, len(roleList))
	for _, r := range roleList {
		out = append(out, handlerminiapp.RoleModel{
			Role:  string(r),
			Model: s.modelRegistry.FullModelID(r),
		})
	}
	if s.chatHandler != nil {
		if m := s.chatHandler.DefaultModel(); m != "" {
			out[0].Model = m
		}
	}
	return out
}

func (s *Server) addMiniappCustomModel(ctx context.Context, endpoint, model string) (handlerminiapp.ModelAddResult, error) {
	cfgPath := config.ResolveConfigPath()
	meta := s.detectCustomModelMeta(ctx, endpoint, model)
	persisted, err := config.PersistCustomProviderModel(cfgPath, endpoint, model, meta, s.logger)
	if err != nil {
		if errors.Is(err, config.ErrInvalidCustomModel) {
			return handlerminiapp.ModelAddResult{}, rpcerr.InvalidRequest(err.Error())
		}
		return handlerminiapp.ModelAddResult{}, rpcerr.WrapDependencyFailed("persist custom model", err)
	}

	localModelCache.mu.Lock()
	localModelCache.models = nil
	localModelCache.builtAt = time.Time{}
	localModelCache.mu.Unlock()

	if s.chatHandler != nil {
		s.chatHandler.SetProviderConfigs(loadProviderConfigs(s.logger))
	}

	return handlerminiapp.ModelAddResult{
		OK:       true,
		ID:       persisted.FullModelID,
		Provider: persisted.ProviderID,
		Endpoint: persisted.BaseURL,
		Model:    persisted.ModelID,
		Added:    persisted.Added,
	}, nil
}

// deleteMiniappCustomModel removes a user-added custom model and applies the
// change live (no gateway restart). If the deleted model was bound to a role,
// that role is reset to the local vLLM default — the same fallback a fresh
// registry build applies for an unset role — so a deletion never leaves a
// dangling reference behind. The inverse of addMiniappCustomModel.
func (s *Server) deleteMiniappCustomModel(_ context.Context, id string) (handlerminiapp.ModelDeleteResult, error) {
	cfgPath := config.ResolveConfigPath()
	deleted, err := config.DeleteCustomProviderModel(cfgPath, id, s.logger)
	if err != nil {
		if errors.Is(err, config.ErrInvalidCustomModel) {
			return handlerminiapp.ModelDeleteResult{}, rpcerr.InvalidRequest(err.Error())
		}
		return handlerminiapp.ModelDeleteResult{}, rpcerr.WrapDependencyFailed("delete custom model", err)
	}
	if !deleted.Removed {
		return handlerminiapp.ModelDeleteResult{}, rpcerr.Newf(protocol.ErrNotFound, "custom model not found: %s", id)
	}

	// Drop cached local-model discovery so the removed entry disappears from
	// the picker (mirrors addMiniappCustomModel).
	localModelCache.mu.Lock()
	localModelCache.models = nil
	localModelCache.builtAt = time.Time{}
	localModelCache.mu.Unlock()

	// Reset any role that was bound to the deleted model to the local vLLM
	// default. SetRoleModelID reconciles the actual served vLLM model name, so
	// this stays valid even if config drifted.
	defaultModel := "vllm/" + modelrole.DefaultVllmModel
	for _, role := range deleted.ClearedRoles {
		switch role {
		case "main":
			// Reset both the live chat default and the registry role: the
			// chat default drives currentMiniappModel(), while the registry
			// feeds the picker's 역할 section — leaving the registry stale
			// would keep the just-deleted model visible there.
			if s.chatHandler != nil {
				s.chatHandler.SetDefaultModel(defaultModel)
			}
			if s.modelRegistry != nil {
				s.modelRegistry.SetRoleModelID(modelrole.RoleMain, defaultModel)
			}
		case "lightweight", "fallback":
			if s.modelRegistry != nil {
				s.modelRegistry.SetRoleModelID(modelrole.Role(role), defaultModel)
			}
		}
	}

	if s.chatHandler != nil {
		s.chatHandler.SetProviderConfigs(loadProviderConfigs(s.logger))
	}

	return handlerminiapp.ModelDeleteResult{
		OK:           true,
		ID:           deleted.FullModelID,
		Removed:      true,
		ClearedRoles: deleted.ClearedRoles,
		Current:      s.currentMiniappModel(),
	}, nil
}

// detectCustomModelMeta best-effort probes the endpoint's /models so a newly
// added custom model is persisted with its context window and a display name
// instead of a bare {"id": ...} stub. contextWindow is left 0 when the probe
// fails or the server omits max_model_len; name defaults to the model id.
func (s *Server) detectCustomModelMeta(ctx context.Context, endpoint, model string) config.CustomModelMeta {
	endpoint = strings.TrimSpace(endpoint)
	model = strings.TrimSpace(model)
	meta := config.CustomModelMeta{Name: model}
	if endpoint == "" || model == "" {
		return meta
	}
	probeCtx, cancel := context.WithTimeout(ctx, customModelProbeTimeout)
	defer cancel()
	infos, err := modelrole.DiscoverServedVllmModelInfos(probeCtx, endpoint)
	if err != nil {
		return meta
	}
	for _, info := range infos {
		if info.ID == model && info.MaxModelLen > 0 {
			meta.ContextWindow = info.MaxModelLen
			break
		}
	}
	return meta
}

func (s *Server) miniappModelSnapshot(ctx context.Context) miniappModelSnapshot {
	roles := registryRoleEntries(s.modelRegistry, s.currentMiniappModel())
	providers := appendBuiltinProviders(loadConfiguredProviders())
	discovered := s.discoverMiniappLocalModels(ctx, providers)
	probes := s.miniappModelHealthProbes(ctx, providers, discovered)
	for i := range providers {
		providers[i].models = mergeModels(providers[i].models, discovered[providers[i].name])
		if len(providers[i].models) > maxModelsPerProvider {
			providers[i].models = providers[i].models[:maxModelsPerProvider]
		}
	}
	sections := assembleMiniappModelSections(roles, providers)
	return miniappModelSnapshot{
		sections: sections,
		health:   buildMiniappModelHealth(sections, probes),
	}
}

func (s *Server) miniappModelSections(ctx context.Context) []modelSection {
	roles := registryRoleEntries(s.modelRegistry, s.currentMiniappModel())
	providers := appendBuiltinProviders(loadConfiguredProviders())
	discovered := s.discoverMiniappLocalModels(ctx, providers)
	for i := range providers {
		providers[i].models = mergeModels(providers[i].models, discovered[providers[i].name])
		if len(providers[i].models) > maxModelsPerProvider {
			providers[i].models = providers[i].models[:maxModelsPerProvider]
		}
	}
	return assembleMiniappModelSections(roles, providers)
}

func (s *Server) miniappModelHealthProbes(
	ctx context.Context,
	providers []providerSpec,
	localDiscovered map[string][]string,
) map[string]providerModelProbe {
	probes := make(map[string]providerModelProbe, len(providers))
	type target struct {
		name    string
		baseURL string
	}
	var targets []target
	for _, provider := range providers {
		baseURL := effectiveBaseURL(provider)
		if baseURL == "" {
			continue
		}
		if isLocalURL(baseURL) {
			// Local providers are probed once by discoverMiniappLocalModels and
			// reused here; a non-empty served list means up + enumerable.
			models := localDiscovered[provider.name]
			probes[provider.name] = providerModelProbe{
				checked:   true,
				reachable: len(models) > 0,
				listed:    len(models) > 0,
				models:    models,
			}
			continue
		}
		// Every remote provider with a resolvable endpoint (built-in cloud or
		// configured custom) gets a live reachability probe — previously only
		// custom providers were checked, leaving cloud models permanently gray.
		targets = append(targets, target{name: provider.name, baseURL: baseURL})
	}
	if len(targets) == 0 {
		return probes
	}

	results := make([]providerModelProbe, len(targets))
	var wg sync.WaitGroup
	for i, target := range targets {
		wg.Add(1)
		go func(idx int, name, baseURL string) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil && s.logger != nil {
					s.logger.Error("panic in miniapp model health probe", "provider", name, "panic", r)
				}
			}()
			probeCtx, cancel := context.WithTimeout(ctx, miniappModelHealthTimeout)
			defer cancel()
			models, listed, reachable := probeModelsClassified(probeCtx, baseURL)
			results[idx] = providerModelProbe{
				checked:   true,
				reachable: reachable,
				listed:    listed,
				models:    models,
			}
		}(i, target.name, target.baseURL)
	}
	wg.Wait()

	for i, target := range targets {
		probes[target.name] = results[i]
	}
	return probes
}

func buildMiniappModelHealth(
	sections []modelSection,
	probes map[string]providerModelProbe,
) map[string]string {
	health := make(map[string]string)
	for _, section := range sections {
		for _, entry := range section.entries {
			health[entry.fullID] = miniappModelHealthForEntry(entry, probes)
		}
	}
	return health
}

func miniappModelHealthForEntry(entry modelEntry, probes map[string]providerModelProbe) string {
	if entry.provider == "" {
		return miniappModelHealthUnknown
	}
	probe, ok := probes[entry.provider]
	if !ok || !probe.checked {
		return miniappModelHealthUnknown
	}
	// When we have a served-model list, membership is authoritative: present →
	// online, absent → offline (e.g. a mistyped local/custom model name).
	if probe.listed {
		modelID := modelIDForProviderEntry(entry)
		for _, served := range probe.models {
			if served == modelID {
				return miniappModelHealthOnline
			}
		}
		return miniappModelHealthOffline
	}
	// No enumerable list (Anthropic-format endpoints without /models, non-OK
	// responses): a reachable endpoint counts as usable, never a false offline.
	if probe.reachable {
		return miniappModelHealthOnline
	}
	return miniappModelHealthOffline
}

func modelIDForProviderEntry(entry modelEntry) string {
	if entry.provider != "" {
		if modelID, ok := strings.CutPrefix(entry.fullID, entry.provider+"/"); ok {
			return modelID
		}
	}
	return entry.display
}

func isMiniappCustomProvider(name string) bool {
	return name == "custom" || strings.HasPrefix(name, "custom-")
}

// probeModelsClassified does GET <baseURL>/models and classifies the outcome so
// the picker can show a meaningful dot for any OpenAI-style endpoint:
//
//	reachable=false                 → network error / timeout (provider down)
//	reachable=true, listed=false    → endpoint answered but no parseable model
//	                                  list (non-200, or not OpenAI-shaped, e.g.
//	                                  Anthropic-format providers) — treat as up
//	reachable=true, listed=true     → models holds the served model IDs
//
// No auth header is sent: the goal is reachability + (when available) the served
// set, and many /models endpoints (e.g. OpenRouter) are public while others
// answer 401/404 — all of which still prove the endpoint is up.
func probeModelsClassified(ctx context.Context, baseURL string) (models []string, listed, reachable bool) {
	endpoint := strings.TrimRight(strings.TrimSpace(baseURL), "/") + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, false, false
	}
	resp, err := miniappProbeClient.Do(req)
	if err != nil {
		return nil, false, false // network failure → provider unreachable
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false, true // reachable, but no usable model list
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&payload); err != nil {
		return nil, false, true // reachable, but response is not OpenAI-shaped
	}
	for _, m := range payload.Data {
		if id := strings.TrimSpace(m.ID); id != "" {
			models = append(models, id)
		}
	}
	if len(models) == 0 {
		return nil, false, true // reachable, list empty/unenumerable
	}
	return models, true, true
}

func assembleMiniappModelSections(roles []modelEntry, providers []providerSpec) []modelSection {
	seen := make(map[string]struct{})
	var sections []modelSection

	add := func(title string, entries []modelEntry) {
		var kept []modelEntry
		for _, entry := range entries {
			if entry.fullID == "" {
				continue
			}
			if _, dup := seen[entry.fullID]; dup {
				continue
			}
			seen[entry.fullID] = struct{}{}
			kept = append(kept, entry)
		}
		if len(kept) > 0 {
			sections = append(sections, modelSection{title: title, entries: kept})
		}
	}

	add("역할", roles)
	for _, provider := range providers {
		add(providerDisplayName(provider.name), providerEntries(provider))
	}
	return sections
}

func (s *Server) discoverMiniappLocalModels(ctx context.Context, providers []providerSpec) map[string][]string {
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
	for _, provider := range providers {
		if base := effectiveBaseURL(provider); isLocalURL(base) {
			targets = append(targets, target{name: provider.name, baseURL: base})
		}
	}

	out := make(map[string][]string)
	if len(targets) > 0 {
		results := make([][]string, len(targets))
		var wg sync.WaitGroup
		for i, target := range targets {
			wg.Add(1)
			go func(idx int, name, baseURL string) {
				defer wg.Done()
				defer func() {
					if r := recover(); r != nil {
						s.logger.Error("panic in miniapp model discovery probe", "provider", name, "panic", r)
					}
				}()
				probeCtx, cancel := context.WithTimeout(ctx, localDiscoveryTimeout)
				defer cancel()
				results[idx] = discoverProviderModels(probeCtx, baseURL)
			}(i, target.name, target.baseURL)
		}
		wg.Wait()
		for i, target := range targets {
			if len(results[i]) > 0 {
				out[target.name] = results[i]
			}
		}
	}

	localModelCache.mu.Lock()
	localModelCache.models = out
	localModelCache.builtAt = time.Now()
	localModelCache.mu.Unlock()
	return out
}

// --- Model-picker shared helpers (previously in inbound_models.go) ---

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
	if name == "custom" || strings.HasPrefix(name, "custom-") {
		return "직접 추가"
	}
	return name
}

// builtinProviders lists the well-known providers Deneb ships with.
func builtinProviders() []providerSpec {
	return []providerSpec{
		{name: "zai", models: []string{"glm-5-turbo", "glm-5.1"}},
		{name: "openrouter", models: []string{
			"anthropic/claude-opus-4.7",
			"anthropic/claude-sonnet-4.6",
			"google/gemini-3.1-pro",
		}},
		{name: "vllm"},
		{name: "localai"},
		{name: "kimi", models: []string{"kimi-for-coding"}},
		{name: "mimo-plan", models: []string{"mimo-v2.5-pro"}},
	}
}

// appendBuiltinProviders merges built-in providers with operator-configured
// ones (explicit config wins).
func appendBuiltinProviders(configured []providerSpec) []providerSpec {
	builtin := builtinProviders()
	builtinByName := make(map[string]providerSpec, len(builtin))
	for _, b := range builtin {
		builtinByName[b.name] = b
	}
	have := make(map[string]struct{}, len(configured))
	for i := range configured {
		have[configured[i].name] = struct{}{}
		if len(configured[i].models) == 0 {
			if b, ok := builtinByName[configured[i].name]; ok && len(b.models) > 0 {
				configured[i].models = append([]string(nil), b.models...)
			}
		}
	}
	for _, b := range builtin {
		if _, ok := have[b.name]; !ok {
			configured = append(configured, b)
		}
	}
	sort.Slice(configured, func(i, j int) bool { return configured[i].name < configured[j].name })
	return configured
}

// isLocalURL reports whether a base URL points at a loopback host.
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

// effectiveBaseURL returns the provider's base URL, falling back to known defaults.
func effectiveBaseURL(spec providerSpec) string {
	if spec.baseURL != "" {
		return spec.baseURL
	}
	switch spec.name {
	case "vllm":
		return modelrole.DefaultVllmBaseURL
	case "localai":
		return modelrole.DefaultLocalAIBaseURL
	case "zai":
		return modelrole.DefaultZaiBaseURL
	case "openrouter":
		return "https://openrouter.ai/api/v1"
	case "kimi":
		return modelrole.DefaultKimiBaseURL
	case "mimo":
		return modelrole.DefaultMimoBaseURL
	case "mimo-plan":
		return modelrole.DefaultMimoPlanBaseURL
	}
	return ""
}

// loadConfiguredProviders reads models.providers from deneb.json.
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

// mergeModels concatenates configured + discovered model ids, de-duplicating.
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

// providerEntries builds the model entries for one provider section.
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
func registryRoleEntries(reg *modelrole.Registry, liveMain string) []modelEntry {
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
		if r.role == modelrole.RoleMain {
			if live := strings.TrimSpace(liveMain); live != "" {
				providerID, _ := modelrole.ParseModelID(live)
				entries = append(entries, modelEntry{
					provider: providerID,
					label:    r.label + ": " + shortModelName(live),
					fullID:   live,
					display:  shortModelName(live),
				})
				continue
			}
		}
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

// discoverProviderModels probes an OpenAI-compatible /models endpoint.
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

// miniapp_models_health.go — model-picker state assembly: the snapshot the
// native client renders (sections + health), provider/model health probing,
// and local vLLM model discovery with its TTL cache. Split from
// miniapp_models.go (RPC surface).
package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

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
		health:   buildMiniappModelHealth(sections, probes, s.roleHealthVerdicts()),
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
	roleVerdicts map[string]string,
) map[string]string {
	health := make(map[string]string)
	for _, section := range sections {
		for _, entry := range section.entries {
			health[entry.fullID] = miniappModelHealthForEntry(entry, probes, roleVerdicts)
		}
	}
	return health
}

func miniappModelHealthForEntry(entry modelEntry, probes map[string]providerModelProbe, roleVerdicts map[string]string) string {
	if entry.provider == "" {
		return miniappModelHealthUnknown
	}
	// A rejected credential trumps reachability: the endpoint may answer
	// every GET while refusing all real completions (the 2026-06 Z.AI key
	// expiry). Only role-assigned cloud providers are probed this deeply.
	if roleVerdicts[entry.provider] == roleHealthAuth {
		return miniappModelHealthAuth
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

// isMiniappLocalProvider reports whether a provider serves node-local models
// (the vLLM cluster / LocalAI) that are role-critical and ops-managed, hence
// not removable from the picker.
func isMiniappLocalProvider(name string) bool {
	return name == "vllm" || name == "localai"
}

// isMiniappDeletableProvider reports whether the picker may offer to remove a
// model from this provider: user-added custom providers and cloud-catalog
// providers (openrouter/zai/kimi/mimo-plan/…) are deletable; node-local and
// empty/unknown providers are not.
func isMiniappDeletableProvider(name string) bool {
	return name != "" && !isMiniappLocalProvider(name)
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
		apiKey  string
	}
	var targets []target
	for _, provider := range providers {
		if base := effectiveBaseURL(provider); isLocalURL(base) {
			targets = append(targets, target{name: provider.name, baseURL: base, apiKey: provider.apiKey})
		}
	}

	out := make(map[string][]string)
	if len(targets) > 0 {
		results := make([][]string, len(targets))
		var wg sync.WaitGroup
		for i, target := range targets {
			wg.Add(1)
			go func(idx int, name, baseURL, apiKey string) {
				defer wg.Done()
				defer func() {
					if r := recover(); r != nil {
						s.logger.Error("panic in miniapp model discovery probe", "provider", name, "panic", r)
					}
				}()
				probeCtx, cancel := context.WithTimeout(ctx, localDiscoveryTimeout)
				defer cancel()
				results[idx] = discoverProviderModels(probeCtx, baseURL, apiKey)
			}(i, target.name, target.baseURL, target.apiKey)
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

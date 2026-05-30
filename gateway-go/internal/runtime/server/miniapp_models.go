package server

import (
	"context"
	"errors"
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

const (
	miniappModelHealthOnline  = "online"
	miniappModelHealthOffline = "offline"
	miniappModelHealthUnknown = "unknown"
	miniappModelHealthTimeout = 1500 * time.Millisecond
)

type miniappModelSnapshot struct {
	sections []modelSection
	health   map[string]string
}

type providerModelProbe struct {
	checked bool
	models  []string
}

func (s *Server) miniappModelMethods() map[string]rpcutil.HandlerFunc {
	return handlerminiapp.ModelMethods(handlerminiapp.ModelDeps{
		CurrentModel: s.currentMiniappModel,
		RoleModels:   s.roleMiniappModels,
		ListModels:   s.listMiniappModels,
		SetModel:     s.setMiniappModel,
		AddModel:     s.addMiniappCustomModel,
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

func (s *Server) addMiniappCustomModel(_ context.Context, endpoint, model string) (handlerminiapp.ModelAddResult, error) {
	cfgPath := config.ResolveConfigPath()
	persisted, err := config.PersistCustomProviderModel(cfgPath, endpoint, model, s.logger)
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
			probes[provider.name] = providerModelProbe{
				checked: true,
				models:  localDiscovered[provider.name],
			}
			continue
		}
		if strings.TrimSpace(provider.baseURL) == "" || !isMiniappCustomProvider(provider.name) {
			continue
		}
		targets = append(targets, target{name: provider.name, baseURL: baseURL})
	}
	if len(targets) == 0 {
		return probes
	}

	results := make([][]string, len(targets))
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
			results[idx] = probeProviderModelIDs(probeCtx, baseURL)
		}(i, target.name, target.baseURL)
	}
	wg.Wait()

	for i, target := range targets {
		probes[target.name] = providerModelProbe{
			checked: true,
			models:  results[i],
		}
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
	modelID := modelIDForProviderEntry(entry)
	for _, served := range probe.models {
		if served == modelID {
			return miniappModelHealthOnline
		}
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

func probeProviderModelIDs(ctx context.Context, baseURL string) []string {
	ids, err := modelrole.DiscoverServedVllmModels(ctx, baseURL)
	if err != nil {
		return nil
	}
	return ids
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

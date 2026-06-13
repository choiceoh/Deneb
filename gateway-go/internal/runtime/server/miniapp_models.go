package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modeltuner"
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
	// miniappModelHealthAuth marks a provider whose endpoint answers but
	// whose credential is rejected (401/403-class). Reachability probes
	// alone cannot see this — Z.AI returns 200 on GET /models even for an
	// expired key — so the verdict comes from the role health watch's real
	// 1-token probes (role_health_watch.go) and overrides reachability.
	miniappModelHealthAuth    = "auth"
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
		Advisories: func() []string {
			return modeltuner.LoadScorecard(modeltuner.DefaultStatePath()).AdvisoryLines()
		},
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
	// Tuner enrichment: latest scorecard window (small JSON, read per list
	// call — the picker is an on-demand screen) + circuit-breaker state.
	scorecard := modeltuner.LoadScorecard(modeltuner.DefaultStatePath())
	hidden := config.LoadHiddenModels(config.ResolveConfigPath())

	out := make([]handlerminiapp.ModelSection, 0, len(snapshot.sections))
	for _, section := range snapshot.sections {
		models := make([]handlerminiapp.ModelOption, 0, len(section.entries))
		for _, entry := range section.entries {
			if hidden[entry.fullID] {
				continue // soft-hidden via models.hiddenModels (deleted built-in cloud model)
			}
			modelName := entry.display
			if modelName == "" {
				_, modelName = modelrole.ParseModelID(entry.fullID)
			}
			unhealthy := false
			tunedFloor := 0
			if s.modelRegistry != nil {
				unhealthy = s.modelRegistry.ModelUnhealthy(modelName)
				tunedFloor = s.modelRegistry.TunedMaxTokens(modelName)
			}
			models = append(models, handlerminiapp.ModelOption{
				ID:        entry.fullID,
				Label:     entry.label,
				Provider:  entry.provider,
				Display:   entry.display,
				Health:    snapshot.health[entry.fullID],
				Current:   entry.fullID == current,
				Custom:    isMiniappCustomProvider(entry.provider),
				Deletable: isMiniappDeletableProvider(entry.provider),
				Unhealthy: unhealthy,
				Note:      scorecard.NoteFor(modelName, tunedFloor),
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
	// Must mirror the native picker's ModelRole enum (DenebConfigScreen.kt) and
	// the roles reported by roleMiniappModels — tiny/analysis were added to both
	// the picker and the list response in #2065, but this gate stayed at the
	// original three roles, so switching the 초경량/분석 tiers was rejected here
	// ("unknown model role") and surfaced as "모델 전환에 실패했어요" to the user.
	switch role {
	case "main", "tiny", "lightweight", "analysis", "fallback":
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
// per-role picker (main/tiny/lightweight/analysis/fallback). Main reflects the
// live chat-handler default when a /model switch changed it this session.
func (s *Server) roleMiniappModels() []handlerminiapp.RoleModel {
	if s.modelRegistry == nil {
		return nil
	}
	roleList := []modelrole.Role{
		modelrole.RoleMain,
		modelrole.RoleTiny,
		modelrole.RoleLightweight,
		modelrole.RoleAnalysis,
		modelrole.RoleFallback,
	}
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

// deleteMiniappCustomModel removes a model from the picker and applies the
// change live (no gateway restart). Three cases by provider:
//   - custom/custom-N (user-added)      → the entry is removed from config
//   - cloud catalog (openrouter/zai/…)  → soft-hidden via models.hiddenModels
//     (the built-in catalog re-merges these every build, so a config removal
//     wouldn't stick — a hide entry does)
//   - vllm/localai (node-local)         → rejected; role-critical, ops-managed
//
// Any role bound to the removed model is reset to the local vLLM default — the
// same fallback a fresh registry build applies for an unset role — so a deletion
// never leaves a dangling reference behind. The inverse of addMiniappCustomModel.
func (s *Server) deleteMiniappCustomModel(_ context.Context, id string) (handlerminiapp.ModelDeleteResult, error) {
	cfgPath := config.ResolveConfigPath()
	provider, _ := modelrole.ParseModelID(id)
	if isMiniappLocalProvider(provider) {
		return handlerminiapp.ModelDeleteResult{}, rpcerr.InvalidRequest("로컬 모델(vLLM/LocalAI)은 삭제할 수 없습니다")
	}

	var fullID string
	var clearedRoles []string
	if isMiniappCustomProvider(provider) {
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
		fullID, clearedRoles = deleted.FullModelID, deleted.ClearedRoles
	} else {
		hidden, err := config.HideModel(cfgPath, id, s.logger)
		if err != nil {
			if errors.Is(err, config.ErrInvalidCustomModel) {
				return handlerminiapp.ModelDeleteResult{}, rpcerr.InvalidRequest(err.Error())
			}
			return handlerminiapp.ModelDeleteResult{}, rpcerr.WrapDependencyFailed("hide model", err)
		}
		fullID, clearedRoles = hidden.FullModelID, hidden.ClearedRoles
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
	for _, role := range clearedRoles {
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
		case "lightweight", "tiny", "analysis", "fallback":
			// tiny/analysis are bindable from the picker too (see roleMiniappModels),
			// so a deleted model could be left dangling on those roles if not reset.
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
		ID:           fullID,
		Removed:      true,
		ClearedRoles: clearedRoles,
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

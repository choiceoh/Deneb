package modelpicker

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modeltuner"
	"github.com/choiceoh/deneb/gateway-go/internal/core/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/core/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// maxModelsPerProvider caps the models shown per provider in the model picker
// so the list stays navigable on a phone screen. Operator-declared models
// (deneb.json models.providers.<id>.models) are exempt — declaring a model is
// an explicit "show this"; the cap only bounds discovered extras appended
// after them (capMergedModels).
const maxModelsPerProvider = 12

// maxDiscoveredModels bounds a runaway /models response (an aggregator can
// list hundreds). Deliberately far above maxModelsPerProvider: the discovered
// list is also the health-membership authority, and trimming it to the display
// cap made every served-but-trimmed model render as a false "offline" red dot
// while answering completions fine (wormhole at 10 entries, 2026-07).
const maxDiscoveredModels = 100

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
	apiKey  string   // credential (env-expanded); sent on the local /models probe for token-gated endpoints (wormhole)
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

// Controller owns model-picker discovery, health, selection, and persistence.
type Controller struct {
	modelRegistry               *modelrole.Registry
	chatHandler                 chatport.ModelController
	logger                      *slog.Logger
	roleHealthVerdicts          func() map[string]string
	refreshCodingModelConsumers func()
	providerConfigs             func() map[string]chatport.ProviderConfig
	usageStats                  func(sinceMs int64) []agentlog.ModelStat
	sessions                    sessionBinder

	// usageCache memoizes the 24h usage aggregation: usageStats folds every
	// agent-log file on each call, which is too heavy per list render.
	usageCache struct {
		mu      sync.Mutex
		byModel map[string]agentlog.ModelStat
		builtAt time.Time
	}
}

// usageCacheTTL bounds how stale the picker's 24h usage figures can be. The
// screen is an on-demand settings view; a minute of staleness is invisible.
const usageCacheTTL = time.Minute

// ControllerConfig contains the live model-system boundaries used by Controller.
type ControllerConfig struct {
	Registry                    *modelrole.Registry
	ChatHandler                 chatport.ModelController
	Logger                      *slog.Logger
	RoleHealthVerdicts          func() map[string]string
	RefreshCodingModelConsumers func()
	ProviderConfigs             func() map[string]chatport.ProviderConfig
	// UsageStats aggregates per-model run/token counters since a cutoff
	// (agentlog.Writer.AggregateByModel). nil = no usage enrichment.
	UsageStats func(sinceMs int64) []agentlog.ModelStat
	// Sessions patches a conversation's model when the chat picker sends
	// miniapp.models.set with sessionKey. nil disables session-scoped set.
	Sessions sessionBinder
}

// NewController constructs a model-picker controller.
func NewController(cfg ControllerConfig) *Controller {
	return &Controller{
		modelRegistry:               cfg.Registry,
		chatHandler:                 cfg.ChatHandler,
		logger:                      cfg.Logger,
		roleHealthVerdicts:          cfg.RoleHealthVerdicts,
		refreshCodingModelConsumers: cfg.RefreshCodingModelConsumers,
		providerConfigs:             cfg.ProviderConfigs,
		usageStats:                  cfg.UsageStats,
		sessions:                    cfg.Sessions,
	}
}

// usage24h returns the last-24h per-model usage keyed by "provider/model" AND
// bare model name (stats may omit the provider), memoized for usageCacheTTL.
func (s *Controller) usage24h() map[string]agentlog.ModelStat {
	if s.usageStats == nil {
		return nil
	}
	s.usageCache.mu.Lock()
	defer s.usageCache.mu.Unlock()
	if s.usageCache.byModel != nil && time.Since(s.usageCache.builtAt) < usageCacheTTL {
		return s.usageCache.byModel
	}
	cutoff := time.Now().Add(-24 * time.Hour).UnixMilli()
	byModel := make(map[string]agentlog.ModelStat)
	for _, st := range s.usageStats(cutoff) {
		if st.Model == "" {
			continue
		}
		key := st.Model
		if st.Provider != "" {
			key = st.Provider + "/" + st.Model
		}
		byModel[key] = st
		// Bare-name fallback: keep the row with more runs when two providers
		// serve the same model name.
		if prior, ok := byModel[st.Model]; !ok || st.Runs > prior.Runs {
			byModel[st.Model] = st
		}
	}
	s.usageCache.byModel = byModel
	s.usageCache.builtAt = time.Now()
	return byModel
}

func (s *Controller) chatReady() bool {
	return s.chatHandler != nil && s.chatHandler.ChatReady()
}

// Methods returns the complete miniapp model-picker RPC domain.
func (s *Controller) Methods() map[string]rpcutil.HandlerFunc {
	return handlerminiapp.ModelMethods(handlerminiapp.ModelDeps{
		CurrentModel:    s.currentMiniappModel,
		RoleModels:      s.roleMiniappModels,
		ListModels:      s.listMiniappModels,
		SetModel:        s.setMiniappModel,
		SetSessionModel: s.setMiniappSessionModel,
		AddModel:        s.addMiniappCustomModel,
		DeleteModel:     s.deleteMiniappCustomModel,
		MainHasVision:   s.mainModelHasVision,
		Advisories: func() []string {
			return modeltuner.LoadScorecard(modeltuner.DefaultStatePath()).AdvisoryLines()
		},
	})
}

func (s *Controller) currentMiniappModel() string {
	if s.chatReady() {
		if m := s.chatHandler.DefaultModel(); m != "" {
			return m
		}
	}
	if s.modelRegistry != nil {
		return s.modelRegistry.FullModelID(modelrole.RoleMain)
	}
	return ""
}

func (s *Controller) listMiniappModels(ctx context.Context) ([]handlerminiapp.ModelSection, error) {
	current := s.currentMiniappModel()
	snapshot := s.miniappModelSnapshot(ctx)
	// Tuner enrichment: latest scorecard window (small JSON, read per list
	// call — the picker is an on-demand screen) + circuit-breaker state.
	scorecard := modeltuner.LoadScorecard(modeltuner.DefaultStatePath())
	hidden := config.LoadHiddenModels(config.ResolveConfigPath())
	usage := s.usage24h()

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
			stat, ok := usage[entry.fullID]
			if !ok {
				stat = usage[modelName]
			}
			models = append(models, handlerminiapp.ModelOption{
				ID:                 entry.fullID,
				Label:              entry.label,
				Provider:           entry.provider,
				Display:            entry.display,
				Health:             snapshot.health[entry.fullID],
				Current:            entry.fullID == current,
				Custom:             isMiniappCustomProvider(entry.provider),
				Deletable:          isMiniappDeletableProvider(entry.provider),
				Unhealthy:          unhealthy,
				Note:               scorecard.NoteFor(modelName, tunedFloor),
				Runs24h:            stat.Runs,
				InputTokens24h:     stat.InputTokens,
				OutputTokens24h:    stat.OutputTokens,
				CacheReadTokens24h: stat.CacheReadTokens,
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

func (s *Controller) setMiniappModel(ctx context.Context, role, requested string) (string, error) {
	role = strings.TrimSpace(role)
	if role == "" {
		role = "main"
	}
	// Derived from pickerRoles rather than restated: this gate repeatedly
	// drifted behind new roles when it was its own list, rejecting picker
	// entries with "unknown model role" → "모델 전환에 실패했어요".
	if !isPickerRole(role) {
		return "", rpcerr.InvalidRequest("unknown model role: " + role)
	}

	modelID, err := s.resolveAllowedMiniappModel(ctx, requested)
	if err != nil {
		return "", err
	}

	cfgPath := config.ResolveConfigPath()
	if err := config.PersistRoleModel(cfgPath, role, modelID, s.logger); err != nil {
		return "", rpcerr.WrapDependencyFailed("persist role model", err)
	}

	// Apply in-memory so the change takes effect without a gateway restart.
	switch role {
	case "main":
		if !s.chatReady() {
			return "", rpcerr.Unavailable("chat handler is not ready")
		}
		s.chatHandler.SetDefaultModel(modelID)
	default:
		if s.modelRegistry == nil {
			return "", rpcerr.Unavailable("model registry is not ready")
		}
		s.modelRegistry.SetRoleModelID(modelrole.Role(role), modelID)
		if role == string(modelrole.RoleCoding) {
			if s.refreshCodingModelConsumers != nil {
				s.refreshCodingModelConsumers()
			}
		}
	}
	return modelID, nil
}

// roleMiniappModels reports the model bound to each registry role for the
// per-role picker (main/tiny/lightweight/fallback plus opt-in roles).
// Main reflects the
// live chat-handler default when a /model switch changed it this session.
func (s *Controller) roleMiniappModels() []handlerminiapp.RoleModel {
	if s.modelRegistry == nil {
		return nil
	}
	out := make([]handlerminiapp.RoleModel, 0, len(pickerRoles))
	for _, r := range pickerRoles {
		model := s.modelRegistry.FullModelID(r.role)
		if r.role == modelrole.RoleMain && s.chatReady() {
			if m := s.chatHandler.DefaultModel(); m != "" {
				model = m
			}
		}
		// An opt-in role with nothing bound is omitted so the picker shows
		// "미설정" rather than implying a default it does not have.
		if r.optIn && model == "" {
			continue
		}
		out = append(out, handlerminiapp.RoleModel{Role: string(r.role), Model: model})
	}
	return out
}

// mainModelHasVision reports whether the live main model accepts image input.
// The native picker hides the opt-in vision role when this is true (a separate
// vision model is redundant — images route to main directly). It mirrors the
// routing-time capability check (run_prepare.go): false exactly when the main
// model is marked vision:false (modelcaps.NoVision). Unknown models are assumed
// vision-capable, so the vision row surfaces only when main is known not to be.
func (s *Controller) mainModelHasVision() bool {
	if s.modelRegistry == nil {
		return true
	}
	main := s.modelRegistry.FullModelID(modelrole.RoleMain)
	if s.chatReady() {
		if m := s.chatHandler.DefaultModel(); m != "" {
			main = m // reflect a live /model switch this session
		}
	}
	if main == "" {
		return true
	}
	providerID, model := modelrole.ParseModelID(main)
	return !s.modelRegistry.CapabilityForModel(providerID, model).NoVision
}

func (s *Controller) addMiniappCustomModel(ctx context.Context, endpoint, model string) (handlerminiapp.ModelAddResult, error) {
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

	if s.chatReady() {
		if s.providerConfigs != nil {
			s.chatHandler.SetProviderConfigs(s.providerConfigs())
		}
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
func (s *Controller) deleteMiniappCustomModel(_ context.Context, id string) (handlerminiapp.ModelDeleteResult, error) {
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
			if s.chatReady() {
				s.chatHandler.SetDefaultModel(defaultModel)
			}
			if s.modelRegistry != nil {
				s.modelRegistry.SetRoleModelID(modelrole.RoleMain, defaultModel)
			}
		case "lightweight", "tiny", "fallback":
			// tiny is bindable from the picker too (see roleMiniappModels),
			// so a deleted model could be left dangling on those roles if not reset.
			if s.modelRegistry != nil {
				s.modelRegistry.SetRoleModelID(modelrole.Role(role), defaultModel)
			}
		case "coding":
			// Coding is opt-in: remove the role so implementer sub-agents fall
			// back to their normal default after deletion (config already
			// cleared codingModel) instead of pinning it to the vLLM default
			// like the always-on roles above.
			if s.modelRegistry != nil {
				s.modelRegistry.ClearRole(modelrole.RoleCoding)
				if s.refreshCodingModelConsumers != nil {
					s.refreshCodingModelConsumers()
				}
			}
		case "vision":
			// Vision is opt-in like coding: clear the role so image turns revert
			// to the main model when the bound multimodal model is deleted.
			if s.modelRegistry != nil {
				s.modelRegistry.ClearRole(modelrole.RoleVision)
			}
		case "submain":
			// Submain (autonomous lane) is opt-in like coding: clear the role
			// so heartbeat/phone-event judgment falls back to main instead of
			// dangling on the deleted model (config already cleared
			// submainModel).
			if s.modelRegistry != nil {
				s.modelRegistry.ClearRole(modelrole.RoleSubmain)
			}
		}
	}

	if s.chatReady() {
		if s.providerConfigs != nil {
			s.chatHandler.SetProviderConfigs(s.providerConfigs())
		}
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
func (s *Controller) detectCustomModelMeta(ctx context.Context, endpoint, model string) config.CustomModelMeta {
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

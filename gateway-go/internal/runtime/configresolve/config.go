// Package configresolve reads runtime model, workspace, and topic settings.
package configresolve

import (
	"encoding/json"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/provider"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
)

// LoadProviderConfigs reads configured chat providers from the default config path.
func LoadProviderConfigs(logger *slog.Logger) map[string]chatport.ProviderConfig {
	snapshot, err := config.LoadConfigFromDefaultPath()
	if err != nil || !snapshot.Valid || snapshot.Raw == "" {
		return nil
	}

	var root struct {
		Models struct {
			Providers map[string]chatport.ProviderConfig `json:"providers"`
		} `json:"models"`
	}
	if err := json.Unmarshal([]byte(snapshot.Raw), &root); err != nil {
		logger.Warn("failed to parse provider configs", "error", err)
		return nil
	}

	if len(root.Models.Providers) > 0 {
		logger.Info("loaded provider configs", "count", len(root.Models.Providers))
	}
	return root.Models.Providers
}

// ProviderCatalog converts the deneb.json models.providers entries into the
// modelrole registry's dependency-free ProviderResolved shape, so a per-role
// model can target ANY configured provider (e.g. "google/...") instead of
// falling back to modelrole's built-in provider switch.
//
// BaseURL/APIKey expand ${ENV} references here, like the chat path
// (run_provider.go) and the model-picker probe (miniapp_models_providers.go):
// deneb.json keeps secrets as ${WORMHOLE_TOKEN}-style refs, and the registry
// consumers (buildClient → llm.NewClient) never expand — without this the
// literal "${WORMHOLE_TOKEN}" would be sent as the bearer token AND, being
// non-empty, suppress the kimi OAuth-token fallback in buildClient. An unset
// env expands to "" so the built-in env/key fallbacks still apply.
func ProviderCatalog(logger *slog.Logger) map[string]modelrole.ProviderResolved {
	raw := LoadProviderConfigs(logger)
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string]modelrole.ProviderResolved, len(raw))
	for id, p := range raw {
		out[id] = modelrole.ProviderResolved{
			BaseURL:       strings.TrimSpace(provider.ExpandEnvVars(p.BaseURL)),
			APIKey:        strings.TrimSpace(provider.ExpandEnvVars(p.APIKey)),
			APIMode:       p.API,
			ContextWindow: p.ContextWindow,
			Reasoning:     p.Reasoning,
			Vision:        p.Vision,
			PromptCache:   p.PromptCache,
			Temperature:   p.Temperature,
			TopP:          p.TopP,
			TopK:          p.TopK,
			Routing:       convertRoutingConfig(p.Routing),
		}
	}
	return out
}

// convertRoutingConfig maps the deneb.json routing block (toolport JSON shape)
// to the registry's dependency-free RoutingOverride. The two structs are
// field-identical pointer bags; the split mirrors the existing
// ProviderConfig/ProviderResolved boundary so modelrole stays free of the
// config package. Nil in, nil out.
func convertRoutingConfig(r *chatport.RoutingConfig) *modelrole.RoutingOverride {
	if r == nil {
		return nil
	}
	return &modelrole.RoutingOverride{
		Enabled:           r.Enabled,
		ToggleKwarg:       r.ToggleKwarg,
		MaxSimpleRunes:    r.MaxSimpleRunes,
		StepCeilingTurn:   r.StepCeilingTurn,
		ObservationRunes:  r.ObservationRunes,
		CumulativeRunes:   r.CumulativeRunes,
		HeavyHistoryRunes: r.HeavyHistoryRunes,
	}
}

// DefaultModel reads agents.defaultModel or agents.defaults.model from
// deneb.json, falling back to the registry's main model default.
// The model field can be either a string ("model-name") or an object
// ({"primary": "model-name", "fallbacks": [...]}).
func DefaultModel(logger *slog.Logger) string {
	snapshot, err := config.LoadConfigFromDefaultPath()
	if err != nil || !snapshot.Valid || snapshot.Raw == "" {
		return "" // empty: registry will provide the default
	}
	var root struct {
		Agents struct {
			DefaultModel string          `json:"defaultModel"`
			Defaults     json.RawMessage `json:"defaults"`
		} `json:"agents"`
	}
	if err := json.Unmarshal([]byte(snapshot.Raw), &root); err != nil {
		logger.Warn("failed to parse agents config for model", "error", err)
		return "" // empty: registry will provide the default
	}
	if root.Agents.DefaultModel != "" {
		// agents.defaultModel wins over agents.defaults.model.primary. A stale
		// primary that silently loses is a real operator trap — it reads like
		// the main model to anyone inspecting the config — so make the
		// shadowing visible once at startup.
		if shadowed := extractModelFromDefaults(root.Agents.Defaults); shadowed != "" && shadowed != root.Agents.DefaultModel {
			logger.Warn("agents.defaults.model.primary is shadowed by agents.defaultModel and ignored",
				"defaultModel", root.Agents.DefaultModel,
				"shadowedPrimary", shadowed)
		}
		return root.Agents.DefaultModel
	}
	if len(root.Agents.Defaults) > 0 {
		model := extractModelFromDefaults(root.Agents.Defaults)
		if model != "" {
			return model
		}
	}
	return "" // empty: registry will provide the default
}

// ProactiveEscalateThreshold reads agents.proactiveEscalateThreshold from
// deneb.json — the operator's proactive-cadence dial. Returns 0 (= "unset", keep
// the calibrated default) when the config is absent, invalid, or non-positive.
// Mirrors DefaultModel's load-on-read pattern.
func ProactiveEscalateThreshold(logger *slog.Logger) int {
	snapshot, err := config.LoadConfigFromDefaultPath()
	if err != nil || snapshot == nil || !snapshot.Valid || snapshot.Raw == "" {
		return 0
	}
	var root struct {
		Agents struct {
			ProactiveEscalateThreshold *int `json:"proactiveEscalateThreshold"`
		} `json:"agents"`
	}
	if err := json.Unmarshal([]byte(snapshot.Raw), &root); err != nil {
		logger.Warn("failed to parse agents config for proactive threshold", "error", err)
		return 0
	}
	if t := root.Agents.ProactiveEscalateThreshold; t != nil && *t > 0 {
		logger.Info("proactive escalate threshold overridden from config", "threshold", *t)
		return *t
	}
	return 0
}

// LocalVLLMModel reads models.providers.vllm.models[0].id from deneb.json
// to determine the model name the local vLLM server is serving. Returns empty
// string if unconfigured — NewRegistry will fall back to the const default.
func LocalVLLMModel(_ *slog.Logger) string {
	snapshot, err := config.LoadConfigFromDefaultPath()
	if err != nil || !snapshot.Valid || snapshot.Raw == "" {
		return ""
	}
	var root struct {
		Models struct {
			Providers struct {
				Vllm struct {
					Models []struct {
						ID string `json:"id"`
					} `json:"models"`
				} `json:"vllm"`
			} `json:"providers"`
		} `json:"models"`
	}
	if err := json.Unmarshal([]byte(snapshot.Raw), &root); err != nil {
		return ""
	}
	if len(root.Models.Providers.Vllm.Models) == 0 {
		return ""
	}
	return root.Models.Providers.Vllm.Models[0].ID
}

// mailAnalysisModels returns the role-resolved clients and model names shared
// by BOTH mail-analysis paths — the autonomous gmail poller (initGmailPoll)
// LightweightModel reads the optional agents.lightweightModel override.
func LightweightModel(logger *slog.Logger) string {
	return agentRoleModel("lightweightModel", logger)
}

// FallbackModel reads the optional agents.fallbackModel override.
func FallbackModel(logger *slog.Logger) string {
	return agentRoleModel("fallbackModel", logger)
}

// CodingModel reads the optional agents.codingModel override from
// deneb.json. Empty leaves RoleCoding absent, so code-writing sub-agents and
// skill rewrites use their existing defaults.
func CodingModel(logger *slog.Logger) string {
	return agentRoleModel("codingModel", logger)
}

// Main2Model reads the optional agents.main2Model override from deneb.json —
// the second main-tier model (mutual failover pair with main). Empty leaves
// RoleMain2 absent, keeping the single-main behavior.
func Main2Model(logger *slog.Logger) string {
	return agentRoleModel("main2Model", logger)
}

// TinyModel reads the optional per-role override agents.tinyModel from
// deneb.json. Empty leaves the registry's lightweight model for that role (the
// prior single-tier behavior).
func TinyModel(logger *slog.Logger) string {
	return agentRoleModel("tinyModel", logger)
}

// VisionModel reads the optional agents.visionModel override from
// deneb.json. Empty leaves RoleVision absent, so image turns use the main
// model — separating a multimodal vision model is fully opt-in.
func VisionModel(logger *slog.Logger) string {
	return agentRoleModel("visionModel", logger)
}

// SubmainModel reads the optional agents.submainModel override from
// deneb.json — the autonomous-background lane (heartbeat, phone-event ingest).
// Empty leaves RoleSubmain absent, so those tasks stay on the main role.
func SubmainModel(logger *slog.Logger) string {
	return agentRoleModel("submainModel", logger)
}

// agentRoleModel reads a string field directly under "agents" in
// deneb.json (e.g. "lightweightModel"). Returns "" when absent/unparseable.
func agentRoleModel(field string, logger *slog.Logger) string {
	snapshot, err := config.LoadConfigFromDefaultPath()
	if err != nil || !snapshot.Valid || snapshot.Raw == "" {
		return ""
	}
	var root struct {
		Agents map[string]json.RawMessage `json:"agents"`
	}
	if err := json.Unmarshal([]byte(snapshot.Raw), &root); err != nil {
		logger.Warn("failed to parse agents config for role model", "field", field, "error", err)
		return ""
	}
	raw, ok := root.Agents[field]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return strings.TrimSpace(s)
}

// SubagentDefaultModel reads agents.defaults.subagents.model from
// deneb.json for separate sub-agent model configuration.
func SubagentDefaultModel(_ *slog.Logger) string {
	snapshot, err := config.LoadConfigFromDefaultPath()
	if err != nil || !snapshot.Valid || snapshot.Raw == "" {
		return ""
	}
	var root struct {
		Agents struct {
			Defaults json.RawMessage `json:"defaults"`
		} `json:"agents"`
	}
	if err := json.Unmarshal([]byte(snapshot.Raw), &root); err != nil {
		return ""
	}
	if len(root.Agents.Defaults) == 0 {
		return ""
	}
	var defaults struct {
		Subagents struct {
			Model json.RawMessage `json:"model"`
		} `json:"subagents"`
	}
	if err := json.Unmarshal(root.Agents.Defaults, &defaults); err != nil {
		return ""
	}
	if len(defaults.Subagents.Model) == 0 {
		return ""
	}
	// Try string first, then object with primary field.
	var s string
	if err := json.Unmarshal(defaults.Subagents.Model, &s); err == nil && s != "" {
		return s
	}
	var obj struct {
		Primary string `json:"primary"`
	}
	if err := json.Unmarshal(defaults.Subagents.Model, &obj); err == nil && obj.Primary != "" {
		return obj.Primary
	}
	return ""
}

// extractModelFromDefaults handles both string and object forms of the model field.
func extractModelFromDefaults(raw json.RawMessage) string {
	var defaults struct {
		Model json.RawMessage `json:"model"`
	}
	if err := json.Unmarshal(raw, &defaults); err != nil || len(defaults.Model) == 0 {
		return ""
	}
	// Try string first.
	var s string
	if err := json.Unmarshal(defaults.Model, &s); err == nil && s != "" {
		return s
	}
	// Try object with primary field.
	var obj struct {
		Primary string `json:"primary"`
	}
	if err := json.Unmarshal(defaults.Model, &obj); err == nil && obj.Primary != "" {
		return obj.Primary
	}
	return ""
}

// SessionThinkingDefaults reads agents.defaults.thinking from
// deneb.json and returns the values used to seed new sessions. The level
// is normalized (lowercased / "off" → ""); interleaved is forwarded as a
// pointer so callers can distinguish "unset" from "false".
//
// Returns the zero value when the config is missing, unparseable, or has
// no thinking section — equivalent to "no defaults installed".
func SessionThinkingDefaults(logger *slog.Logger) session.SessionDefaults {
	snapshot, err := config.LoadConfigFromDefaultPath()
	if err != nil || !snapshot.Valid || snapshot.Raw == "" {
		return session.SessionDefaults{}
	}
	var root struct {
		Agents struct {
			Defaults json.RawMessage `json:"defaults"`
		} `json:"agents"`
	}
	if err := json.Unmarshal([]byte(snapshot.Raw), &root); err != nil {
		logger.Warn("failed to parse agents config for thinking defaults", "error", err)
		return session.SessionDefaults{}
	}
	if len(root.Agents.Defaults) == 0 {
		return session.SessionDefaults{}
	}
	var defaults struct {
		Thinking *struct {
			Level       string `json:"level"`
			Interleaved *bool  `json:"interleaved"`
		} `json:"thinking"`
	}
	if err := json.Unmarshal(root.Agents.Defaults, &defaults); err != nil {
		logger.Warn("failed to parse agents.defaults.thinking", "error", err)
		return session.SessionDefaults{}
	}
	if defaults.Thinking == nil {
		return session.SessionDefaults{}
	}
	level := strings.ToLower(strings.TrimSpace(defaults.Thinking.Level))
	if level == "off" {
		level = ""
	}
	return session.SessionDefaults{
		ThinkingLevel:       level,
		InterleavedThinking: defaults.Thinking.Interleaved,
	}
}

// WorkspaceDir determines the workspace directory for file tool operations.
// Reads agents.defaults.workspace / agents.list[].workspace from config,
// falling back to ~/.deneb/workspace (matching TS resolveAgentWorkspaceDir).
func WorkspaceDir() string {
	snap, err := config.LoadConfigFromDefaultPath()
	if err == nil && snap != nil {
		dir := config.ResolveAgentWorkspaceDir(&snap.Config)
		if dir != "" {
			return dir
		}
	}
	// Config unavailable — fall back to built-in default.
	return config.ResolveAgentWorkspaceDir(nil)
}

// DenebDir returns the Deneb state dir (DENEB_STATE_DIR, else ~/.deneb).
// Routed through config.ResolveStateDir so a test/dev gateway with an isolated
// state dir doesn't fall back to prod ~/.deneb.
func DenebDir() string {
	return config.ResolveStateDir()
}

// TopicsDir resolves the absolute "<workspace>/<topics.dir>" directory
// holding the per-topic <key>.md knowledge files, mirroring the path logic in
// prompt.loadTopicKnowledgeFromDisk: an empty topics.dir defaults to "topics",
// a relative dir resolves against the agent workspace, an absolute dir is used
// as-is. Returns "" when topics are unconfigured so the topicdocs handler
// registers conditionally (no editor surface). Read per call (not snapshotted)
// so a config change takes effect without a restart — this is the editor path,
// not a per-turn hot path, so the prompt-cache Rule C reload concern does not
// apply here.
func TopicsDir() string {
	snap, err := config.LoadConfigFromDefaultPath()
	if err != nil || snap == nil || snap.Config.Topics == nil {
		return ""
	}
	dir := strings.TrimSpace(snap.Config.Topics.Dir)
	if dir == "" {
		dir = "topics"
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(WorkspaceDir(), dir)
	}
	return dir
}

// CurrentTopicKey resolves the topic key for the native client / the
// threadId-less default delivery, which both normalize to the "0" source ID
// (see topicResolver.TopicKey). Returns "" when topics are unconfigured or the
// map has no "0" entry — exactly the cases where no topic knowledge injects, so
// the editor stays unavailable instead of editing a file no session reads.
func CurrentTopicKey() string {
	snap, err := config.LoadConfigFromDefaultPath()
	if err != nil || snap == nil || snap.Config.Topics == nil {
		return ""
	}
	return strings.TrimSpace(snap.Config.Topics.Map["0"])
}

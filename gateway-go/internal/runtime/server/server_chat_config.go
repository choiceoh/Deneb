// Provider config loading, model/workspace resolution, and Gmail polling.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/cron"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/dropbox"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/dropboxpoll"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmailpoll"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
)

// noopGmailNotifier is a gmailpoll.Notifier that drops messages. Used in
// silent mode so the poller fills the Mini App cache + wiki (via OnAnalyzed)
// without delivering a duplicate proactive chat message. A real no-op (rather
// than a nil notifier) keeps sendNotification from logging a per-cycle warn.
type noopGmailNotifier struct{}

func (noopGmailNotifier) Notify(context.Context, string) error { return nil }

func (s *Server) initGmailPoll(snap *config.ConfigSnapshot) {
	if snap == nil {
		return
	}
	pollCfg := snap.Config.GmailPoll
	if pollCfg == nil || pollCfg.Enabled == nil || !*pollCfg.Enabled {
		return
	}

	home, _ := os.UserHomeDir()
	stateDir := filepath.Join(home, ".deneb")

	stage2, stage2Model, stage1, stage1Model := s.mailAnalysisModels()
	cfg := gmailpoll.Config{
		StateDir:      stateDir,
		LLMClient:     stage2,
		Model:         stage2Model,
		LocalClient:   stage1,
		LocalModel:    stage1Model,
		SenderFactsFn: s.wikiSenderFacts,
	}

	if pollCfg.IntervalMin != nil {
		cfg.IntervalMin = *pollCfg.IntervalMin
	}
	if pollCfg.Query != "" {
		cfg.Query = pollCfg.Query
	}
	if pollCfg.MaxPerCycle != nil {
		cfg.MaxPerCycle = *pollCfg.MaxPerCycle
	}
	if pollCfg.Model != "" {
		cfg.Model = pollCfg.Model // explicit override from config
	}
	if pollCfg.PromptFile != "" {
		cfg.PromptFile = pollCfg.PromptFile
	}

	// Wire diary dir for wiki knowledge synthesis.
	if s.wikiStore != nil && s.wikiStore.DiaryDir() != "" {
		cfg.DiaryDir = s.wikiStore.DiaryDir()
	}

	// Per-email persistence (Mini App cache + per-message wiki page with
	// related projects) and the project-candidate provider for
	// related-project selection. Both read the same wiki store the Mini App
	// uses, so a polled email shows up already-analyzed with its projects.
	cfg.OnAnalyzed = s.makeMailAnalysisSink()
	cfg.ProjectsFn = s.projectCandidatesFn()

	s.gmailPollSvc = gmailpoll.NewService(cfg, s.logger)

	// Wire proactive relay as the gmail-poll notifier so email summaries
	// are delivered verbatim AND mirrored into the main session
	// transcript — follow-ups ("방금 그 메일 답장 초안 써줘") answer in a
	// session that knows what arrived.
	//
	// All proactive output goes to the native client's 업무 chat (client:main).
	// The deliverTo config field was Telegram-target-specific and is no longer
	// consulted after Telegram bot retirement.
	//
	// Silent mode: the kakao-watch email-single-analysis cron already delivers
	// the prose analysis to chat, so a duplicate gmailpoll message is noise. A
	// no-op notifier suppresses delivery while OnAnalyzed still pre-warms the
	// Mini App analysis cache + per-message wiki page.
	if pollCfg.Silent != nil && *pollCfg.Silent {
		s.gmailPollSvc.SetNotifier(noopGmailNotifier{})
		s.logger.Info("gmailpoll: silent mode — cache/wiki pre-warm only, chat delivery suppressed")
	} else {
		s.gmailPollSvc.SetNotifier(s.proactiveRelay.notifierForSession(nativeWorkSessionKey))
	}

	// Register as a periodic task within the autonomous service.
	// The autonomous service handles lifecycle, panic recovery, and scheduling.
	if s.autonomousSvc != nil {
		s.autonomousSvc.RegisterTask(s.gmailPollSvc)
		s.logger.Info("gmailpoll registered with autonomous service",
			"interval", fmt.Sprintf("%dm", cfg.IntervalMin))
	} else {
		s.logger.Warn("gmailpoll: autonomous service not available, polling disabled")
	}
}

// seedDropboxBackupJob installs a weekly Dropbox backup cron job once (idempotent
// by name). The job runs an agent turn that invokes the dropbox tool's backup
// action. It is seeded disabled when no Dropbox token exists yet — the user
// enables it after running cmd/deneb-dropbox-auth (via the cron tool or Mini
// App) — and enabled otherwise. Respecting an existing job preserves the user's
// later schedule/enabled edits across restarts.
func (s *Server) seedDropboxBackupJob() {
	if s.cronService == nil {
		return
	}
	// Seed only once a Dropbox token exists, so the job is created enabled in the
	// same startup that first sees the token — no stale disabled job latched from
	// a token-less first boot. JobByName keeps it idempotent and respects later
	// user edits (schedule/enabled).
	if !dropbox.HasToken() {
		return
	}
	const jobName = "dropbox-backup-weekly"
	if existing, _ := s.cronService.JobByName(jobName); existing != nil {
		return
	}
	job := cron.StoreJob{
		ID:      jobName,
		Name:    jobName,
		AgentID: "main",
		Enabled: true,
		Schedule: cron.StoreSchedule{
			Kind: "cron",
			Expr: "0 3 * * 0", // weekly, Sunday 03:00
			Tz:   "Asia/Seoul",
		},
		Payload: cron.StorePayload{
			Kind:    "agentTurn",
			Message: "Dropbox에 위키·주간보고·세션기록을 백업해줘. dropbox 도구의 backup 액션(target=all)을 사용하면 된다. 조용히 처리하고 결과만 한 줄로 보고해줘.",
		},
	}
	if err := s.cronService.Add(s.ShutdownCtx(), job); err != nil {
		s.logger.Warn("dropbox backup cron seed failed", "error", err)
		return
	}
	s.logger.Info("dropbox backup cron seeded", "enabled", job.Enabled, "schedule", "weekly Sun 03:00 KST")
}

// dropboxAgentAdapter adapts chat.Handler to dropboxpoll.AgentRunner, running
// the analysis turn in an isolated "dropboxpoll" session. The agent does the
// real work via the dropbox + wiki tools; its final text is delivered to the
// 업무 chat by the watcher's notifier. AutoDeliveredOutput marks the run so an
// in-loop message-send guard failure is a benign no-op (same as cron).
type dropboxAgentAdapter struct {
	chat *chat.Handler
}

func (a *dropboxAgentAdapter) RunAgentTurn(ctx context.Context, prompt string) (string, error) {
	// system: prefix keeps this internal session out of Aurora recall and the
	// native session drawer (isSystemSession). Ephemeral flags stop the
	// fixed-key transcript from growing unbounded — the boot/heartbeat failure
	// mode where compaction eventually misses its deadline and the turn stalls.
	result, err := a.chat.SendSync(ctx, "system:dropboxpoll", prompt, "", &chat.SyncOptions{
		AutoDeliveredOutput: true,
		EphemeralUser:       true,
		EphemeralAssistant:  true,
	})
	if err != nil {
		return "", err
	}
	return result.BestText(), nil
}

// initDropboxPoll wires the Dropbox folder watcher when enabled in deneb.json.
// Mirrors initGmailPoll: detection runs here, analysis is delegated to an agent
// turn, and the summary is delivered to the native 업무 chat via the proactive
// relay (workfeed card + push).
func (s *Server) initDropboxPoll(snap *config.ConfigSnapshot) {
	if snap == nil {
		return
	}
	pollCfg := snap.Config.DropboxPoll
	if pollCfg == nil || pollCfg.Enabled == nil || !*pollCfg.Enabled {
		return
	}
	if s.chatHandler == nil || s.autonomousSvc == nil {
		s.logger.Warn("dropboxpoll: chat/autonomous unavailable, watcher disabled")
		return
	}

	home, _ := os.UserHomeDir()
	cfg := dropboxpoll.Config{
		StateDir:   filepath.Join(home, ".deneb"),
		FolderPath: "/Deneb-Inbox",
	}
	if pollCfg.FolderPath != "" {
		cfg.FolderPath = pollCfg.FolderPath
	}
	if pollCfg.IntervalMin != nil {
		cfg.IntervalMin = *pollCfg.IntervalMin
	}
	if pollCfg.MaxPerCycle != nil {
		cfg.MaxPerCycle = *pollCfg.MaxPerCycle
	}

	svc := dropboxpoll.NewService(cfg, s.logger)
	svc.SetNotifier(s.proactiveRelay.notifierForSession(nativeWorkSessionKey))
	svc.SetAgent(&dropboxAgentAdapter{chat: s.chatHandler})

	s.autonomousSvc.RegisterTask(svc)
	s.logger.Info("dropboxpoll registered with autonomous service",
		"folder", cfg.FolderPath, "interval", fmt.Sprintf("%dm", cfg.IntervalMin))
}

// registerNativeSystemMethods registers native Go system RPC methods:
// usage, logs, doctor, maintenance, update.

func loadProviderConfigs(logger *slog.Logger) map[string]chat.ProviderConfig {
	snapshot, err := config.LoadConfigFromDefaultPath()
	if err != nil || !snapshot.Valid || snapshot.Raw == "" {
		return nil
	}

	var root struct {
		Models struct {
			Providers map[string]chat.ProviderConfig `json:"providers"`
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

// providerCatalog converts the deneb.json models.providers entries into the
// modelrole registry's dependency-free ProviderResolved shape, so a per-role
// model can target ANY configured provider (e.g. "google/...") instead of
// falling back to modelrole's built-in provider switch.
func providerCatalog(logger *slog.Logger) map[string]modelrole.ProviderResolved {
	raw := loadProviderConfigs(logger)
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string]modelrole.ProviderResolved, len(raw))
	for id, p := range raw {
		out[id] = modelrole.ProviderResolved{
			BaseURL:       p.BaseURL,
			APIKey:        p.APIKey,
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

// convertRoutingConfig maps the deneb.json routing block (toolctx JSON shape)
// to the registry's dependency-free RoutingOverride. The two structs are
// field-identical pointer bags; the split mirrors the existing
// ProviderConfig/ProviderResolved boundary so modelrole stays free of the
// config package. Nil in, nil out.
func convertRoutingConfig(r *chat.RoutingConfig) *modelrole.RoutingOverride {
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

// resolveDefaultModel reads agents.defaultModel or agents.defaults.model from
// deneb.json, falling back to the registry's main model default.
// The model field can be either a string ("model-name") or an object
// ({"primary": "model-name", "fallbacks": [...]}).
func resolveDefaultModel(logger *slog.Logger) string {
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

// resolveLocalVllmModel reads models.providers.vllm.models[0].id from deneb.json
// to determine the model name the local vLLM server is serving. Returns empty
// string if unconfigured — NewRegistry will fall back to the const default.
func resolveLocalVllmModel(_ *slog.Logger) string {
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
// and the interactive miniapp gmail.analyze factory (method_registry.go).
// Stage-2 synthesis is reasoning-grade → analysis role; stage-1 extractors
// are trivial classification → tiny role. Keeping the choice in ONE place
// prevents the two paths drifting apart: the #2045 tiny/analysis upgrade
// reached only the poller, and the miniapp button stayed pinned to the
// fallback role until that provider's key died (401, 2026-06-10).
func (s *Server) mailAnalysisModels() (stage2 *llm.Client, stage2Model string, stage1 *llm.Client, stage1Model string) {
	if s.modelRegistry == nil {
		return nil, "", nil, ""
	}
	return s.modelRegistry.Client(modelrole.RoleAnalysis),
		s.modelRegistry.Model(modelrole.RoleAnalysis),
		s.modelRegistry.Client(modelrole.RoleTiny),
		s.modelRegistry.Model(modelrole.RoleTiny)
}

// resolveLightweightModel / resolveFallbackModel read the optional per-role
// overrides agents.lightweightModel / agents.fallbackModel from deneb.json.
// Empty leaves the registry's built-in local vLLM default for that role.
func resolveLightweightModel(logger *slog.Logger) string {
	return resolveAgentRoleModel("lightweightModel", logger)
}

func resolveFallbackModel(logger *slog.Logger) string {
	return resolveAgentRoleModel("fallbackModel", logger)
}

// resolveTinyModel / resolveAnalysisModel read the optional per-role overrides
// agents.tinyModel / agents.analysisModel from deneb.json. Empty leaves the
// registry's lightweight model for that role (the prior single-tier behavior).
func resolveTinyModel(logger *slog.Logger) string {
	return resolveAgentRoleModel("tinyModel", logger)
}

func resolveAnalysisModel(logger *slog.Logger) string {
	return resolveAgentRoleModel("analysisModel", logger)
}

// resolveChatbotModel reads the optional agents.chatbotModel override from
// deneb.json. Empty leaves RoleChatbot absent, so 챗봇(chat:) turns use the
// main model — separating a chatbot model is fully opt-in.
func resolveChatbotModel(logger *slog.Logger) string {
	return resolveAgentRoleModel("chatbotModel", logger)
}

// resolveVisionModel reads the optional agents.visionModel override from
// deneb.json. Empty leaves RoleVision absent, so image turns use the main
// model — separating a multimodal vision model is fully opt-in.
func resolveVisionModel(logger *slog.Logger) string {
	return resolveAgentRoleModel("visionModel", logger)
}

// resolveAgentRoleModel reads a string field directly under "agents" in
// deneb.json (e.g. "lightweightModel"). Returns "" when absent/unparseable.
func resolveAgentRoleModel(field string, logger *slog.Logger) string {
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

// resolveSubagentDefaultModel reads agents.defaults.subagents.model from
// deneb.json for separate sub-agent model configuration.
func resolveSubagentDefaultModel(_ *slog.Logger) string {
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

// resolveSessionThinkingDefaults reads agents.defaults.thinking from
// deneb.json and returns the values used to seed new sessions. The level
// is normalized (lowercased / "off" → ""); interleaved is forwarded as a
// pointer so callers can distinguish "unset" from "false".
//
// Returns the zero value when the config is missing, unparseable, or has
// no thinking section — equivalent to "no defaults installed".
func resolveSessionThinkingDefaults(logger *slog.Logger) session.SessionDefaults {
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

// resolveWorkspaceDir determines the workspace directory for file tool operations.
// Reads agents.defaults.workspace / agents.list[].workspace from config,
// falling back to ~/.deneb/workspace (matching TS resolveAgentWorkspaceDir).
func resolveWorkspaceDir() string {
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

// resolveDenebDir returns the path to ~/.deneb.
func resolveDenebDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".deneb")
	}
	return "/tmp/deneb"
}

// Provider config loading, model/workspace resolution, and Gmail polling.
package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/choiceoh/deneb/gateway-go/internal/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/config"
	"github.com/choiceoh/deneb/gateway-go/internal/gmailpoll"
	"github.com/choiceoh/deneb/gateway-go/internal/modelrole"
)

func (s *Server) initGmailPoll() {
	snap, err := config.LoadConfigFromDefaultPath()
	if err != nil || snap == nil {
		return
	}
	pollCfg := snap.Config.GmailPoll
	if pollCfg == nil || pollCfg.Enabled == nil || !*pollCfg.Enabled {
		return
	}

	home, _ := os.UserHomeDir()
	stateDir := filepath.Join(home, ".deneb")

	cfg := gmailpoll.Config{
		StateDir:    stateDir,
		LLMClient:   s.modelRegistry.Client(modelrole.RoleLightweight),
		Model:       s.modelRegistry.Model(modelrole.RoleLightweight),
		LocalClient: s.modelRegistry.Client(modelrole.RoleLightweight),
		LocalModel:  s.modelRegistry.Model(modelrole.RoleLightweight),
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

	// Wire diary dir for RLM knowledge synthesis.
	if s.wikiStore != nil && s.wikiStore.DiaryDir() != "" {
		cfg.DiaryDir = s.wikiStore.DiaryDir()
	}

	s.gmailPollSvc = gmailpoll.NewService(cfg, s.logger)

	// Wire Telegram notifier.
	if s.telegramPlug != nil {
		tgCfg := s.telegramPlug.Config()
		if tgCfg != nil && len(tgCfg.AllowFrom.IDs) > 0 {
			s.gmailPollSvc.SetNotifier(&telegramNotifier{
				plugin: s.telegramPlug,
				chatID: tgCfg.AllowFrom.IDs[0],
				logger: s.logger,
			})
		}
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

// resolveSubagentDefaultModel reads agents.defaults.subagents.model from
// deneb.json for separate sub-agent model configuration.
func resolveSubagentDefaultModel(logger *slog.Logger) string {
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

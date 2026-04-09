// Package skill provides RPC handlers for skills.*, plugins.*, tools.*, and
// tools.catalog methods. Migrated from the flat internal/rpc package into a
// domain-based handler subpackage.
package skill

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/internal/skills"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// broadcast calls fn only if it is non-nil — avoids a nil check at every call site.
func broadcast(fn BroadcastFunc, event string, payload any) {
	if fn != nil {
		fn(event, payload)
	}
}

// BroadcastFunc is the canonical broadcast type defined in rpcutil.
type BroadcastFunc = rpcutil.BroadcastFunc

// ---------------------------------------------------------------------------
// Deps — skills.* handlers
// ---------------------------------------------------------------------------

// Deps holds the dependencies for skills.* RPC methods.
type Deps struct {
	Skills      *skills.Registry
	Broadcaster BroadcastFunc
}

// Methods returns all skills.* RPC handler methods.
func Methods(deps Deps) map[string]rpcutil.HandlerFunc {
	if deps.Skills == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"skills.status":           skillsStatus(deps),
		"skills.bins":             skillsBins(deps),
		"skills.install":          skillsInstall(deps),
		"skills.update":           skillsUpdate(deps),
		"skills.snapshot":         skillsSnapshot(deps),
		"skills.commands":         skillsCommands(deps),
		"skills.discover":         skillsDiscover(deps),
		"skills.workspace_status": skillsWorkspaceStatus(deps),
		"skills.entries":          skillsEntries(deps),
	}
}

func skillsStatus(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			AgentID string `json:"agentId,omitempty"`
		}](req)
		if errResp != nil {
			return errResp
		}

		status := deps.Skills.GetStatus(p.AgentID)
		return rpcutil.RespondOK(req.ID, status)
	}
}

func skillsBins(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		bins := deps.Skills.ListBins()
		if bins == nil {
			bins = make([]string, 0)
		}
		return rpcutil.RespondOK(req.ID, map[string]any{"bins": bins})
	}
}

func skillsInstall(deps Deps) rpcutil.HandlerFunc {
	type params struct {
		Name      string `json:"name"`
		InstallID string `json:"installId"`
		TimeoutMs int64  `json:"timeoutMs,omitempty"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		if p.Name == "" || p.InstallID == "" {
			return nil, rpcerr.MissingParam("name and installId")
		}
		result := deps.Skills.Install(p.Name, p.InstallID)
		broadcast(deps.Broadcaster, "skills.changed", map[string]any{
			"action": "installed",
			"name":   p.Name,
		})
		return result, nil
	})
}

func skillsUpdate(deps Deps) rpcutil.HandlerFunc {
	type params struct {
		SkillKey string            `json:"skillKey"`
		Enabled  *bool             `json:"enabled,omitempty"`
		APIKey   string            `json:"apiKey,omitempty"`
		Env      map[string]string `json:"env,omitempty"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		if p.SkillKey == "" {
			return nil, rpcerr.MissingParam("skillKey")
		}
		updated, err := deps.Skills.Update(p.SkillKey, skills.ConfigPatch{
			Enabled: p.Enabled,
			APIKey:  p.APIKey,
			Env:     p.Env,
		})
		if err != nil {
			return nil, rpcerr.NotFound(err.Error())
		}
		broadcast(deps.Broadcaster, "skills.changed", map[string]any{
			"action":   "updated",
			"skillKey": p.SkillKey,
		})
		return map[string]any{
			"ok":       true,
			"skillKey": p.SkillKey,
			"config":   updated.Config,
		}, nil
	})
}

// skillsSnapshot returns a full skill snapshot (prompt + metadata + version)
// for a workspace. This is the primary endpoint used by TypeScript consumers.
func skillsSnapshot(_ Deps) rpcutil.HandlerFunc {
	type params struct {
		WorkspaceDir     string                        `json:"workspaceDir"`
		BundledSkillsDir string                        `json:"bundledSkillsDir,omitempty"`
		ManagedSkillsDir string                        `json:"managedSkillsDir,omitempty"`
		ExtraDirs        []string                      `json:"extraDirs,omitempty"`
		PluginSkillDirs  []string                      `json:"pluginSkillDirs,omitempty"`
		SkillFilter      []string                      `json:"skillFilter,omitempty"`
		SkillConfigs     map[string]skills.SkillConfig `json:"skillConfigs,omitempty"`
		AllowBundled     []string                      `json:"allowBundled,omitempty"`
		ConfigValues     map[string]bool               `json:"configValues,omitempty"`
		EnvVars          map[string]string             `json:"envVars,omitempty"`
		RemoteNote       string                        `json:"remoteNote,omitempty"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		if p.WorkspaceDir == "" {
			return nil, rpcerr.MissingParam("workspaceDir")
		}
		eligCtx := skills.DefaultEligibilityContext()
		if p.SkillConfigs != nil {
			eligCtx.SkillConfigs = p.SkillConfigs
		}
		if p.AllowBundled != nil {
			eligCtx.AllowBundled = p.AllowBundled
		}
		if p.ConfigValues != nil {
			eligCtx.ConfigValues = p.ConfigValues
		}
		if p.EnvVars != nil {
			eligCtx.EnvVars = p.EnvVars
		}
		snapshot := skills.BuildWorkspaceSkillSnapshot(skills.SnapshotConfig{
			DiscoverConfig: skills.DiscoverConfig{
				WorkspaceDir:     p.WorkspaceDir,
				BundledSkillsDir: p.BundledSkillsDir,
				ManagedSkillsDir: p.ManagedSkillsDir,
				ExtraDirs:        p.ExtraDirs,
				PluginSkillDirs:  p.PluginSkillDirs,
			},
			SkillFilter: p.SkillFilter,
			Eligibility: eligCtx,
			RemoteNote:  p.RemoteNote,
		})
		return snapshot, nil
	})
}

// skillsCommands returns slash command specs derived from eligible skills.
func skillsCommands(_ Deps) rpcutil.HandlerFunc {
	type params struct {
		WorkspaceDir     string                        `json:"workspaceDir"`
		BundledSkillsDir string                        `json:"bundledSkillsDir,omitempty"`
		ExtraDirs        []string                      `json:"extraDirs,omitempty"`
		PluginSkillDirs  []string                      `json:"pluginSkillDirs,omitempty"`
		SkillFilter      []string                      `json:"skillFilter,omitempty"`
		SkillConfigs     map[string]skills.SkillConfig `json:"skillConfigs,omitempty"`
		AllowBundled     []string                      `json:"allowBundled,omitempty"`
		ReservedNames    []string                      `json:"reservedNames,omitempty"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		if p.WorkspaceDir == "" {
			return nil, rpcerr.MissingParam("workspaceDir")
		}
		entries := skills.DiscoverWorkspaceSkills(skills.DiscoverConfig{
			WorkspaceDir:     p.WorkspaceDir,
			BundledSkillsDir: p.BundledSkillsDir,
			ExtraDirs:        p.ExtraDirs,
			PluginSkillDirs:  p.PluginSkillDirs,
		})
		eligCtx := skills.DefaultEligibilityContext()
		if p.SkillConfigs != nil {
			eligCtx.SkillConfigs = p.SkillConfigs
		}
		if p.AllowBundled != nil {
			eligCtx.AllowBundled = p.AllowBundled
		}
		eligible := skills.FilterEligibleSkills(entries, eligCtx)
		eligible = skills.FilterBySkillFilter(eligible, p.SkillFilter)
		reserved := make(map[string]bool)
		for _, name := range p.ReservedNames {
			reserved[name] = true
		}
		return map[string]any{"commands": skills.BuildSkillCommandSpecs(eligible, reserved)}, nil
	})
}

// skillsDiscover triggers skill re-discovery and returns counts.
func skillsDiscover(deps Deps) rpcutil.HandlerFunc {
	type params struct {
		WorkspaceDir     string   `json:"workspaceDir"`
		BundledSkillsDir string   `json:"bundledSkillsDir,omitempty"`
		ExtraDirs        []string `json:"extraDirs,omitempty"`
		PluginSkillDirs  []string `json:"pluginSkillDirs,omitempty"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		if p.WorkspaceDir == "" {
			return nil, rpcerr.MissingParam("workspaceDir")
		}
		entries := skills.DiscoverWorkspaceSkills(skills.DiscoverConfig{
			WorkspaceDir:     p.WorkspaceDir,
			BundledSkillsDir: p.BundledSkillsDir,
			ExtraDirs:        p.ExtraDirs,
			PluginSkillDirs:  p.PluginSkillDirs,
		})
		broadcast(deps.Broadcaster, "skills.changed", map[string]any{
			"action": "discovered",
			"count":  len(entries),
		})
		return map[string]any{"ok": true, "count": len(entries)}, nil
	})
}

// skillsEntries returns the full discovered skill entries for a workspace.
// Used by TS consumers that need the raw SkillEntry objects (e.g., skills-status, skills-install).
func skillsEntries(_ Deps) rpcutil.HandlerFunc {
	type params struct {
		WorkspaceDir     string                        `json:"workspaceDir"`
		BundledSkillsDir string                        `json:"bundledSkillsDir,omitempty"`
		ManagedSkillsDir string                        `json:"managedSkillsDir,omitempty"`
		ExtraDirs        []string                      `json:"extraDirs,omitempty"`
		PluginSkillDirs  []string                      `json:"pluginSkillDirs,omitempty"`
		SkillConfigs     map[string]skills.SkillConfig `json:"skillConfigs,omitempty"`
		AllowBundled     []string                      `json:"allowBundled,omitempty"`
		SkillFilter      []string                      `json:"skillFilter,omitempty"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		if p.WorkspaceDir == "" {
			return nil, rpcerr.MissingParam("workspaceDir")
		}
		entries := skills.DiscoverWorkspaceSkills(skills.DiscoverConfig{
			WorkspaceDir:     p.WorkspaceDir,
			BundledSkillsDir: p.BundledSkillsDir,
			ManagedSkillsDir: p.ManagedSkillsDir,
			ExtraDirs:        p.ExtraDirs,
			PluginSkillDirs:  p.PluginSkillDirs,
		})
		// Optionally filter by eligibility.
		if p.SkillConfigs != nil || p.AllowBundled != nil {
			ctx := skills.DefaultEligibilityContext()
			if p.SkillConfigs != nil {
				ctx.SkillConfigs = p.SkillConfigs
			}
			if p.AllowBundled != nil {
				ctx.AllowBundled = p.AllowBundled
			}
			entries = skills.FilterEligibleSkills(entries, ctx)
		}
		entries = skills.FilterBySkillFilter(entries, p.SkillFilter)
		return map[string]any{"entries": entries}, nil
	})
}

// skillsWorkspaceStatus returns a full skill status report for a workspace.
func skillsWorkspaceStatus(_ Deps) rpcutil.HandlerFunc {
	type params struct {
		WorkspaceDir     string                        `json:"workspaceDir"`
		BundledSkillsDir string                        `json:"bundledSkillsDir,omitempty"`
		ExtraDirs        []string                      `json:"extraDirs,omitempty"`
		SkillConfigs     map[string]skills.SkillConfig `json:"skillConfigs,omitempty"`
		AllowBundled     []string                      `json:"allowBundled,omitempty"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		if p.WorkspaceDir == "" {
			return nil, rpcerr.MissingParam("workspaceDir")
		}
		eligCtx := skills.DefaultEligibilityContext()
		if p.SkillConfigs != nil {
			eligCtx.SkillConfigs = p.SkillConfigs
		}
		if p.AllowBundled != nil {
			eligCtx.AllowBundled = p.AllowBundled
		}
		return skills.BuildWorkspaceSkillStatus(
			skills.DiscoverConfig{
				WorkspaceDir:     p.WorkspaceDir,
				BundledSkillsDir: p.BundledSkillsDir,
				ExtraDirs:        p.ExtraDirs,
			},
			eligCtx,
		), nil
	})
}

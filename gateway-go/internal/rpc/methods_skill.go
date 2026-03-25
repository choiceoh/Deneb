package rpc

import (
	"context"
	"encoding/json"

	"github.com/choiceoh/deneb/gateway-go/internal/skill"
	"github.com/choiceoh/deneb/gateway-go/internal/skills"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// SkillDeps holds the dependencies for skill RPC methods.
type SkillDeps struct {
	Skills      *skill.Manager
	Broadcaster BroadcastFunc
}

// RegisterSkillMethods registers all skills.* RPC methods.
func RegisterSkillMethods(d *Dispatcher, deps SkillDeps) {
	if deps.Skills == nil {
		return
	}

	d.Register("skills.status", skillsStatus(deps))
	d.Register("skills.bins", skillsBins(deps))
	d.Register("skills.install", skillsInstall(deps))
	d.Register("skills.update", skillsUpdate(deps))
	d.Register("skills.snapshot", skillsSnapshot(deps))
	d.Register("skills.commands", skillsCommands(deps))
	d.Register("skills.discover", skillsDiscover(deps))
	d.Register("skills.workspace_status", skillsWorkspaceStatus(deps))
}

func skillsStatus(deps SkillDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			AgentID string `json:"agentId,omitempty"`
		}
		_ = json.Unmarshal(req.Params, &p)

		status := deps.Skills.GetStatus(p.AgentID)
		resp := protocol.MustResponseOK(req.ID, status)
		return resp
	}
}

func skillsBins(deps SkillDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		bins := deps.Skills.ListBins()
		if bins == nil {
			bins = make([]string, 0)
		}
		resp := protocol.MustResponseOK(req.ID, map[string]any{"bins": bins})
		return resp
	}
}

func skillsInstall(deps SkillDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Name      string `json:"name"`
			InstallID string `json:"installId"`
			TimeoutMs int64  `json:"timeoutMs,omitempty"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params: "+err.Error()))
		}
		if p.Name == "" || p.InstallID == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "name and installId are required"))
		}

		result := deps.Skills.Install(p.Name, p.InstallID)

		if deps.Broadcaster != nil {
			deps.Broadcaster("skills.changed", map[string]any{
				"action": "installed",
				"name":   p.Name,
			})
		}

		resp := protocol.MustResponseOK(req.ID, result)
		return resp
	}
}

func skillsUpdate(deps SkillDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			SkillKey string            `json:"skillKey"`
			Enabled  *bool             `json:"enabled,omitempty"`
			APIKey   string            `json:"apiKey,omitempty"`
			Env      map[string]string `json:"env,omitempty"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params: "+err.Error()))
		}
		if p.SkillKey == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "skillKey is required"))
		}

		updated, err := deps.Skills.Update(p.SkillKey, skill.SkillPatch{
			Enabled: p.Enabled,
			APIKey:  p.APIKey,
			Env:     p.Env,
		})
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrNotFound, err.Error()))
		}

		if deps.Broadcaster != nil {
			deps.Broadcaster("skills.changed", map[string]any{
				"action":   "updated",
				"skillKey": p.SkillKey,
			})
		}

		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"ok":       true,
			"skillKey": p.SkillKey,
			"config":   updated.Config,
		})
		return resp
	}
}

// skillsSnapshot returns a full skill snapshot (prompt + metadata + version)
// for a workspace. This is the primary endpoint used by TypeScript consumers.
func skillsSnapshot(_ SkillDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			WorkspaceDir     string            `json:"workspaceDir"`
			BundledSkillsDir string            `json:"bundledSkillsDir,omitempty"`
			ManagedSkillsDir string            `json:"managedSkillsDir,omitempty"`
			ExtraDirs        []string          `json:"extraDirs,omitempty"`
			PluginSkillDirs  []string          `json:"pluginSkillDirs,omitempty"`
			SkillFilter      []string          `json:"skillFilter,omitempty"`
			SkillConfigs     map[string]skills.SkillConfig `json:"skillConfigs,omitempty"`
			AllowBundled     []string          `json:"allowBundled,omitempty"`
			ConfigValues     map[string]bool   `json:"configValues,omitempty"`
			EnvVars          map[string]string `json:"envVars,omitempty"`
			RemoteNote       string            `json:"remoteNote,omitempty"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params: "+err.Error()))
		}
		if p.WorkspaceDir == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "workspaceDir is required"))
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

		return protocol.MustResponseOK(req.ID, snapshot)
	}
}

// skillsCommands returns slash command specs derived from eligible skills.
func skillsCommands(_ SkillDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			WorkspaceDir     string            `json:"workspaceDir"`
			BundledSkillsDir string            `json:"bundledSkillsDir,omitempty"`
			ExtraDirs        []string          `json:"extraDirs,omitempty"`
			PluginSkillDirs  []string          `json:"pluginSkillDirs,omitempty"`
			SkillFilter      []string          `json:"skillFilter,omitempty"`
			SkillConfigs     map[string]skills.SkillConfig `json:"skillConfigs,omitempty"`
			AllowBundled     []string          `json:"allowBundled,omitempty"`
			ReservedNames    []string          `json:"reservedNames,omitempty"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params: "+err.Error()))
		}
		if p.WorkspaceDir == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "workspaceDir is required"))
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
		specs := skills.BuildSkillCommandSpecs(eligible, reserved)

		return protocol.MustResponseOK(req.ID, map[string]any{
			"commands": specs,
		})
	}
}

// skillsDiscover triggers skill re-discovery and returns counts.
func skillsDiscover(deps SkillDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			WorkspaceDir     string   `json:"workspaceDir"`
			BundledSkillsDir string   `json:"bundledSkillsDir,omitempty"`
			ExtraDirs        []string `json:"extraDirs,omitempty"`
			PluginSkillDirs  []string `json:"pluginSkillDirs,omitempty"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params: "+err.Error()))
		}
		if p.WorkspaceDir == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "workspaceDir is required"))
		}

		entries := skills.DiscoverWorkspaceSkills(skills.DiscoverConfig{
			WorkspaceDir:     p.WorkspaceDir,
			BundledSkillsDir: p.BundledSkillsDir,
			ExtraDirs:        p.ExtraDirs,
			PluginSkillDirs:  p.PluginSkillDirs,
		})

		if deps.Broadcaster != nil {
			deps.Broadcaster("skills.changed", map[string]any{
				"action": "discovered",
				"count":  len(entries),
			})
		}

		return protocol.MustResponseOK(req.ID, map[string]any{
			"ok":    true,
			"count": len(entries),
		})
	}
}

// skillsWorkspaceStatus returns a full skill status report for a workspace.
func skillsWorkspaceStatus(_ SkillDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			WorkspaceDir     string            `json:"workspaceDir"`
			BundledSkillsDir string            `json:"bundledSkillsDir,omitempty"`
			ExtraDirs        []string          `json:"extraDirs,omitempty"`
			SkillConfigs     map[string]skills.SkillConfig `json:"skillConfigs,omitempty"`
			AllowBundled     []string          `json:"allowBundled,omitempty"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params: "+err.Error()))
		}
		if p.WorkspaceDir == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "workspaceDir is required"))
		}

		eligCtx := skills.DefaultEligibilityContext()
		if p.SkillConfigs != nil {
			eligCtx.SkillConfigs = p.SkillConfigs
		}
		if p.AllowBundled != nil {
			eligCtx.AllowBundled = p.AllowBundled
		}

		status := skills.BuildWorkspaceSkillStatus(
			skills.DiscoverConfig{
				WorkspaceDir:     p.WorkspaceDir,
				BundledSkillsDir: p.BundledSkillsDir,
				ExtraDirs:        p.ExtraDirs,
			},
			eligCtx,
		)

		return protocol.MustResponseOK(req.ID, status)
	}
}

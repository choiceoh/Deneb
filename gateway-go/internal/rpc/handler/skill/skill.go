// Package skill provides RPC handlers for skills.*, plugins.*, tools.*, and
// tools.catalog methods. Migrated from the flat internal/rpc package into a
// domain-based handler subpackage.
package skill

import (
	"context"
	"encoding/json"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/process"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	skillpkg "github.com/choiceoh/deneb/gateway-go/internal/skill"
	"github.com/choiceoh/deneb/gateway-go/internal/skills"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// broadcast calls fn only if it is non-nil — avoids a nil check at every call site.
func broadcast(fn BroadcastFunc, event string, payload any) {
	if fn != nil {
		fn(event, payload)
	}
}

// BroadcastFunc broadcasts an event to subscribers.
type BroadcastFunc func(event string, payload any) (int, []error)

// ---------------------------------------------------------------------------
// Deps — skills.* handlers
// ---------------------------------------------------------------------------

// Deps holds the dependencies for skills.* RPC methods.
type Deps struct {
	Skills      *skillpkg.Manager
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
		var p struct {
			AgentID string `json:"agentId,omitempty"`
		}
		_ = json.Unmarshal(req.Params, &p)

		status := deps.Skills.GetStatus(p.AgentID)
		resp := protocol.MustResponseOK(req.ID, status)
		return resp
	}
}

func skillsBins(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		bins := deps.Skills.ListBins()
		if bins == nil {
			bins = make([]string, 0)
		}
		resp := protocol.MustResponseOK(req.ID, map[string]any{"bins": bins})
		return resp
	}
}

func skillsInstall(deps Deps) rpcutil.HandlerFunc {
	type params struct {
		Name      string `json:"name"`
		InstallID string `json:"installId"`
		TimeoutMs int64  `json:"timeoutMs,omitempty"`
	}
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return rpcutil.Bind[params](req, func(p params) (any, error) {
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
}

func skillsUpdate(deps Deps) rpcutil.HandlerFunc {
	type params struct {
		SkillKey string            `json:"skillKey"`
		Enabled  *bool             `json:"enabled,omitempty"`
		APIKey   string            `json:"apiKey,omitempty"`
		Env      map[string]string `json:"env,omitempty"`
	}
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return rpcutil.Bind[params](req, func(p params) (any, error) {
			if p.SkillKey == "" {
				return nil, rpcerr.MissingParam("skillKey")
			}
			updated, err := deps.Skills.Update(p.SkillKey, skillpkg.SkillPatch{
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
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return rpcutil.Bind[params](req, func(p params) (any, error) {
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
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return rpcutil.Bind[params](req, func(p params) (any, error) {
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
}

// skillsDiscover triggers skill re-discovery and returns counts.
func skillsDiscover(deps Deps) rpcutil.HandlerFunc {
	type params struct {
		WorkspaceDir     string   `json:"workspaceDir"`
		BundledSkillsDir string   `json:"bundledSkillsDir,omitempty"`
		ExtraDirs        []string `json:"extraDirs,omitempty"`
		PluginSkillDirs  []string `json:"pluginSkillDirs,omitempty"`
	}
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return rpcutil.Bind[params](req, func(p params) (any, error) {
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
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return rpcutil.Bind[params](req, func(p params) (any, error) {
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
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return rpcutil.Bind[params](req, func(p params) (any, error) {
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
}

// ---------------------------------------------------------------------------
// PluginDeps — plugins.* handlers
// ---------------------------------------------------------------------------

// PluginRegistry is the interface for querying registered plugins.
// This decouples the RPC layer from the concrete plugin manager.
type PluginRegistry interface {
	ListPlugins() []protocol.PluginMeta
	GetPluginHealth(id string) *protocol.PluginHealthStatus
}

// PluginDeps holds the dependencies for plugin RPC methods.
type PluginDeps struct {
	PluginRegistry PluginRegistry
}

// PluginMethods returns all plugins.* RPC handler methods.
func PluginMethods(deps PluginDeps) map[string]rpcutil.HandlerFunc {
	if deps.PluginRegistry == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"plugins.list":     pluginsList(deps),
		"plugins.snapshot": pluginsSnapshot(deps),
	}
}

func pluginsList(deps PluginDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		plugins := deps.PluginRegistry.ListPlugins()
		resp := protocol.MustResponseOK(req.ID, plugins)
		return resp
	}
}

func pluginsSnapshot(deps PluginDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		plugins := deps.PluginRegistry.ListPlugins()
		health := make([]protocol.PluginHealthStatus, 0, len(plugins))
		for _, p := range plugins {
			if h := deps.PluginRegistry.GetPluginHealth(p.ID); h != nil {
				health = append(health, *h)
			}
		}
		snapshot := protocol.PluginRegistrySnapshot{
			Plugins:    plugins,
			Health:     health,
			SnapshotAt: time.Now().UnixMilli(),
		}
		resp := protocol.MustResponseOK(req.ID, snapshot)
		return resp
	}
}

// ---------------------------------------------------------------------------
// ToolDeps — tools.* handlers
// ---------------------------------------------------------------------------

// ToolDeps holds dependencies for tool invocation RPC methods.
type ToolDeps struct {
	Processes *process.Manager
}

// ToolMethods returns all tools.* RPC handler methods (invoke, list, status).
func ToolMethods(deps ToolDeps) map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"tools.invoke": toolsInvoke(deps),
		"tools.list":   toolsList(deps),
		"tools.status": toolsStatus(deps),
	}
}

// toolsInvoke handles "tools.invoke" — executes a tool by name.
// Native execution via the process manager is supported for bash/exec tools.
// Uses the manual unmarshal pattern because toolsExecLocal needs both ctx and req.
func toolsInvoke(deps ToolDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Tool       string         `json:"tool"`
			Action     string         `json:"action,omitempty"`
			Args       map[string]any `json:"args,omitempty"`
			SessionKey string         `json:"sessionKey,omitempty"`
			DryRun     bool           `json:"dryRun,omitempty"`
		}
		if err := rpcutil.UnmarshalParams(req.Params, &p); err != nil {
			return rpcerr.InvalidParams(err).Response(req.ID)
		}
		if p.Tool == "" {
			return rpcerr.MissingParam("tool").Response(req.ID)
		}

		// For bash/exec tools, execute locally via process manager.
		if (p.Tool == "bash" || p.Tool == "exec") && deps.Processes != nil {
			return toolsExecLocal(ctx, req, deps, p.Tool, p.Args, p.DryRun)
		}

		// Non-bash/exec tools are not available in standalone Go gateway.
		return rpcerr.Unavailable("tool " + p.Tool + " not available in standalone mode").Response(req.ID)
	}
}

// toolsExecLocal executes a bash/exec tool locally using the process manager.
func toolsExecLocal(ctx context.Context, req *protocol.RequestFrame, deps ToolDeps, tool string, args map[string]any, dryRun bool) *protocol.ResponseFrame {
	if dryRun {
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"tool":   tool,
			"dryRun": true,
			"args":   args,
		})
		return resp
	}

	command, _ := args["command"].(string)
	if command == "" {
		return rpcerr.MissingParam("command for " + tool + " tool").Response(req.ID)
	}

	var execArgs []string
	if tool == "bash" {
		execArgs = []string{"-c", command}
		command = "bash"
	}

	timeoutMs := int64(30000)
	if t, ok := args["timeoutMs"].(float64); ok && t > 0 {
		timeoutMs = int64(t)
		// Cap at 5 minutes to prevent unbounded execution.
		const maxTimeoutMs = int64(5 * 60 * 1000)
		if timeoutMs > maxTimeoutMs {
			timeoutMs = maxTimeoutMs
		}
	}

	workDir, _ := args["workingDir"].(string)

	result := deps.Processes.Execute(ctx, process.ExecRequest{
		Command:    command,
		Args:       execArgs,
		WorkingDir: workDir,
		TimeoutMs:  timeoutMs,
	})

	resp := protocol.MustResponseOK(req.ID, result)
	return resp
}

// toolsList handles "tools.list" — returns the available tool catalog.
// Enumerates core tools from the static catalog (same source as tools.catalog).
func toolsList(_ ToolDeps) rpcutil.HandlerFunc {
	// Pre-compute the flat tool list at registration time.
	groups := buildCoreToolCatalog()
	tools := make([]map[string]any, 0, 24)
	for _, g := range groups {
		for _, t := range g.Tools {
			tools = append(tools, map[string]any{
				"id":          t.ID,
				"label":       t.Label,
				"description": t.Description,
				"source":      t.Source,
				"group":       g.ID,
			})
		}
	}

	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		resp := protocol.MustResponseOK(req.ID, map[string]any{
			"tools": tools,
		})
		return resp
	}
}

// toolsStatus handles "tools.status" — returns status of a running tool execution.
func toolsStatus(deps ToolDeps) rpcutil.HandlerFunc {
	type params struct {
		ID string `json:"id"`
	}
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		return rpcutil.Bind[params](req, func(p params) (any, error) {
			if p.ID == "" {
				return nil, rpcerr.MissingParam("id")
			}
			if deps.Processes == nil {
				return nil, rpcerr.NotFound("process tracking")
			}
			tracked := deps.Processes.Get(p.ID)
			if tracked == nil {
				return nil, rpcerr.NotFound("tool execution")
			}
			return tracked, nil
		})
	}
}

// ---------------------------------------------------------------------------
// CatalogMethods — tools.catalog (static core tool catalog)
// ---------------------------------------------------------------------------

// ToolProfileID identifies a tool access profile.
type ToolProfileID = string

const (
	ProfileMinimal   ToolProfileID = "minimal"
	ProfileCoding    ToolProfileID = "coding"
	ProfileMessaging ToolProfileID = "messaging"
	ProfileFull      ToolProfileID = "full"
)

// ToolCatalogEntry describes a single tool in the catalog.
type ToolCatalogEntry struct {
	ID              string          `json:"id"`
	Label           string          `json:"label"`
	Description     string          `json:"description"`
	Source          string          `json:"source"`
	DefaultProfiles []ToolProfileID `json:"defaultProfiles"`
}

// ToolCatalogGroup describes a section of related tools.
type ToolCatalogGroup struct {
	ID     string             `json:"id"`
	Label  string             `json:"label"`
	Source string             `json:"source"`
	Tools  []ToolCatalogEntry `json:"tools"`
}

// profileOption is a display-friendly profile descriptor.
type profileOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

var catalogProfileOptions = []profileOption{
	{ID: ProfileMinimal, Label: "Minimal"},
	{ID: ProfileCoding, Label: "Coding"},
	{ID: ProfileMessaging, Label: "Messaging"},
	{ID: ProfileFull, Label: "Full"},
}

// coreTool is a compact definition; Source is always "core" and Label == ID.
type coreTool struct {
	ID          string
	Description string
	Profiles    []ToolProfileID
}

// coreSection defines a tool group before expansion.
type coreSection struct {
	ID    string
	Label string
	Tools []coreTool
}

// coreSections mirrors the 22 core tools from src/agents/tool-catalog.ts.
// Source ("core") and Label (== ID) are filled in by buildCoreToolCatalog.
var coreSections = []coreSection{
	{"fs", "Files", []coreTool{
		{"read", "Read file contents", []ToolProfileID{ProfileCoding}},
		{"write", "Create or overwrite files", []ToolProfileID{ProfileCoding}},
		{"edit", "Make precise edits", []ToolProfileID{ProfileCoding}},
	}},
	{"runtime", "Runtime", []coreTool{
		{"exec", "Run shell commands", []ToolProfileID{ProfileCoding}},
		{"process", "Manage background processes", []ToolProfileID{ProfileCoding}},
	}},
	{"web", "Web", []coreTool{
		{"web", "Search the web, fetch URLs, or search+auto-fetch", []ToolProfileID{ProfileCoding}},
	}},
	{"memory", "Memory", []coreTool{
		{"memory_search", "Semantic search", []ToolProfileID{ProfileCoding}},
	}},
	{"sessions", "Sessions", []coreTool{
		{"sessions_list", "List sessions", []ToolProfileID{ProfileCoding, ProfileMessaging}},
		{"sessions_history", "Session history", []ToolProfileID{ProfileCoding, ProfileMessaging}},
		{"sessions_search", "Search sessions", []ToolProfileID{ProfileCoding, ProfileMessaging}},
		{"sessions_send", "Send to session", []ToolProfileID{ProfileCoding, ProfileMessaging}},
		{"sessions_spawn", "Spawn sub-agent", []ToolProfileID{ProfileCoding}},
		{"sessions_yield", "End turn to receive sub-agent results", []ToolProfileID{ProfileCoding}},
		{"subagents", "Manage sub-agents", []ToolProfileID{ProfileCoding}},
	}},
	{"messaging", "Messaging", []coreTool{
		{"message", "Send messages", []ToolProfileID{ProfileMessaging}},
	}},
	{"automation", "Automation", []coreTool{
		{"cron", "Schedule tasks", []ToolProfileID{ProfileCoding}},
		{"gateway", "Gateway control", []ToolProfileID{}},
	}},
{"media", "Media", []coreTool{
		{"image", "Image understanding", []ToolProfileID{ProfileCoding}},
	}},
}

// buildCoreToolCatalog expands compact definitions into the full JSON-ready
// catalog, filling in Source="core" and Label=ID, skipping empty sections.
func buildCoreToolCatalog() []ToolCatalogGroup {
	groups := make([]ToolCatalogGroup, 0, len(coreSections))
	for _, sec := range coreSections {
		if len(sec.Tools) == 0 {
			continue
		}
		entries := make([]ToolCatalogEntry, len(sec.Tools))
		for i, t := range sec.Tools {
			entries[i] = ToolCatalogEntry{
				ID:              t.ID,
				Label:           t.ID,
				Description:     t.Description,
				Source:          "core",
				DefaultProfiles: t.Profiles,
			}
		}
		groups = append(groups, ToolCatalogGroup{
			ID:     sec.ID,
			Label:  sec.Label,
			Source: "core",
			Tools:  entries,
		})
	}
	return groups
}

// CatalogMethods returns the static tools.catalog RPC handler.
func CatalogMethods() map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"tools.catalog": toolsCatalog(),
	}
}

func toolsCatalog() rpcutil.HandlerFunc {
	// Pre-compute the catalog once at registration time.
	groups := buildCoreToolCatalog()

	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			AgentID string `json:"agentId"`
			// IncludePlugins is accepted but ignored; plugin tools are not
			// available in the standalone Go gateway catalog.
			IncludePlugins *bool `json:"includePlugins"`
		}
		if len(req.Params) > 0 {
			_ = rpcutil.UnmarshalParams(req.Params, &p)
		}

		agentID := p.AgentID
		if agentID == "" {
			agentID = "default"
		}

		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
			"agentId":  agentID,
			"profiles": catalogProfileOptions,
			"groups":   groups,
		})
		return resp
	}
}

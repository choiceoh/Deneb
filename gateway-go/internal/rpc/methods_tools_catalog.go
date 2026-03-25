package rpc

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// ---------------------------------------------------------------------------
// tools.catalog — static core tool catalog
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

// coreToolCatalog mirrors the 22 core tools from src/agents/tool-catalog.ts.
var coreToolCatalog = []ToolCatalogGroup{
	{ID: "fs", Label: "Files", Source: "core", Tools: []ToolCatalogEntry{
		{ID: "read", Label: "read", Description: "Read file contents", Source: "core", DefaultProfiles: []ToolProfileID{ProfileCoding}},
		{ID: "write", Label: "write", Description: "Create or overwrite files", Source: "core", DefaultProfiles: []ToolProfileID{ProfileCoding}},
		{ID: "edit", Label: "edit", Description: "Make precise edits", Source: "core", DefaultProfiles: []ToolProfileID{ProfileCoding}},
		{ID: "apply_patch", Label: "apply_patch", Description: "Patch files (OpenAI)", Source: "core", DefaultProfiles: []ToolProfileID{ProfileCoding}},
	}},
	{ID: "runtime", Label: "Runtime", Source: "core", Tools: []ToolCatalogEntry{
		{ID: "exec", Label: "exec", Description: "Run shell commands", Source: "core", DefaultProfiles: []ToolProfileID{ProfileCoding}},
		{ID: "process", Label: "process", Description: "Manage background processes", Source: "core", DefaultProfiles: []ToolProfileID{ProfileCoding}},
	}},
	{ID: "web", Label: "Web", Source: "core", Tools: []ToolCatalogEntry{
		{ID: "web_search", Label: "web_search", Description: "Search the web", Source: "core", DefaultProfiles: []ToolProfileID{ProfileCoding}},
		{ID: "web_fetch", Label: "web_fetch", Description: "Fetch web content", Source: "core", DefaultProfiles: []ToolProfileID{ProfileCoding}},
	}},
	{ID: "memory", Label: "Memory", Source: "core", Tools: []ToolCatalogEntry{
		{ID: "memory_search", Label: "memory_search", Description: "Semantic search", Source: "core", DefaultProfiles: []ToolProfileID{ProfileCoding}},
		{ID: "memory_get", Label: "memory_get", Description: "Read memory files", Source: "core", DefaultProfiles: []ToolProfileID{ProfileCoding}},
	}},
	{ID: "sessions", Label: "Sessions", Source: "core", Tools: []ToolCatalogEntry{
		{ID: "sessions_list", Label: "sessions_list", Description: "List sessions", Source: "core", DefaultProfiles: []ToolProfileID{ProfileCoding, ProfileMessaging}},
		{ID: "sessions_history", Label: "sessions_history", Description: "Session history", Source: "core", DefaultProfiles: []ToolProfileID{ProfileCoding, ProfileMessaging}},
		{ID: "sessions_send", Label: "sessions_send", Description: "Send to session", Source: "core", DefaultProfiles: []ToolProfileID{ProfileCoding, ProfileMessaging}},
		{ID: "sessions_spawn", Label: "sessions_spawn", Description: "Spawn sub-agent", Source: "core", DefaultProfiles: []ToolProfileID{ProfileCoding}},
		{ID: "sessions_yield", Label: "sessions_yield", Description: "End turn to receive sub-agent results", Source: "core", DefaultProfiles: []ToolProfileID{ProfileCoding}},
		{ID: "subagents", Label: "subagents", Description: "Manage sub-agents", Source: "core", DefaultProfiles: []ToolProfileID{ProfileCoding}},
		{ID: "session_status", Label: "session_status", Description: "Session status", Source: "core", DefaultProfiles: []ToolProfileID{ProfileMinimal, ProfileCoding, ProfileMessaging}},
	}},
	{ID: "ui", Label: "UI", Source: "core", Tools: []ToolCatalogEntry{}},
	{ID: "messaging", Label: "Messaging", Source: "core", Tools: []ToolCatalogEntry{
		{ID: "message", Label: "message", Description: "Send messages", Source: "core", DefaultProfiles: []ToolProfileID{ProfileMessaging}},
	}},
	{ID: "automation", Label: "Automation", Source: "core", Tools: []ToolCatalogEntry{
		{ID: "cron", Label: "cron", Description: "Schedule tasks", Source: "core", DefaultProfiles: []ToolProfileID{ProfileCoding}},
		{ID: "gateway", Label: "gateway", Description: "Gateway control", Source: "core", DefaultProfiles: []ToolProfileID{}},
	}},
	{ID: "nodes", Label: "Nodes", Source: "core", Tools: []ToolCatalogEntry{
		{ID: "nodes", Label: "nodes", Description: "Nodes + devices", Source: "core", DefaultProfiles: []ToolProfileID{}},
	}},
	{ID: "media", Label: "Media", Source: "core", Tools: []ToolCatalogEntry{
		{ID: "image", Label: "image", Description: "Image understanding", Source: "core", DefaultProfiles: []ToolProfileID{ProfileCoding}},
	}},
}

// nonEmptyCoreToolCatalog filters out groups with no tools.
func nonEmptyCoreToolCatalog() []ToolCatalogGroup {
	out := make([]ToolCatalogGroup, 0, len(coreToolCatalog))
	for _, g := range coreToolCatalog {
		if len(g.Tools) > 0 {
			out = append(out, g)
		}
	}
	return out
}

func toolsCatalog() HandlerFunc {
	// Pre-compute the filtered catalog once.
	groups := nonEmptyCoreToolCatalog()

	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			AgentID        string `json:"agentId"`
			IncludePlugins *bool  `json:"includePlugins"`
		}
		// Params are optional; ignore parse errors.
		if len(req.Params) > 0 {
			_ = unmarshalParams(req.Params, &p)
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

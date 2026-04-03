// Tool catalog RPC handlers (tools.catalog).
package skill

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

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
		{"memory", "Unified memory (search, get, set, forget, status)", []ToolProfileID{ProfileCoding}},
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
		p, errResp := rpcutil.DecodeParams[struct {
			AgentID string `json:"agentId"`
			// IncludePlugins is accepted but ignored; plugin tools are not
			// available in the standalone Go gateway catalog.
			IncludePlugins *bool `json:"includePlugins"`
		}](req)
		if errResp != nil {
			return errResp
		}

		agentID := p.AgentID
		if agentID == "" {
			agentID = "default"
		}

		return rpcutil.RespondOK(req.ID, map[string]any{
			"agentId":  agentID,
			"profiles": catalogProfileOptions,
			"groups":   groups,
		})
	}
}

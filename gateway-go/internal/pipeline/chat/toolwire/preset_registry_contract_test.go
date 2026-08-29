package toolwire

import (
	"sort"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/toolpreset"
)

// registryNames is every tool RegisterCoreTools and the chat-side registrars put
// on the registry, plus the names that are legitimately registered elsewhere and
// so cannot appear in a toolwire fixture.
func registryNames(t *testing.T) map[string]struct{} {
	t.Helper()
	reg := &discoveryRegistry{defs: map[string]toolport.ToolDef{}}
	ws := t.TempDir()
	RegisterCoreTools(reg, &tooldeps.CoreToolDeps{WorkspaceDir: ws})
	RegisterPersonaTools(reg, ws)
	RegisterPeopleTool(reg, &tooldeps.ContactsDeps{}, nil)

	names := map[string]struct{}{}
	for name := range reg.defs {
		names[name] = struct{}{}
	}
	// Registered outside this package: wiki/notebook/skills/calendar need live
	// deps, fetch_tools and code_action come from RegisterRegistryBridgeTools
	// (they dial back into the concrete registry), gmail and skill_lifecycle
	// from the server, codegraph_* are external MCP tools, and the projection
	// sentinel is deliberately unregistrable.
	for _, n := range []string{
		"wiki", "wiki_forget", "notebook", "skills", "skill_lifecycle", "gmail",
		"knowledge", "polaris", "research_panel", "deal_ledger", "calendar",
		"sessions_yield", "memory", "image", "clarify",
		"fetch_tools", "code_action", "read_spillover",
		"codegraph_explore", "codegraph_node", "codegraph_search",
		"codegraph_callers", "codegraph_callees", "codegraph_impact",
		"__projection_no_tools__",
	} {
		names[n] = struct{}{}
	}
	return names
}

// TestEveryPresetNamesARegisteredTool guards the failure this test was written
// after: `contacts` folded into `people` (2026-08-29) but stayed listed in the
// researcher and briefcase presets. A preset entry that matches no tool is
// silent — the filter simply never admits it — so those two spawn presets lost
// person lookup entirely and nothing failed to say so.
func TestEveryPresetNamesARegisteredTool(t *testing.T) {
	known := registryNames(t)
	for _, preset := range []toolpreset.Preset{
		toolpreset.PresetConversation, toolpreset.PresetBoot, toolpreset.PresetSelfReview,
		toolpreset.PresetResearcher, toolpreset.PresetImplementer, toolpreset.PresetVerifier,
		toolpreset.PresetWikiResearch, toolpreset.PresetWikiScout, toolpreset.PresetNotiDigest,
		toolpreset.PresetCoding, toolpreset.PresetBriefcase, toolpreset.PresetProjection,
	} {
		allowed := toolpreset.AllowedTools(preset)
		if allowed == nil {
			t.Errorf("preset %q has no allow-list", preset)
			continue
		}
		var dead []string
		for name := range allowed {
			if _, ok := known[name]; !ok {
				dead = append(dead, name)
			}
		}
		if len(dead) > 0 {
			sort.Strings(dead)
			t.Errorf("preset %q allows tools that are not registered: %s", preset, strings.Join(dead, ", "))
		}
	}
}

// TestToolDisplayMapsNameRegisteredTools: the label and detail maps are the
// user-facing half of the same staleness — a key for a removed tool is dead
// weight, and a missing key means a live tool renders without its label.
func TestToolDisplayMapsNameRegisteredTools(t *testing.T) {
	known := registryNames(t)
	for _, name := range toolport.LabelledToolNames() {
		if _, ok := known[name]; !ok {
			t.Errorf("tool label map has an entry for %q, which is not a registered tool", name)
		}
	}
	for _, name := range toolport.DetailedToolNames() {
		if _, ok := known[name]; !ok {
			t.Errorf("tool detail map has an entry for %q, which is not a registered tool", name)
		}
	}
}

// TestToolPromptsDoNotNameRetiredTools: a description that points at a removed
// tool sends the model somewhere that no longer exists. phone_read said "주소록은
// contacts" and sessions_spawn's preset help listed contacts among the
// researcher tools, both months after the tool was gone.
func TestToolPromptsDoNotNameRetiredTools(t *testing.T) {
	retired := []string{"contacts", "subagents"}
	reg := &discoveryRegistry{defs: map[string]toolport.ToolDef{}}
	ws := t.TempDir()
	RegisterCoreTools(reg, &tooldeps.CoreToolDeps{WorkspaceDir: ws})
	RegisterPersonaTools(reg, ws)
	RegisterPeopleTool(reg, &tooldeps.ContactsDeps{}, nil)

	// Exempt: text that names a retired word for a reason other than pointing
	// at a tool. phone_read lists clipboard/contacts/screen as what values it
	// does NOT accept, which is guidance the model needs.
	exempt := map[string]string{"phone_read": "lists 'contacts' as a rejected `what` value, not as a tool"}

	for name, def := range reg.defs {
		if _, ok := exempt[name]; ok {
			continue
		}
		haystack := def.Description + " " + schemaText(def.InputSchema)
		for _, gone := range retired {
			// Word-ish match: the bare name, not a substring of another word.
			for _, sep := range []string{" " + gone + " ", " " + gone + ".", "/" + gone, gone + "를", gone + "을"} {
				if strings.Contains(haystack, sep) {
					t.Errorf("%s prompt still names the retired tool %q (%q)", name, gone, sep)
				}
			}
		}
	}
}

// schemaText flattens a tool schema's prose so a description sweep can read it.
func schemaText(schema map[string]any) string {
	var b strings.Builder
	var walk func(any)
	walk = func(v any) {
		switch t := v.(type) {
		case map[string]any:
			for k, sub := range t {
				if k == "description" {
					if s, ok := sub.(string); ok {
						b.WriteString(" " + s)
					}
					continue
				}
				walk(sub)
			}
		case []any:
			for _, sub := range t {
				walk(sub)
			}
		}
	}
	walk(schema)
	return b.String()
}

// TestBrowserRegistersOnlyWithAConfiguredBridge pins the 2026-08-29 sweep for
// tools that exist but cannot work. Without DENEB_BROWSER_URL every browser
// call answers "브라우저 연동이 꺼져 있습니다", so an unconfigured deployment was
// advertising a surface that can only refuse — and it had never been called
// once in the recorded history. The address book already followed this rule
// ("do not show the agent a dead surface"); browser did not.
func TestBrowserRegistersOnlyWithAConfiguredBridge(t *testing.T) {
	registered := func(deps *tooldeps.CoreToolDeps) bool {
		reg := &discoveryRegistry{defs: map[string]toolport.ToolDef{}}
		RegisterCoreTools(reg, deps)
		_, ok := reg.defs["browser"]
		return ok
	}

	if registered(&tooldeps.CoreToolDeps{WorkspaceDir: t.TempDir()}) {
		t.Error("browser must not register without a bridge URL")
	}
	if registered(&tooldeps.CoreToolDeps{
		WorkspaceDir: t.TempDir(),
		Browser:      tooldeps.BrowserDeps{BaseURL: func() string { return "   " }},
	}) {
		t.Error("a blank bridge URL is still unconfigured")
	}
	if !registered(&tooldeps.CoreToolDeps{
		WorkspaceDir: t.TempDir(),
		Browser:      tooldeps.BrowserDeps{BaseURL: func() string { return "http://127.0.0.1:1" }},
	}) {
		t.Error("browser must register once the bridge is configured")
	}
}

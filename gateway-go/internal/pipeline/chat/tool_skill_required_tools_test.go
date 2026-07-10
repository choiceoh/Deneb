package chat

import (
	"context"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
)

func requiredToolsRegistry() *ToolRegistry {
	r := NewToolRegistry()
	r.RegisterTool(toolctx.ToolDef{Name: "graphify", Description: "graph queries", Deferred: true})
	r.RegisterTool(toolctx.ToolDef{Name: "notebook", Description: "notebook", Deferred: true})
	r.RegisterTool(toolctx.ToolDef{Name: "exec", Description: "run a command"})
	return r
}

func requiredToolsCatalog() []skills.PromptSkill {
	return []skills.PromptSkill{
		{Name: "graph-analysis", RequiresTools: []string{"graphify", "exec", "ghost-tool"}},
		{Name: "plain-skill"},
	}
}

// TestActivateSkillRequiredTools: a consult activates the skill's deferred
// requires_tools — eager and unknown names are skipped, the notice names
// exactly what was activated, and the DeferredActivation tracker receives it.
func TestActivateSkillRequiredTools(t *testing.T) {
	registry := requiredToolsRegistry()
	da := toolctx.NewDeferredActivation()
	ctx := toolctx.WithDeferredActivation(context.Background(), da)

	notice := activateSkillRequiredTools(ctx, registry, "graph-analysis", requiredToolsCatalog())
	if !strings.Contains(notice, "graphify") {
		t.Fatalf("notice must name the activated tool, got %q", notice)
	}
	if strings.Contains(notice, "exec") || strings.Contains(notice, "ghost-tool") {
		t.Fatalf("eager/unknown tools must not be activated, got %q", notice)
	}
	if got := da.ActivatedNames(); len(got) != 1 || got[0] != "graphify" {
		t.Fatalf("activated = %v, want [graphify]", got)
	}
}

// TestActivateSkillRequiredTools_noOps: every path that must return "" —
// no tracker on ctx, skill without requires_tools, unknown skill, already
// active tool, and preset-excluded tool.
func TestActivateSkillRequiredTools_noOps(t *testing.T) {
	registry := requiredToolsRegistry()
	catalog := requiredToolsCatalog()

	if got := activateSkillRequiredTools(context.Background(), registry, "graph-analysis", catalog); got != "" {
		t.Fatalf("nil DeferredActivation must no-op, got %q", got)
	}

	da := toolctx.NewDeferredActivation()
	ctx := toolctx.WithDeferredActivation(context.Background(), da)
	if got := activateSkillRequiredTools(ctx, registry, "plain-skill", catalog); got != "" {
		t.Fatalf("skill without requires_tools must no-op, got %q", got)
	}
	if got := activateSkillRequiredTools(ctx, registry, "no-such-skill", catalog); got != "" {
		t.Fatalf("unknown skill must no-op, got %q", got)
	}
	if got := activateSkillRequiredTools(ctx, nil, "graph-analysis", catalog); got != "" {
		t.Fatalf("nil registry must no-op, got %q", got)
	}

	// Already active: drain publishes the IsActive snapshot (as the executor
	// does between turns), so a second consult adds no notice.
	if notice := activateSkillRequiredTools(ctx, registry, "graph-analysis", catalog); notice == "" {
		t.Fatal("first consult must activate")
	}
	da.ActivatedNames()
	if got := activateSkillRequiredTools(ctx, registry, "graph-analysis", catalog); got != "" {
		t.Fatalf("already-active tool must no-op, got %q", got)
	}

	// Preset gate: the conversation preset does not allow graphify.
	restricted := toolctx.WithToolPreset(
		toolctx.WithDeferredActivation(context.Background(), toolctx.NewDeferredActivation()),
		"conversation",
	)
	if got := activateSkillRequiredTools(restricted, registry, "graph-analysis", catalog); got != "" {
		t.Fatalf("preset-excluded tool must no-op, got %q", got)
	}
}

// TestSkillNameFromSkillsReadOutput: only a cataloged SKILL.md body (raw
// frontmatter at the top, name matching the catalog) identifies a consult.
func TestSkillNameFromSkillsReadOutput(t *testing.T) {
	catalog := requiredToolsCatalog()
	cases := []struct {
		name   string
		output string
		want   string
	}{
		{
			"cataloged skill body",
			"---\nname: graph-analysis\nversion: 1.0.0\n---\n\n# Graph Analysis\n", "graph-analysis",
		},
		{
			"frontmatter name outside the catalog",
			"---\nname: stranger\n---\nbody", "",
		},
		{"list action JSON output", `{"skills":[{"name":"graph-analysis"}]}`, ""},
		{"aux file without frontmatter", "# reference notes\nplain markdown", ""},
		{"frontmatter without name", "---\nversion: 1.0.0\n---\nbody", ""},
	}
	for _, tc := range cases {
		if got := skillNameFromSkillsReadOutput(tc.output, catalog); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
	if got := skillNameFromSkillsReadOutput("---\nname: graph-analysis\n---\n", nil); got != "" {
		t.Errorf("empty catalog must never match, got %q", got)
	}
}

// TestSkillsReadToolsActivator_passthrough: the skills-tool post-processor
// passes non-consult outputs through unchanged (global snapshot is empty
// under test; the matching itself is covered above).
func TestSkillsReadToolsActivator_passthrough(t *testing.T) {
	pp := NewSkillsReadToolsActivator(requiredToolsRegistry())
	in := "---\nname: graph-analysis\n---\nbody"
	if out := pp(context.Background(), "skills", in); out != in {
		t.Fatalf("output must pass through unchanged, got %q", out)
	}
}

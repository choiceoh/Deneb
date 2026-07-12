// tool_skill_required_tools.go — activate a consulted skill's required
// deferred tools in the same step the skill body is loaded.
//
// A skill's frontmatter already names the tools its procedure needs
// (metadata.deneb.requires_tools — until now only an eligibility filter for
// the compact index). When the model loads a skill body it is about to follow
// the procedure, so any required tool that is still deferred forces an extra
// fetch_tools round-trip right after the read. This bridge removes that trip:
// the consult itself activates the skill's deferred tools, so the skill's
// instructions (SKILL.md) and its tools arrive as one bundle — the same
// composition Pydantic AI 2.0 ships as "capabilities" (instructions + tools
// load together via load_capability).
//
// Cache note: activation changes the Tools array exactly like a fetch_tools
// call would — a one-time prompt-cache break the deferred-tool design already
// pays. The appended notice lives in the tool result (transcript), never in
// the system prompt.
package chat

import (
	"context"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/toolpreset"
	"github.com/choiceoh/deneb/gateway-go/pkg/toolmeta"
)

// activateSkillRequiredTools activates the named skill's requires_tools that
// are deferred, preset-allowed, and not yet active. Returns a short notice to
// append to the tool output, or "" when nothing was activated. Eager and
// unknown tool names are skipped silently: eager tools are already callable,
// and requires_tools is also an eligibility filter whose entries need not all
// be registry tools.
func activateSkillRequiredTools(ctx context.Context, registry *ToolRegistry, skillName string, resolved []skills.PromptSkill) string {
	if registry == nil {
		return ""
	}
	da := toolctx.DeferredActivationFromContext(ctx)
	if da == nil {
		return ""
	}
	var required []string
	for _, s := range resolved {
		if s.Name == skillName {
			required = s.RequiresTools
			break
		}
	}
	if len(required) == 0 {
		return ""
	}
	// Same preset gate as fetch_tools: a restricted run must not activate a
	// tool Execute would reject anyway. nil allowed = no restriction.
	allowed := toolpreset.AllowedTools(toolpreset.Preset(toolctx.ToolPresetFromContext(ctx)))
	var names []string
	for _, name := range required {
		if _, ok := registry.DeferredToolDef(name); !ok {
			continue
		}
		if allowed != nil {
			if _, ok := allowed[name]; !ok {
				continue
			}
		}
		if da.IsActive(name) {
			continue
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		return ""
	}
	da.Activate(names)
	// Structured replay evidence (server-attached) — a SKILL.md body that
	// CONTAINS a notice-shaped string can no longer forge activation once
	// readers prefer metadata (deferred_replay.go).
	toolmeta.Set(ctx, "activatedTools", names)
	// Exact shared format (toolctx/activation_notice.go): the model-facing
	// half, and the replay fallback for pre-metadata transcripts.
	return "\n\n" + toolctx.FormatSkillActivationNotice(names)
}

// NewSkillsReadToolsActivator returns the per-tool post-processor for the
// `skills` tool. When an action=read returned a cataloged SKILL.md body, it
// activates the skill's required deferred tools. Consult recording stays in
// the tool itself (tools/skill_manage.go); this only adds the tool half of
// the bundle.
func NewSkillsReadToolsActivator(registry *ToolRegistry) PostProcessor {
	return func(ctx context.Context, _, output string) string {
		resolved := cachedResolvedSkills()
		name := skillNameFromSkillsReadOutput(output, resolved)
		if name == "" {
			return output
		}
		return output + activateSkillRequiredTools(ctx, registry, name, resolved)
	}
}

// skillNameFromSkillsReadOutput extracts the skill name from a
// skills(action=read) output, or "" when the output is not a cataloged
// SKILL.md body. A main-body read returns the raw file content, so the
// frontmatter is at the top and its name field must exactly match a resolved
// catalog skill — the same catalog gating as skillNameFromReadOutput.
// Auxiliary-file reads and every other action fall through the frontmatter
// check.
func skillNameFromSkillsReadOutput(output string, resolved []skills.PromptSkill) string {
	if len(resolved) == 0 || !strings.HasPrefix(output, "---") {
		return ""
	}
	if header, _ := skills.ExtractFrontmatterBlock(output); header == "" {
		return ""
	}
	name := strings.TrimSpace(skills.ParseFrontmatter(output)["name"])
	if name == "" {
		return ""
	}
	for _, s := range resolved {
		if s.Name == name {
			return s.Name
		}
	}
	return ""
}

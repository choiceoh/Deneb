// tool_skill_read_consult.go — count a plain `read` of a cataloged SKILL.md as
// a skill consult.
//
// The system prompt's compact skills index teaches the model to load a skill
// body by `read`ing the listed path directly, and the very first live hint
// verification (2026-07-04, "이 계약서 검토해줘") did exactly that: the model
// read skills/productivity/contract-review/SKILL.md with the `read` tool and
// followed the procedure — but only the `skills(action="read")` path fed the
// SkillConsultLog, so the consult never reached skill_usage.jsonl and the
// hint→consult conversion metric undercounted its very first success. This
// post-processor closes that gap: when a `read` output is a SKILL.md whose
// directory names a skill in the runtime catalog, record the consult.
//
// Catalog-gated on purpose: a SKILL.md read in a coding worktree (editing a
// skill as CODE) only matches if the dirname is a live skill name. That catalog
// gate is the only filter — the recording layer (recordRunSkillUsage) excludes
// nothing but the review fork's own sessions, so a catalog-named SKILL.md read
// in a coding session does count as a consult.
package chat

import (
	"context"
	"path"
	"path/filepath"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire"
)

// NewReadSkillConsultRecorder returns the per-tool post-processor for `read`.
// It records the consult and activates the skill's required deferred tools
// (tool_skill_required_tools.go) — non-consult reads pass through unchanged.
// Registered before the global compaction/trimming processors, so the
// "[File: …]" header is still intact.
func NewReadSkillConsultRecorder(registry *ToolRegistry) PostProcessor {
	return func(ctx context.Context, _, output string) string {
		if toolport.ToolPresetFromContext(ctx) == toolwire.PresetBriefcase {
			return output
		}
		resolved := cachedResolvedSkills()
		name := skillNameFromReadOutput(output, resolved)
		if name == "" {
			return output
		}
		toolport.SkillConsultLogFromContext(ctx).Add(name)
		return output + activateSkillRequiredTools(ctx, registry, name, resolved)
	}
}

// skillNameFromReadOutput extracts the consulted skill name from a read-tool
// output, or "" when the read was not a cataloged SKILL.md. The read header is
// "[File: <path> | N lines]" (with an optional anchor-columns variant); the
// skill is identified by its directory name, which must exactly match a
// resolved catalog skill (both bundled skills/<category>/<name>/SKILL.md and
// workspace ~/.deneb/skills/<name>/SKILL.md layouts name the dir after the
// skill).
func skillNameFromReadOutput(output string, resolved []skills.PromptSkill) string {
	const prefix = "[File: "
	if len(resolved) == 0 || !strings.HasPrefix(output, prefix) {
		return ""
	}
	header := output[len(prefix):]
	if nl := strings.IndexByte(header, '\n'); nl >= 0 {
		header = header[:nl]
	}
	sep := strings.Index(header, " | ")
	if sep < 0 {
		return ""
	}
	p := filepath.ToSlash(strings.TrimSpace(header[:sep]))
	if path.Base(p) != "SKILL.md" {
		return ""
	}
	dir := path.Base(path.Dir(p))
	if dir == "" || dir == "." || dir == "/" {
		return ""
	}
	for _, s := range resolved {
		if s.Name == dir {
			return s.Name
		}
	}
	return ""
}

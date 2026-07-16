// skill_hints.go — deterministic per-turn skill surfacing (auto-hint).
//
// The semi-static system prompt carries only a name manifest, but 30-day
// production forensics (2026-07) showed the model almost never acts on it
// unprompted: ~170 skill consults across 7,986 tool calls, and every consult
// that did happen was force-summoned (mail cron) or name-obvious (topsolar-db).
// The barrier is discovery-at-the-right-moment, not availability.
//
// This matcher closes that gap: a skill declares Korean utterance triggers in
// its frontmatter (metadata.deneb.triggers), and when the user's message
// contains one, a short pointer rides the wire-only tail of the last user
// message (run_tail_inject.go) telling the model the procedure exists and how
// to load it. Deterministic — no LLM call (model-roles.md dogma 4), no I/O
// (in-memory skills snapshot), APC-safe (tail addition, prompt-cache.md §1.5).
// Opt-in per skill: no triggers, no hint — tags/descriptions are deliberately
// NOT mined (generic taxonomy words like "검토" would over-fire).
package chat

import (
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire"
)

// maxSkillHints caps how many skills one turn may surface — a hint is a nudge,
// not a menu.
const maxSkillHints = 2

// cachedResolvedSkills returns the resolved skills of the last-built snapshot,
// or nil before the first prompt build (hints simply stay off until then).
func cachedResolvedSkills() []skills.PromptSkill {
	if snap := CachedSkillsSnapshot(); snap != nil {
		return snap.ResolvedSkills
	}
	return nil
}

// buildSkillHints returns the tail-addition block pointing at skills whose
// triggers match the user message, plus the matched skill names (for the
// hint-fired measurement event — joined against skill_usage.jsonl this yields
// the hint→consult conversion rate). Both empty when nothing matches. Skipped
// for system-internal runs (session "system:*" — skill-review forks carry
// SKILL.md bodies as messages, which would self-trigger on their own trigger
// lists) and for recall-suppressed runs (ephemeral probes share the same "no
// side context" intent).
//
// sessionToolPreset is the run's effective preset: the hint instructs a
// `skills(action="read")` call, so a preset whose allow-list excludes the
// skills tool (btw:* runs use "conversation") would turn the hint into a
// guaranteed tool-not-allowed error — no hint beats a hint at a blocked door.
func buildSkillHints(params RunParams, sessionToolPreset string, resolved []skills.PromptSkill) (string, []string) {
	if params.EphemeralUser || params.SkipRecall {
		return "", nil
	}
	if strings.HasPrefix(params.SessionKey, "system:") {
		return "", nil
	}
	if !presetAllowsSkillsTool(sessionToolPreset) {
		return "", nil
	}
	hints := skills.MatchSkillTriggers(params.Message, resolved, maxSkillHints)
	if len(hints) == 0 {
		return "", nil
	}

	var b strings.Builder
	names := make([]string, 0, len(hints))
	b.WriteString("[관련 스킬 — 이 요청에 맞는 준비된 절차]")
	for _, skill := range hints {
		fmt.Fprintf(&b, "\n- %s: %s → `skills(action=\"read\", name=%q)`로 절차와 필요한 도구를 함께 로드해 그대로 따르라.",
			skill.Name, skillHintSummary(skill.Description), skill.Name)
		names = append(names, skill.Name)
	}
	return b.String(), names
}

// presetAllowsSkillsTool reports whether the run's effective tool preset
// permits calling the `skills` tool the hint points at. An empty/unknown
// preset means no restriction (AllowedTools nil).
func presetAllowsSkillsTool(sessionToolPreset string) bool {
	allowed := toolwire.AllowedTools(sessionToolPreset)
	if allowed == nil {
		return true
	}
	_, ok := allowed["skills"]
	return ok
}

// skillHintSummary reduces a skill description to its first clause — the
// "what it does" — dropping the "Use when/NOT for" tail. Cut at the EARLIEST
// clause separator, capped at 90 runes.
func skillHintSummary(desc string) string {
	desc = strings.TrimSpace(desc)
	cut := len(desc)
	for _, sep := range []string{" — ", ". ", "。"} {
		if i := strings.Index(desc, sep); i > 0 && i < cut {
			cut = i
		}
	}
	desc = desc[:cut]
	if runes := []rune(desc); len(runes) > 90 {
		desc = string(runes[:90]) + "…"
	}
	return desc
}

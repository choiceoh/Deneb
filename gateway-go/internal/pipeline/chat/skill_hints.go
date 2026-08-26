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
// contains one, its bounded instruction body rides the wire-only tail of the
// last user message (run_tail_inject.go). This avoids a fragile model-initiated
// read hop while keeping unrelated skills out of the prompt. Deterministic —
// no LLM call (model-roles.md dogma 4), no per-turn I/O (frozen in-memory
// snapshot), APC-safe (tail addition, prompt-cache.md §1.5).
// Opt-in per skill: no triggers, no hint — tags/descriptions are deliberately
// NOT mined (generic taxonomy words like "검토" would over-fire).
package chat

import (
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire"
)

const (
	// maxSkillHints caps how many skills one turn may surface — exact triggers
	// select procedures, not a menu.
	maxSkillHints = 2
	// Individual and aggregate caps keep JIT context bounded. Oversized bodies
	// fall back to the existing explicit read pointer instead of truncating an
	// instruction contract mid-step.
	// Markers around an auto-loaded body. skillBodiesInHistory parses them back
	// out, so the two must stay in sync — they are written once, here.
	skillBodyBeginPrefix = "--- "
	skillBodyBeginSuffix = " instructions begin ---"

	maxAutoLoadedSkillBodyBytes  = 15_000
	maxAutoLoadedSkillTotalBytes = 20_000
)

// cachedResolvedSkills returns the resolved skills of the last-built snapshot,
// or nil before the first prompt build (hints simply stay off until then).
func cachedResolvedSkills() []skills.PromptSkill {
	if snap := CachedSkillsSnapshot(); snap != nil {
		return snap.ResolvedSkills
	}
	return nil
}

// buildSkillHints returns the tail-addition block for skills whose triggers
// match the user message, the matched names, and the subset whose bodies were
// loaded directly. Cap overflows keep the explicit read fallback; a skill the
// JIT loader produced NO body for is dropped entirely (skillHasLoadableBody).
// All outputs are empty when nothing matches. Skipped
// for system-internal runs (session "system:*" — skill-review forks carry
// SKILL.md bodies as messages, which would self-trigger on their own trigger
// lists) and for recall-suppressed runs (ephemeral probes share the same "no
// side context" intent).
//
// sessionToolPreset is the run's effective preset. Keep the historical skills
// tool gate so restricted side runs do not acquire new procedural context.
func buildSkillHints(
	params RunParams,
	sessionToolPreset string,
	resolved []skills.PromptSkill,
	alreadyInHistory map[string]bool,
) (string, []string, []string) {
	if params.EphemeralUser || params.SkipRecall {
		return "", nil, nil
	}
	if session.IsSystemSession(params.SessionKey) {
		return "", nil, nil
	}
	if !presetAllowsSkillsTool(sessionToolPreset) {
		return "", nil, nil
	}
	// Match only what the operator actually asked for — not the payload their
	// client pasted in with it (skill_hint_scope.go).
	hints := skills.MatchSkillTriggers(skillTriggerScope(params.Message), resolved, maxSkillHints)
	hints = skillsRunnableUnderPreset(hints, sessionToolPreset)
	hints = skillsWithLoadableBody(hints)
	if len(hints) == 0 {
		return "", nil, nil
	}

	var loaded, fallback strings.Builder
	names := make([]string, 0, len(hints))
	autoLoaded := make([]string, 0, len(hints))
	loadedBytes := 0
	for _, skill := range hints {
		names = append(names, skill.Name)
		// Already on the wire from an earlier turn of this session (the tail
		// register keeps historical copies attached): skip the BODY, not the
		// skill. It still counts as loaded for this turn — its instructions are
		// in the model's context — so the tools it declares (RequiresTools) keep
		// getting activated. Skipping the whole skill dropped those activations
		// and, with the called-only deferred replay, the tool vanished while the
		// history still told the model to use it (모닝레터 → morning_letter,
		// caught in puppet mode 2026-08-26).
		if alreadyInHistory[skill.Name] {
			autoLoaded = append(autoLoaded, skill.Name)
			continue
		}
		body := strings.TrimSpace(skill.Body)
		if body != "" && len(body) <= maxAutoLoadedSkillBodyBytes && loadedBytes+len(body) <= maxAutoLoadedSkillTotalBytes {
			if loaded.Len() == 0 {
				loaded.WriteString("[Auto-loaded skills — exact trigger match]")
			}
			safeName := strings.Join(strings.Fields(skill.Name), " ")
			fmt.Fprintf(&loaded, "\n\n--- %s instructions begin ---\n%s\n--- %s instructions end ---", safeName, body, safeName)
			autoLoaded = append(autoLoaded, skill.Name)
			loadedBytes += len(body)
			continue
		}
		if fallback.Len() == 0 {
			fallback.WriteString("[Related skills — load on demand]")
		}
		fmt.Fprintf(&fallback, "\n- %s: %s → `skills(action=\"read\", name=%q)`로 필요한 절차만 로드하라.",
			skill.Name, skillHintSummary(skill.Description), skill.Name)
	}
	if loaded.Len() > 0 {
		loaded.WriteString("\n\nApply only the procedure and completion criteria needed for this request. Preserve dependencies and safety gates; do not force an order on independent checks.")
	}
	parts := make([]string, 0, 2)
	if loaded.Len() > 0 {
		parts = append(parts, loaded.String())
	}
	if fallback.Len() > 0 {
		parts = append(parts, fallback.String())
	}
	return strings.Join(parts, "\n\n"), names, autoLoaded
}

// presetAllowsSkillsTool reports whether the run's effective tool preset
// admits skill context and its read fallback. An empty/unknown preset means no
// restriction (AllowedTools nil).
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

// skillBodiesInHistory reports which skills already have an auto-loaded body in
// the assembled messages, using the same markers buildSkillHints writes. Called
// with the tail-attached history, so it sees the copies the model will actually
// receive.
func skillBodiesInHistory(messages []llm.Message) map[string]bool {
	present := map[string]bool{}
	for _, msg := range messages {
		content := string(msg.Content.Bytes())
		if !strings.Contains(content, skillBodyBeginPrefix) {
			continue
		}
		rest := content
		for {
			i := strings.Index(rest, skillBodyBeginPrefix)
			if i < 0 {
				break
			}
			rest = rest[i+len(skillBodyBeginPrefix):]
			j := strings.Index(rest, skillBodyBeginSuffix)
			if j < 0 {
				break
			}
			if name := strings.TrimSpace(rest[:j]); name != "" {
				present[name] = true
			}
			rest = rest[j:]
		}
	}
	return present
}

// skillsRunnableUnderPreset drops skills whose declared tools this run cannot
// reach. The eligibility filter behind the ambient catalog is computed against
// the WHOLE registry (run_exec_skills.go: availableToolNames returns
// tools.SortedNames()), so a scoped run — a researcher sub-agent, boot, 대화모드
// — still trigger-matches skills whose procedure needs exec/write it does not
// have. Injecting one is worse than useless since #4783: a skill that is
// delivered and then not followed is recorded as that SKILL's failure, so the
// preset's restriction gets charged to the skill and feeds the evolver's
// success-rate gate.
//
// A preset with no allow-list (nil) restricts nothing and every skill stays.
// skillsWithLoadableBody drops matches the JIT loader produced no body for.
//
// The fallback line tells the model `skills(action="read", name=…)`로 필요한
// 절차만 로드하라 — it promises a procedure. jitSkillInstructionBody returns ""
// in exactly the cases where that promise cannot hold: a SKILL.md that is
// frontmatter and nothing else (the read returns the metadata the hint line
// already carried), and a non-prompt skill, whose body the loader withholds ON
// PURPOSE because a local/system skill is something to invoke, not a procedure
// to follow. Pointing the model at either costs a round trip and returns no
// instruction. Measured from the puppet seat 2026-08-26: a frontmatter-only
// skill was advertised, read, and answered with its own frontmatter.
//
// Dropping them from the hint list also keeps them out of hintedSkills, so a
// skill that could never have been followed is not recorded as one the model
// ignored.
func skillsWithLoadableBody(hints []skills.PromptSkill) []skills.PromptSkill {
	loadable := hints[:0:0]
	for _, skill := range hints {
		if strings.TrimSpace(skill.Body) == "" {
			continue
		}
		loadable = append(loadable, skill)
	}
	return loadable
}

func skillsRunnableUnderPreset(hints []skills.PromptSkill, sessionToolPreset string) []skills.PromptSkill {
	allowed := toolwire.AllowedTools(sessionToolPreset)
	if allowed == nil || len(hints) == 0 {
		return hints
	}
	runnable := hints[:0:0]
	for _, skill := range hints {
		missing := false
		for _, tool := range skill.RequiresTools {
			if _, ok := allowed[tool]; !ok {
				missing = true
				break
			}
		}
		if !missing {
			runnable = append(runnable, skill)
		}
	}
	return runnable
}

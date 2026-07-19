package skilllifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/generation"
	"github.com/choiceoh/deneb/gateway-go/pkg/textutil"
)

const (
	// relevanceTranscriptBudget bounds the transcript slice sent to the classifier
	// — a relevance call is a cheap classification, not a full read.
	relevanceTranscriptBudget = 6000
	relevanceTimeout          = 20 * time.Second
)

// sessionExercisesSkill asks the lightweight text model — in a FRESH context that
// never sees the producer or the candidate — whether the session was a genuine
// example of USING skillName, versus one where the skill was only consulted while
// the real work was unrelated. This is a pure LLM production step (classification);
// the accept/reject judgment stays deterministic downstream, so it does not touch
// the "LLM produces, deterministic Go judges" invariant. Shared by the real-time
// capture (chat_adapters.go) and the retro backfill lane (skill_lifecycle_tool_validation.go).
//
// Fail-OPEN: with no classifier wired, or on any error / unparseable output, it
// returns true (record the case, the prior behavior), so a down classifier can
// never silently starve the already-sparse corpus. It only skips on an explicit,
// parsed "does not use the skill" verdict.
func sessionExercisesSkill(logger *slog.Logger, client *llm.Client, model, skillName string, sctx generation.SessionContext) bool {
	if client == nil || strings.TrimSpace(model) == "" {
		return true // gate disabled — preserve record-everything behavior
	}
	model = strings.TrimSpace(model)
	skillName = strings.TrimSpace(skillName)
	transcript := strings.TrimSpace(textutil.TruncateRunes(sctx.AllText, relevanceTranscriptBudget, " …[truncated]"))
	if skillName == "" || transcript == "" {
		return true // nothing to classify against — don't drop
	}

	ctx, cancel := context.WithTimeout(context.Background(), relevanceTimeout)
	defer cancel()

	system := "You decide whether a chat session is a GENUINE example of using a named agent skill. " +
		"An agent lists/consults many skills each turn; only some actually guide the work. " +
		"Answer with a strict JSON object {\"uses_skill\": true|false} and nothing else. " +
		"true only if the session's actual task is what the skill is for; " +
		"false if the skill was merely available/consulted while the real work was about something else."
	user := fmt.Sprintf("Skill: %s\nTools used this session: %s\n\nSession transcript (truncated):\n%s",
		skillName, strings.Join(toolActivityNames(sctx.ToolActivities), ", "), transcript)

	raw, err := client.Complete(ctx, llm.ChatRequest{
		Model:    model,
		Messages: []llm.Message{llm.NewTextMessage("user", user)},
		System:   llm.SystemString(system),
		// The verdict is a one-line JSON, but Thinking=disabled is ADVISORY:
		// some lightweight-role models (glm/deepseek family) ignore it and
		// reason anyway, and a small MaxTokens then gets fully consumed by
		// reasoning → empty content (finish_reason=length) → fail-open, which
		// admits off-topic sessions into the held-out corpus (the exact
		// contamination this classifier exists to prevent). Budget for a
		// reasoning model's forced thinking (~600 tok observed) plus the tiny
		// output so the verdict actually lands. Models that honor disabled
		// still return in a few tokens — the ceiling only affects the tail.
		MaxTokens:      2048,
		Thinking:       &llm.ThinkingConfig{Type: "disabled"},
		ResponseFormat: &llm.ResponseFormat{Type: "json_object"},
	})
	if err != nil {
		if logger != nil {
			logger.Warn("genesis: skill-relevance classifier failed; recording case anyway",
				"skill", skillName, "session", sctx.Key, "error", err)
		}
		return true // fail-open
	}
	uses, parsed := parseUsesSkillVerdict(raw)
	if !parsed {
		if logger != nil {
			logger.Warn("genesis: skill-relevance classifier gave unparseable output; recording case anyway",
				"skill", skillName, "session", sctx.Key)
		}
		return true // fail-open
	}
	if !uses && logger != nil {
		logger.Info("genesis: skipped off-topic validation case (session did not exercise skill)",
			"skill", skillName, "session", sctx.Key)
	}
	return uses
}

// parseUsesSkillVerdict extracts the {"uses_skill": bool} verdict. parsed is false
// when no such field is present, so the caller can fail open.
func parseUsesSkillVerdict(raw string) (uses bool, parsed bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false, false
	}
	// The model may wrap the object in prose or a code fence; extract the first
	// balanced object rather than requiring a bare JSON body.
	if i := strings.IndexByte(raw, '{'); i >= 0 {
		if j := strings.LastIndexByte(raw, '}'); j > i {
			raw = raw[i : j+1]
		}
	}
	var v struct {
		UsesSkill *bool `json:"uses_skill"`
	}
	if err := json.Unmarshal([]byte(raw), &v); err != nil || v.UsesSkill == nil {
		return false, false
	}
	return *v.UsesSkill, true
}

// toolActivityNames lists the distinct tool names used in a session, order-stable.
func toolActivityNames(activities []generation.ToolActivity) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, t := range activities {
		name := strings.TrimSpace(t.Name)
		if name == "" {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

// dreaming_mutual.go — Phase 6 of AuroraDream: mutual understanding synthesis (상호 인식).
//
// Unlike Phase 5 (static user profile), Phase 6 tracks the EVOLVING relationship:
// - Reads previous mutual understanding state for continuity
// - Reads relationship history for multi-cycle trend analysis
// - Integrates Phase 5 user profile for cross-phase interpretation
// - Analyzes new mutual signals since last cycle
// - Produces updated understanding that reflects temporal changes
// - Appends a history snapshot for long-term trajectory tracking
// - Cleans up consumed mu_signals_raw after synthesis
package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

const mutualUnderstandingSystemPrompt = `You are an AI-user relationship analyst performing "dreaming" mutual understanding synthesis.
You will receive THREE inputs:
1. **이전 상태**: The previous mutual understanding state (may be empty on first run)
2. **변화 이력**: Rolling log of relationship changes across past cycles (trajectory context)
3. **새로운 시그널**: Recently accumulated relationship signals and contextual facts

Your job: EVOLVE the understanding — don't start from scratch. Build on the previous state,
incorporate new signals, and note what CHANGED. Use the 변화 이력 to identify long-term trends
(e.g., "trust has been increasing over the last 3 cycles" or "recurring frustration about X").

## Analysis Framework

### 사용자 → AI 인식 (user_sees_ai)
Synthesize how the user perceives the AI. Look for:
- Satisfaction trajectory: improving, declining, or stable? Why?
- Trust level: does user verify AI output, or delegate freely?
- Unmet expectations: what does the user want that the AI isn't delivering?
- Emotional tone: warm/collaborative, neutral/transactional, or frustrated/distant?

### AI → 사용자 이해 (ai_understands_user)
Synthesize the AI's accumulated understanding of the user. Include:
- Core personality traits (communication style, decision-making approach)
- Expertise depth map (what they know deeply vs superficially)
- Emotional patterns (what triggers frustration, what brings satisfaction)
- Work rhythm (when they work, how they context-switch, attention patterns)

### 관계 역학 (relationship_dynamics)
Analyze the relationship trajectory:
- Rapport trend: deepening, plateauing, or degrading?
- Communication efficiency: is less explanation needed over time?
- Shared context growth: inside references, assumed knowledge
- Power dynamic: does user lead, collaborate, or delegate?

### 적응 메모 (adaptation_notes)
CONCRETE behavioral directives for the AI. Not vague — specific and actionable:
- "사용자가 X를 물을 때 Y 방식으로 답변할 것" (not "더 잘 답변할 것")
- "Z 상황에서는 확인 없이 바로 실행할 것" (trust-based delegation)
- "W 주제는 간결하게, V 주제는 상세하게" (topic-specific adaptation)

## Output Format
Return a JSON object (Korean values, 2-4 sentences per key):
- "user_sees_ai": "..."
- "ai_understands_user": "..."
- "relationship_dynamics": "..."
- "adaptation_notes": "..."

If a previous state exists, note what evolved (e.g., "이전보다 신뢰가 높아짐: ~").
If insufficient data for a key, omit it.
Return ONLY valid JSON object, no markdown fences.`

func synthesizeMutualUnderstanding(ctx context.Context, store *Store, client *llm.Client, model string, logger *slog.Logger) error {
	facts, err := store.GetActiveFacts(ctx)
	if err != nil {
		return err
	}

	if len(facts) < 5 {
		return nil // not enough data to synthesize
	}

	// Load previous state, history, and user model for context.
	entries, _ := store.GetUserModel(ctx)
	entryMap := make(map[string]string, len(entries))
	for _, e := range entries {
		entryMap[e.Key] = e.Value
	}

	prevState := formatPreviousState(entryMap)
	history := entryMap["mu_history"]

	// Build structured input for the LLM.
	var sb strings.Builder

	// Section 1: Previous state (if exists).
	if prevState != "" {
		fmt.Fprintf(&sb, "## 이전 상태\n%s\n\n", prevState)
	}

	// Section 1.5: Relationship history (multi-cycle trajectory).
	if history != "" {
		fmt.Fprintf(&sb, "## 변화 이력\n%s\n\n", history)
	}

	// Section 2: Phase 5 user profile (cross-phase integration).
	// The user profile informs relationship interpretation:
	// e.g., an expert user being corrected on their domain = strong frustration signal.
	profileKeys := []string{"communication_style", "expertise_areas", "work_patterns"}
	var profileParts []string
	for _, k := range profileKeys {
		if v, ok := entryMap[k]; ok && v != "" {
			profileParts = append(profileParts, fmt.Sprintf("- %s: %s", k, v))
		}
	}
	if len(profileParts) > 0 {
		sb.WriteString("## 사용자 프로필 (Phase 5 참고)\n")
		sb.WriteString(strings.Join(profileParts, "\n"))
		sb.WriteString("\n\n")
	}

	// Section 3: New mutual signals (highest priority).
	sb.WriteString("## 새로운 시그널\n")
	mutualFacts := 0
	for _, f := range facts {
		if f.Category == CategoryMutual && mutualFacts < 25 {
			fmt.Fprintf(&sb, "[mutual, %.1f, %s] %s\n",
				f.Importance, f.CreatedAt.Format("01-02"), f.Content)
			mutualFacts++
		}
	}

	// Section 4: Raw accumulated signals (from between dreaming cycles).
	if raw, ok := entryMap["mu_signals_raw"]; ok && raw != "" {
		fmt.Fprintf(&sb, "\n## 미처리 시그널\n%s\n", raw)
	}

	// Section 5: Supporting context from other categories.
	sb.WriteString("\n## 맥락\n")
	supportCats := map[string]bool{CategoryPreference: true, CategoryUserModel: true, CategoryDecision: true}
	support := 0
	for _, f := range facts {
		if supportCats[f.Category] && support < 15 {
			fmt.Fprintf(&sb, "[%s, %.1f] %s\n", f.Category, f.Importance, f.Content)
			support++
		}
	}

	// Skip LLM call if there's nothing relationship-specific to synthesize.
	// Require at least some mutual signals (facts, raw, or previous state).
	hasRaw := entryMap["mu_signals_raw"] != ""
	if mutualFacts == 0 && !hasRaw && prevState == "" {
		return nil // no relationship data to synthesize
	}

	// Use higher token budget for richer synthesis.
	resp, err := callLLM(ctx, client, model, mutualUnderstandingSystemPrompt, sb.String(), 768)
	if err != nil {
		return err
	}

	var profile map[string]string
	if err := json.Unmarshal([]byte(stripCodeFences(resp)), &profile); err != nil {
		return nil // non-fatal
	}

	mutualKeys := map[string]bool{
		"user_sees_ai":          true,
		"ai_understands_user":   true,
		"relationship_dynamics": true,
		"adaptation_notes":      true,
	}

	updated := 0
	for key, value := range profile {
		if value == "" || !mutualKeys[key] {
			continue
		}
		if err := store.SetUserModel(ctx, key, value, 0.85); err != nil {
			logger.Debug("aurora-dream: failed to set mutual understanding", "key", key, "error", err)
		} else {
			updated++
		}
	}

	// Clear consumed mu_signals_raw after successful synthesis.
	if updated > 0 {
		_ = store.SetUserModel(ctx, "mu_signals_raw", "", 0)

		// Append a history snapshot: concise summary of what changed this cycle.
		appendRelationshipHistory(ctx, store, entryMap, profile, logger)

		logger.Info("aurora-dream: updated mutual understanding", "keys", updated, "signals_consumed", mutualFacts)
	}

	return nil
}

// appendRelationshipHistory maintains a rolling log of relationship evolution
// in the "mu_history" user_model key. Keeps the last 8 entries to stay concise
// while preserving multi-cycle trajectory visibility.
// entryMap is the already-loaded user_model map to avoid redundant DB reads.
func appendRelationshipHistory(ctx context.Context, store *Store, entryMap map[string]string, profile map[string]string, logger *slog.Logger) {
	// Build a one-line snapshot from the current synthesis.
	date := time.Now().Format("01-02")
	var parts []string
	if v := profile["relationship_dynamics"]; v != "" {
		// Take first sentence only for compactness.
		if idx := strings.IndexAny(v, ".。"); idx > 0 && idx < 80 {
			parts = append(parts, v[:idx+1])
		} else if len(v) > 80 {
			parts = append(parts, v[:80]+"…")
		} else {
			parts = append(parts, v)
		}
	}
	if v := profile["adaptation_notes"]; v != "" {
		// Extract first actionable note.
		lines := strings.SplitN(v, "\n", 2)
		first := strings.TrimLeft(lines[0], "- •")
		first = strings.TrimSpace(first)
		if first != "" && len(first) > 3 {
			if len(first) > 60 {
				first = first[:60] + "…"
			}
			parts = append(parts, "적응: "+first)
		}
	}

	if len(parts) == 0 {
		return
	}

	entry := fmt.Sprintf("[%s] %s", date, strings.Join(parts, " | "))

	// Use already-loaded history from entryMap instead of re-reading DB.
	existing := entryMap["mu_history"]

	var history string
	if existing != "" {
		history = existing + "\n" + entry
	} else {
		history = entry
	}

	// Keep only last 8 entries for bounded growth.
	lines := strings.Split(history, "\n")
	const maxHistoryEntries = 8
	if len(lines) > maxHistoryEntries {
		lines = lines[len(lines)-maxHistoryEntries:]
		history = strings.Join(lines, "\n")
	}

	if err := store.SetUserModel(ctx, "mu_history", history, 0.9); err != nil {
		logger.Debug("aurora-dream: failed to append relationship history", "error", err)
	}
}

// formatPreviousState formats the current mutual understanding keys as
// context for the next synthesis cycle. Takes a pre-built map to avoid
// redundant DB reads when the caller already loaded entries.
func formatPreviousState(entryMap map[string]string) string {
	labels := map[string]string{
		"user_sees_ai":          "사용자 → AI 인식",
		"ai_understands_user":   "AI → 사용자 이해",
		"relationship_dynamics": "관계 역학",
		"adaptation_notes":      "적응 메모",
	}
	// Ordered iteration for deterministic output.
	order := []string{"user_sees_ai", "ai_understands_user", "relationship_dynamics", "adaptation_notes"}

	var sb strings.Builder
	for _, key := range order {
		label := labels[key]
		if v, ok := entryMap[key]; ok && v != "" {
			fmt.Fprintf(&sb, "- %s: %s\n", label, v)
		}
	}
	return sb.String()
}

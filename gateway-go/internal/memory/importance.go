// importance.go — Structured fact extraction with importance scoring via SGLang.
// Inspired by Honcho's Neuromancer inference layer: every ~1000 tokens,
// evaluate the conversation for facts worth remembering, with structured
// category and importance scoring.
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

const (
	importanceTimeout   = 30 * time.Second
	importanceMaxTokens = 512
	// TokenThreshold is the number of conversation tokens between importance evaluations.
	TokenThreshold = 1000
)

// ExtractedFact is the structured output from the importance extraction LLM call.
type ExtractedFact struct {
	Content    string  `json:"content"`
	Category   string  `json:"category"`
	Importance float64 `json:"importance"`
	ExpiryHint string  `json:"expiry_hint,omitempty"` // ISO8601 or empty
}

// importanceSystemPrompt uses Honcho Neuromancer XR-style reasoning:
// 1. Explicit extraction — what was directly stated
// 2. Deductive reasoning — what can be logically inferred but wasn't said
// 3. Structured output with category, importance, and confidence
const importanceSystemPrompt = `You are Neuromancer, a memory inference engine for an AI agent system.
Your job is NOT just to store what was said, but to REASON about what matters.

## Reasoning Process (follow this order)

### Step 1: Explicit Extraction
What facts were directly stated? Look for:
- Decisions made (architecture, tool, config choices)
- Preferences expressed (communication style, language, workflow)
- Solutions found (problem → fix mapping)
- Technical context established

### Step 2: Deductive Reasoning
What can be LOGICALLY INFERRED but was NOT directly said?
- If user chose tool X over Y → they likely value X's properties
- If user solved problem in way Z → they have expertise in Z's domain
- If user asked about topic T repeatedly → T is an area of active work
- If user corrected the AI on X → X is a strong preference, not casual

### Step 3: Output
Return a JSON array. For each fact:
- "content": Korean, concise (1-2 sentences). Include the reasoning basis
- "category": one of:
  - "decision": choices made (explicit or inferred from actions)
  - "preference": work style, communication, tool preferences
  - "solution": problem-solution pairs
  - "context": project/technical state that affects future interactions
  - "user_model": expertise areas, personality, habits (INFERRED)
- "importance": 0.0-1.0
  - 0.9+: decisions that constrain future work, core identity traits
  - 0.7-0.9: reusable solutions, strong preferences
  - 0.5-0.7: useful context, weak signals
- "expiry_hint": null or "YYYY-MM-DD" if time-sensitive

## Rules
- Max 5 facts. Quality over quantity
- Include at least 1 deductive inference if the conversation has substance
- If nothing worth remembering, return []
- Return ONLY valid JSON array, no markdown fences, no explanation`

// ExtractFacts analyzes a conversation segment and returns structured facts.
// Falls back to legacy bullet-point extraction if JSON parsing fails.
func ExtractFacts(ctx context.Context, client *llm.Client, model string, userMessage, agentResponse string, logger *slog.Logger) ([]ExtractedFact, error) {
	ctx, cancel := context.WithTimeout(ctx, importanceTimeout)
	defer cancel()

	prompt := fmt.Sprintf("User:\n%s\n\nAssistant:\n%s",
		truncate(userMessage, 4000),
		truncate(agentResponse, 8000))

	text, err := callSglang(ctx, client, model, importanceSystemPrompt, prompt, importanceMaxTokens)
	if err != nil {
		return nil, fmt.Errorf("importance extraction: %w", err)
	}
	if text == "" || text == "[]" {
		return nil, nil
	}

	// Strip markdown code fences if present.
	text = stripCodeFences(text)

	var facts []ExtractedFact
	if err := json.Unmarshal([]byte(text), &facts); err != nil {
		logger.Debug("importance: JSON parse failed, trying fallback", "error", err, "raw", text)
		return parseBulletFallback(text), nil
	}

	// Validate and clamp values.
	var valid []ExtractedFact
	for _, f := range facts {
		if f.Content == "" {
			continue
		}
		f.Importance = clamp(f.Importance, 0, 1)
		if !isValidCategory(f.Category) {
			f.Category = CategoryContext
		}
		valid = append(valid, f)
	}

	return valid, nil
}

// InsertExtractedFacts stores extracted facts in the memory store and embeds them.
func InsertExtractedFacts(ctx context.Context, store *Store, embedder *Embedder, facts []ExtractedFact, logger *slog.Logger) {
	for _, ef := range facts {
		var expiresAt *time.Time
		if ef.ExpiryHint != "" {
			if t, err := time.Parse(time.RFC3339, ef.ExpiryHint); err == nil {
				expiresAt = &t
			} else if t, err := time.Parse("2006-01-02", ef.ExpiryHint); err == nil {
				expiresAt = &t
			}
		}

		fact := Fact{
			Content:    ef.Content,
			Category:   ef.Category,
			Importance: ef.Importance,
			Source:     SourceAutoExtract,
			ExpiresAt:  expiresAt,
		}

		id, err := store.InsertFact(ctx, fact)
		if err != nil {
			logger.Warn("aurora-memory: failed to insert fact", "error", err, "content", truncate(ef.Content, 50))
			continue
		}

		// Embed asynchronously (best-effort).
		if embedder != nil {
			go func(factID int64, content string) {
				embedCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				if err := embedder.EmbedAndStore(embedCtx, factID, content); err != nil {
					logger.Debug("aurora-memory: embedding failed", "fact_id", factID, "error", err)
				}
			}(id, ef.Content)
		}

		// If this is a user_model fact, also update the user model table.
		if ef.Category == CategoryUserModel {
			updateUserModelFromFact(ctx, store, ef, logger)
		}

		logger.Info("aurora-memory: stored fact",
			"id", id,
			"category", ef.Category,
			"importance", fmt.Sprintf("%.2f", ef.Importance),
			"content", truncate(ef.Content, 80),
		)
	}
}

// updateUserModelFromFact infers user model key-value from a user_model category fact.
func updateUserModelFromFact(ctx context.Context, store *Store, fact ExtractedFact, logger *slog.Logger) {
	// Simple heuristic: use the fact content as a value for a general "traits" key.
	// The dreaming engine will later consolidate these into proper keys.
	key := "traits"
	existing, _ := store.GetMeta(ctx, "user_model_traits")
	var value string
	if existing != "" {
		value = existing + "\n" + fact.Content
	} else {
		value = fact.Content
	}
	if err := store.SetUserModel(ctx, key, value, fact.Importance); err != nil {
		logger.Debug("aurora-memory: failed to update user model", "error", err)
	}
}

// --- Helpers ---

func stripCodeFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```json") {
		s = strings.TrimPrefix(s, "```json")
	} else if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
	}
	if strings.HasSuffix(s, "```") {
		s = strings.TrimSuffix(s, "```")
	}
	return strings.TrimSpace(s)
}

func isValidCategory(c string) bool {
	switch c {
	case CategoryDecision, CategoryPreference, CategorySolution, CategoryContext, CategoryUserModel:
		return true
	}
	return false
}

func clamp(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// parseBulletFallback handles the legacy unstructured bullet-point format.
func parseBulletFallback(text string) []ExtractedFact {
	var facts []ExtractedFact
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
			content := strings.TrimPrefix(strings.TrimPrefix(line, "- "), "* ")
			if content != "" {
				facts = append(facts, ExtractedFact{
					Content:    content,
					Category:   CategoryContext,
					Importance: 0.5,
				})
			}
		}
	}
	return facts
}

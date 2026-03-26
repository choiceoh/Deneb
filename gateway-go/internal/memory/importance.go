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

const importanceSystemPrompt = `You are a memory extraction assistant for an AI agent system.
Given a conversation segment, extract ONLY genuinely important facts worth remembering across sessions.

Return a JSON array. For each fact:
- "content": the fact in Korean, concise (1-2 sentences max)
- "category": one of ["decision", "preference", "solution", "context", "user_model"]
  - decision: architecture choices, tool selections, config changes
  - preference: user communication/work style preferences
  - solution: problems solved and their solutions
  - context: important project/technical context
  - user_model: user expertise, personality traits, habits
- "importance": 0.0-1.0 (how reusable across future sessions)
  - 0.9+: critical decisions, core preferences
  - 0.7-0.9: useful solutions, important context
  - 0.5-0.7: minor context, temporary info
  - <0.5: probably not worth storing
- "expiry_hint": null or ISO8601 date if the fact is time-sensitive

If nothing is worth remembering, return an empty array [].
Be very selective — only truly reusable knowledge. Max 5 facts per extraction.
IMPORTANT: Return ONLY valid JSON array, no markdown fences, no explanation.`

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
			logger.Warn("memory: failed to insert fact", "error", err, "content", truncate(ef.Content, 50))
			continue
		}

		// Embed asynchronously (best-effort).
		if embedder != nil {
			go func(factID int64, content string) {
				embedCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				if err := embedder.EmbedAndStore(embedCtx, factID, content); err != nil {
					logger.Debug("memory: embedding failed", "fact_id", factID, "error", err)
				}
			}(id, ef.Content)
		}

		// If this is a user_model fact, also update the user model table.
		if ef.Category == CategoryUserModel {
			updateUserModelFromFact(ctx, store, ef, logger)
		}

		logger.Info("memory: stored fact",
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
		logger.Debug("memory: failed to update user model", "error", err)
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

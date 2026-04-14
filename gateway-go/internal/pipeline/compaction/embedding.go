package compaction

import (
	"context"
	"fmt"
	"log/slog"
	"math"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// Embedder produces dense vector embeddings for text.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// MMR tuning parameters.
const (
	// mmrLambda balances relevance vs diversity.
	// Higher = more relevance-focused, lower = more diversity.
	mmrLambda = 0.6

	// mmrMinOldMessages: skip embedding compaction if too few old messages.
	mmrMinOldMessages = 4

	// mmrMaxEmbedBatch: max texts to embed in one call.
	mmrMaxEmbedBatch = 128
)

// EmbeddingCompact uses BGE-M3 embeddings + MMR (Maximal Marginal Relevance)
// to select the most relevant and diverse subset of old messages.
//
// Strategy:
//  1. Split messages into old (candidates) and recent (always kept).
//  2. Embed all old messages and the recent context.
//  3. Compute a query centroid from recent message embeddings.
//  4. Greedily select old messages via MMR until token budget is reached.
//  5. Rebuild: selected old messages (in original order) + recent messages.
//
// This is extractive compaction — no generation, just selection.
func EmbeddingCompact(
	ctx context.Context,
	cfg Config,
	messages []llm.Message,
	embedder Embedder,
	logger *slog.Logger,
) ([]llm.Message, bool) {
	// Same split logic as LLM compaction.
	splitIdx := findSplitPoint(messages, keepRecentTurns)
	if splitIdx < mmrMinOldMessages {
		return messages, false
	}

	old := messages[:splitIdx]
	recent := messages[splitIdx:]

	oldTexts := make([]string, len(old))
	for i, msg := range old {
		oldTexts[i] = serializeSingleMessage(msg)
	}
	recentTexts := make([]string, len(recent))
	for i, msg := range recent {
		recentTexts[i] = serializeSingleMessage(msg)
	}

	// Truncate old messages first if total exceeds batch limit.
	// Recent texts are always preserved (they form the query centroid).
	if len(oldTexts)+len(recentTexts) > mmrMaxEmbedBatch {
		maxOld := mmrMaxEmbedBatch - len(recentTexts)
		if maxOld < mmrMinOldMessages {
			return messages, false
		}
		old = old[:maxOld]
		oldTexts = oldTexts[:maxOld]
	}

	allTexts := make([]string, 0, len(oldTexts)+len(recentTexts))
	allTexts = append(allTexts, oldTexts...)
	allTexts = append(allTexts, recentTexts...)

	embeddings, err := embedder.Embed(ctx, allTexts)
	if err != nil {
		if logger != nil {
			logger.Warn("polaris: embedding compaction failed", "error", err)
		}
		return messages, false
	}

	oldEmbeddings := embeddings[:len(oldTexts)]
	recentEmbeddings := embeddings[len(oldTexts):]

	queryCentroid := centroid(recentEmbeddings)
	if queryCentroid == nil {
		return messages, false
	}

	// Token budget for selected old messages: total budget minus recent tokens.
	recentTokens := EstimateMessagesTokens(recent)
	oldBudget := int(float64(cfg.ContextBudget)*DefaultLLMThresholdPct) - recentTokens
	if oldBudget <= 0 {
		return messages, false
	}

	// MMR selection: greedily pick old messages.
	selected := mmrSelect(oldEmbeddings, queryCentroid, old, oldBudget)
	if len(selected) == 0 || len(selected) >= len(old) {
		// Selected everything or nothing — no compaction benefit.
		return messages, false
	}

	// Rebuild: marker + selected old (original order) + recent.
	compacted := make([]llm.Message, 0, 1+len(selected)+len(recent))
	compacted = append(compacted, llm.NewTextMessage("user",
		fmt.Sprintf("[Polaris embedding compaction (MMR): %d/%d messages selected]",
			len(selected), len(old))))
	compacted = append(compacted, selected...)
	compacted = append(compacted, recent...)
	compacted = mergeConsecutiveSameRole(compacted)

	if logger != nil {
		logger.Info("polaris: embedding compaction applied",
			"selected", len(selected),
			"total", len(old),
			"recentKept", len(recent),
			"tokensBefore", EstimateMessagesTokens(messages),
			"tokensAfter", EstimateMessagesTokens(compacted))
	}
	return compacted, true
}

// mmrSelect greedily selects messages using Maximal Marginal Relevance.
//
// MMR(msg) = λ * sim(msg, query) - (1-λ) * max(sim(msg, selected_j))
//
// Messages are selected in order of MMR score until tokenBudget is exhausted.
// Returns messages in their original chronological order.
func mmrSelect(
	embeddings [][]float32,
	query []float32,
	messages []llm.Message,
	tokenBudget int,
) []llm.Message {
	n := len(embeddings)

	// Pre-compute relevance scores (similarity to query centroid).
	relevance := make([]float64, n)
	for i := range embeddings {
		relevance[i] = cosineSim(embeddings[i], query)
	}

	selectedIndices := make([]int, 0, n)
	selectedSet := make([]bool, n)
	usedTokens := 0

	for usedTokens < tokenBudget && len(selectedIndices) < n {
		bestIdx := -1
		bestScore := math.Inf(-1)

		for i := range n {
			if selectedSet[i] {
				continue
			}

			// Diversity penalty: max similarity to any already-selected message.
			maxSimToSelected := math.Inf(-1)
			if len(selectedIndices) == 0 {
				maxSimToSelected = 0 // no penalty for the first pick
			}
			for _, si := range selectedIndices {
				sim := cosineSim(embeddings[i], embeddings[si]) //nolint:gosec // G602: si is always a valid index from prior iterations
				if sim > maxSimToSelected {
					maxSimToSelected = sim
				}
			}

			score := mmrLambda*relevance[i] - (1-mmrLambda)*maxSimToSelected
			if score > bestScore {
				bestScore = score
				bestIdx = i
			}
		}

		if bestIdx < 0 {
			break
		}

		msgTokens := EstimateTokens(string(messages[bestIdx].Content)) + 4 //nolint:gosec // G602: bestIdx is validated (0 <= bestIdx < n)
		if usedTokens+msgTokens > tokenBudget && len(selectedIndices) > 0 {
			break // would exceed budget
		}

		selectedSet[bestIdx] = true
		selectedIndices = append(selectedIndices, bestIdx)
		usedTokens += msgTokens
	}

	// Return in original chronological order.
	result := make([]llm.Message, 0, len(selectedIndices))
	for i := range n {
		if selectedSet[i] {
			result = append(result, messages[i])
		}
	}
	return result
}

// --- vector operations ---

// centroid computes the mean of a set of vectors.
func centroid(vectors [][]float32) []float32 {
	if len(vectors) == 0 {
		return nil
	}
	dim := len(vectors[0])
	result := make([]float32, dim)
	for _, v := range vectors {
		for i, val := range v {
			result[i] += val
		}
	}
	n := float32(len(vectors))
	for i := range result {
		result[i] /= n
	}
	return result
}

// cosineSim computes cosine similarity between two vectors.
func cosineSim(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		ai, bi := float64(a[i]), float64(b[i])
		dot += ai * bi
		normA += ai * ai
		normB += bi * bi
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}

// serializeSingleMessage converts a single message to text for embedding.
func serializeSingleMessage(msg llm.Message) string {
	// Reuse serializeMessages for a single-element slice.
	return serializeMessages([]llm.Message{msg})
}

// mergeConsecutiveSameRole merges adjacent messages with the same role by
// concatenating their text content. This repairs role-alternation violations
// that can arise when extractive compaction (MMR) skips messages, leaving
// two consecutive user or assistant turns in the result.
func mergeConsecutiveSameRole(messages []llm.Message) []llm.Message {
	if len(messages) <= 1 {
		return messages
	}
	result := make([]llm.Message, 0, len(messages))
	result = append(result, messages[0])
	for _, msg := range messages[1:] {
		last := result[len(result)-1]
		if msg.Role != last.Role {
			result = append(result, msg)
			continue
		}
		// Same role: concatenate text content and replace the last entry.
		merged := llm.ExtractSystemText(last.Content) + "\n\n" + llm.ExtractSystemText(msg.Content)
		result[len(result)-1] = llm.NewTextMessage(msg.Role, merged)
	}
	return result
}

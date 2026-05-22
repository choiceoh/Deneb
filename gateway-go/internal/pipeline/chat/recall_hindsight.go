// recall_hindsight.go is the read path for the Hindsight memory provider.
// It contributes one more evidence source to buildRecallPreflight: when the
// user's message implies past context, the self-hosted Hindsight bank is
// queried alongside wiki/diary/transcript/session search and its hits are
// merged into the same <recall-context> block.
package chat

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/hindsight"
)

const (
	// recallHindsightMaxQueryRunes caps the query length. Hindsight rejects
	// overly long queries (its recall_max_input_chars default is 800).
	recallHindsightMaxQueryRunes = 800
	// recallHindsightMaxResults caps how many bank hits enter the evidence
	// pool, leaving room for wiki/diary/session sources under recallMaxEvidence.
	recallHindsightMaxResults = 5
)

// recallHindsightEvidence queries the Hindsight memory bank with the raw user
// message. Hindsight does semantic + keyword (hybrid) search, so it is fed the
// natural-language message directly rather than the keyword-extracted terms
// the BM25/FTS sources use. The caller's context carries the preflight
// deadline; a timeout degrades gracefully to no evidence.
func recallHindsightEvidence(ctx context.Context, client *hindsight.Client, message string, logger *slog.Logger) []recallEvidence {
	if client == nil {
		return nil
	}
	query := strings.TrimSpace(message)
	if query == "" {
		return nil
	}
	if runes := []rune(query); len(runes) > recallHindsightMaxQueryRunes {
		query = strings.TrimSpace(string(runes[:recallHindsightMaxQueryRunes]))
	}

	memories, err := client.Recall(ctx, query)
	if err != nil {
		// A cancelled context is the preflight budget expiring, not a fault,
		// so only the real failures are worth a log line.
		if logger != nil && ctx.Err() == nil {
			logger.Warn("recall preflight: hindsight recall failed", "error", err)
		}
		return nil
	}

	evidence := make([]recallEvidence, 0, len(memories))
	for i, m := range memories {
		note := m.Text
		if m.Context != "" {
			note = "context: " + m.Context + " | " + note
		}
		evidence = append(evidence, recallEvidence{
			Kind:   "hindsight",
			Source: hindsightEvidenceSource(m),
			Note:   truncateRecallText(note, 420),
			Score:  recallHindsightScore(i),
			At:     parseRecallTimestamp(m.MentionedAt, m.OccurredAt),
		})
		if len(evidence) >= recallHindsightMaxResults {
			break
		}
	}
	if logger != nil && len(evidence) > 0 {
		logger.Info("recall preflight: hindsight evidence injected", "count", len(evidence))
	}
	return evidence
}

// recallHindsightScore assigns a rank-decayed score. Hindsight returns
// curated, reranked memories, so the top hit ranks alongside wiki evidence
// while later hits settle near session/transcript evidence.
func recallHindsightScore(rank int) float64 {
	score := 0.92 - 0.05*float64(rank)
	if score < 0.60 {
		return 0.60
	}
	return score
}

// hindsightEvidenceSource builds a stable ref label for an evidence row.
func hindsightEvidenceSource(m hindsight.Memory) string {
	if m.Type != "" {
		return "hindsight/" + m.Type
	}
	return "hindsight"
}

// parseRecallTimestamp converts the first parseable ISO 8601 timestamp into
// epoch millis, matching the At field used by other recall evidence. Returns
// 0 (rendered as age=unknown) when nothing parses.
func parseRecallTimestamp(values ...string) int64 {
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t.UnixMilli()
		}
	}
	return 0
}

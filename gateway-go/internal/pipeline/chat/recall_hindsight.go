// recall_hindsight.go is the read path for the Hindsight memory provider.
// It contributes one more evidence source to buildRecallPreflight. Unlike the
// cue-gated wiki/diary/transcript/session sources, Hindsight auto-recalls on
// every turn (the Hermes auto_recall model): the self-hosted bank is queried
// with the current message regardless of cue, and its hits are merged into
// the same <recall-context> block.
package chat

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/hindsight"
)

const (
	// recallHindsightMaxQueryRunes caps the query length. The bank enforces a
	// server-side TOKEN limit (500 by default), not a rune limit — and Korean
	// runs at roughly one token per rune, so the previous 800-rune cap blew
	// straight past it: every long Korean turn got a 400 ("682 tokens exceeds
	// maximum of 500") and hindsight contributed nothing on exactly the
	// context-rich turns it exists for. 400 runes keeps Korean-heavy queries
	// comfortably under the limit.
	recallHindsightMaxQueryRunes = 400
	// recallHindsightMaxResults caps how many bank hits enter the evidence
	// pool, leaving room for wiki/diary/session sources under recallMaxEvidence.
	recallHindsightMaxResults = 5
	// recallHindsightStaleAfter: facts older than this carry a soft staleness
	// hint. The bank has no supersession (unlike wiki SupersededBy/Archived) — a
	// changed fact leaves the old one recallable with no "stale" cue — so age is
	// the only proxy. Conservative + phrased as "verify" so an old-but-true fact
	// costs only a harmless check, not a wrong "superseded" claim.
	recallHindsightStaleAfter = 90 * 24 * time.Hour
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
	query := clipRunesAtBoundary(strings.TrimSpace(message), recallHindsightMaxQueryRunes)
	if query == "" {
		return nil
	}

	memories, err := client.Recall(ctx, query)
	if err != nil && ctx.Err() == nil && isHindsightQueryTooLong(err) {
		// The bank's token limit is server-configurable, so the rune cap can
		// still overshoot on token-dense content. Halve once and retry rather
		// than losing the turn's hindsight contribution entirely.
		query = clipRunesAtBoundary(query, len([]rune(query))/2)
		memories, err = client.Recall(ctx, query)
	}
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
		at := parseRecallTimestamp(m.MentionedAt, m.OccurredAt)
		// Mirror recallWikiStalenessMarker: an old fact gets a "may have changed,
		// verify" cue so the model won't cite an outdated bank value as current.
		if marker := recallHindsightStalenessMarker(at); marker != "" {
			note = marker + " " + note
		}
		evidence = append(evidence, recallEvidence{
			Kind:   "hindsight",
			Source: hindsightEvidenceSource(m),
			Note:   truncateRecallText(note, 420),
			Score:  recallHindsightScore(i),
			At:     at,
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

// recallHindsightStalenessMarker returns an inline marker when a recalled
// Hindsight fact is older than recallHindsightStaleAfter, mirroring
// recallWikiStalenessMarker. at is a UnixMilli timestamp; 0 (unknown) yields no
// marker since age can't be judged.
func recallHindsightStalenessMarker(at int64) string {
	if at <= 0 {
		return ""
	}
	if time.Since(time.UnixMilli(at)) < recallHindsightStaleAfter {
		return ""
	}
	return "⚠ 오래된 기억(변경됐을 수 있으니 현행으로 단정 말고 필요시 도구로 확인)"
}

// isHindsightQueryTooLong matches the bank's over-limit rejection.
func isHindsightQueryTooLong(err error) bool {
	return err != nil && strings.Contains(err.Error(), "Query too long")
}

// clipRunesAtBoundary truncates s to at most maxRunes, backing up to the last
// whitespace so the hybrid search never sees a half-cut word.
func clipRunesAtBoundary(s string, maxRunes int) string {
	runes := []rune(s)
	if maxRunes <= 0 || len(runes) <= maxRunes {
		return strings.TrimSpace(s)
	}
	cut := maxRunes
	for i := maxRunes - 1; i > maxRunes/2; i-- {
		if runes[i] == ' ' || runes[i] == '\n' || runes[i] == '\t' {
			cut = i
			break
		}
	}
	return strings.TrimSpace(string(runes[:cut]))
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

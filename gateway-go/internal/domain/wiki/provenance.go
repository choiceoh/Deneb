// provenance.go — episode provenance for LLM-built wiki knowledge.
//
// The dreamer synthesizes wiki pages from raw diary spans, but the link back to
// *which* span produced a fact used to be dropped once the page was written —
// leaving the knowledge graph with no way to cite, verify, or invalidate a
// fact against its source (the "citation needed" problem for LLM-built graphs).
//
// An "episode" here is one dream cycle's consumed diary span, addressed by a
// stable ref: the diary date (locates the raw file) plus a content digest of
// the exact bytes synthesized (pins/verifies the span, dedupes re-processing).
// Every page records the episodes that created or last touched its facts;
// graph_snapshot.go then projects those refs into the graph as real provenance.
package wiki

import (
	"fmt"
	"hash/fnv"
	"strings"
)

// maxSources caps the per-page episode list. Pages accrete across many dream
// cycles; without a cap the provenance list would grow unbounded on hot pages.
// The tail (most recent episodes) is kept — old episodes stay resolvable via
// the diary itself, and recency is what a "where did this last come from?"
// query wants.
const maxSources = 8

// episodeHash returns a short, stable, non-crypto digest of a diary span. fnv
// (not sha256) because this addresses a source span, not a secret — cheap and
// deterministic is all that's needed.
func episodeHash(s string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return fmt.Sprintf("%08x", h.Sum32())
}

// newEpisodeRef mints the provenance token for a dream cycle. The episode is
// the cycle's whole consumed batch (which may span several diary files and
// MEMORY.md), not a single file: synthesis emits page updates without telling
// us which input line produced which fact, so per-file attribution is not
// available without LLM cooperation we deliberately avoid. Accordingly the
// DIGEST — taken over the full batch — is the episode's identity (recompute it
// from the same batch to verify); diaryDate is the batch's latest diary date,
// a coarse human locator, not a claim that the file is the sole source.
//
// Returns "" when there is nothing to attribute (empty span), so callers never
// stamp a hollow ref. The token carries no comma/space, so it is safe inside
// the "[a, b, c]" frontmatter flow array (see sanitizeFlowItems).
func newEpisodeRef(diaryDate, content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	hash := episodeHash(content)
	if date := strings.TrimSpace(diaryDate); date != "" {
		return "d" + date + "#" + hash
	}
	return "ep-" + hash
}

// normalizeSources trims, drops empties, dedupes (first occurrence wins), and
// caps to the most recent maxSources episodes. Enforced at write time
// (writePageFile) so every producer lands within the same bounds regardless of
// which path appended.
func normalizeSources(sources []string) []string {
	seen := make(map[string]struct{}, len(sources))
	out := make([]string, 0, len(sources))
	for _, s := range sources {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	if len(out) > maxSources {
		out = out[len(out)-maxSources:] // keep the newest window
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// appendEpisode records a new episode ref on a page's provenance list, keeping
// it normalized and bounded. An empty ref (nothing to attribute) is a no-op.
func appendEpisode(existing []string, ref string) []string {
	if ref == "" {
		return existing
	}
	return normalizeSources(append(existing, ref))
}

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
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// diaryDateRe is the strict diary-date shape (YYYY-MM-DD). Episode refs and the
// resolved diary filename both feed a filesystem path, so a ref extracted from
// an LLM-written page's frontmatter must be validated against this before it can
// name a file — otherwise a crafted "sources: [d../../etc#x]" would escape the
// diary dir. Anchored, digits-and-dashes only, no separators.
var diaryDateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// IsDiaryDate reports whether s is a well-formed YYYY-MM-DD diary date. Callers
// that turn an agent- or page-supplied date into a diary file path MUST gate on
// it first (path-traversal guard).
func IsDiaryDate(s string) bool {
	return diaryDateRe.MatchString(s)
}

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

// parseEpisodeRef is the inverse of newEpisodeRef: it splits a ref into its
// diary date and content hash. ok is false for anything that is neither a
// "d<date>#<hash>" nor a dateless "ep-<hash>" token, OR whose date is not a
// well-formed YYYY-MM-DD (the date reaches a file path downstream, so a
// malformed one must not resolve). The date is "" for the dateless form.
func parseEpisodeRef(ref string) (date, hash string, ok bool) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", "", false
	}
	if h, found := strings.CutPrefix(ref, "ep-"); found {
		if h == "" {
			return "", "", false
		}
		return "", h, true
	}
	body, found := strings.CutPrefix(ref, "d")
	if !found {
		return "", "", false
	}
	date, hash, found = strings.Cut(body, "#")
	if !found || hash == "" || !IsDiaryDate(date) {
		return "", "", false
	}
	return date, hash, true
}

// EpisodeSource is a resolved episode ref: which diary the fact came from and
// whether that diary file is still on disk. It is the coarse-but-honest answer
// to "이 사실 출처가 뭐야?" — the episode is a whole dream-cycle batch, so this
// points at the batch's diary date, not an exact line span (see newEpisodeRef).
type EpisodeSource struct {
	Ref       string // the original episode ref, verbatim
	Date      string // YYYY-MM-DD; "" for a dateless ep-<hash> ref
	DiaryFile string // "diary-<date>.md"; "" when Date is empty
	Exists    bool   // whether DiaryFile is present under the diary dir
	Malformed bool   // true when Ref did not parse as an episode token
}

// ResolveEpisode maps an episode ref back to its diary source. It locates the
// diary file by date (the diary is date-named) and reports whether it still
// exists — a dangling ref (diary rotated away) is itself useful to surface. It
// deliberately does NOT try to re-derive the exact consumed span: the digest is
// over the whole cycle batch (diary + MEMORY.md) and only verifies a candidate,
// it does not index back to one. Read-only; never fails.
func (s *Store) ResolveEpisode(ref string) EpisodeSource {
	src := EpisodeSource{Ref: strings.TrimSpace(ref)}
	date, _, ok := parseEpisodeRef(ref)
	if !ok {
		src.Malformed = true
		return src
	}
	if date == "" {
		return src // dateless ep-<hash>: content-addressed only, no diary file
	}
	src.Date = date
	src.DiaryFile = "diary-" + date + ".md"
	if dir := s.DiaryDir(); dir != "" {
		if info, err := os.Stat(filepath.Join(dir, src.DiaryFile)); err == nil && !info.IsDir() {
			src.Exists = true
		}
	}
	return src
}

// ResolveEpisodes resolves a page's whole provenance list, preserving order.
func (s *Store) ResolveEpisodes(refs []string) []EpisodeSource {
	if len(refs) == 0 {
		return nil
	}
	out := make([]EpisodeSource, 0, len(refs))
	for _, ref := range refs {
		out = append(out, s.ResolveEpisode(ref))
	}
	return out
}

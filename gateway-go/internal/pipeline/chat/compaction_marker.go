// compaction_marker.go provides session-level marking of summary-producing
// compaction events (P4). When polaris.Compact runs a tier that introduces
// summary placeholders into the message slice (LLM, Embedding, Recency, or
// Emergency eviction), we set Session.CompactionFired so the next turn's
// system prompt includes a one-time-per-session reminder for the model.
//
// Cheap-pruning tiers (Micro fence-strip, TruncateOldToolResults stub) do
// NOT set this — they only shrink existing content without introducing a
// summary placeholder the model needs to be told about.
package chat

import (
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/compaction"
)

// markCompactionFired sets Session.CompactionFired when a polaris result
// indicates a summary-producing tier ran. The flag is sticky — once set,
// it stays set until session deletion (e.g. /reset). Re-setting is a
// cheap no-op (we read first and short-circuit). Safe to call when the
// session manager or the session itself is missing.
//
// Cache implication: the system prompt's dynamic block gains an extra
// reminder paragraph from the next turn onward. That is one cache miss
// at the moment the flag flips; from then on the dynamic block bytes
// stay identical so trailing message markers continue to hit.
func markCompactionFired(deps runDeps, sessionKey string, result compaction.Result) {
	if sessionKey == "" || deps.sessions == nil {
		return
	}
	if !compactionProducedSummary(result) {
		return
	}
	sess := deps.sessions.Get(sessionKey)
	if sess == nil || sess.CompactionFired {
		return
	}
	sess.CompactionFired = true
	_ = deps.sessions.Set(sess) // best-effort: in-memory store, error unreachable
}

// compactionProducedSummary reports whether a polaris result reflects a
// summary-producing tier (LLM, Embedding, Recency, or Emergency eviction).
// Cheap-pruning tiers (MicroPruned, OldToolResultsStubbed) do not count —
// they shrink existing content without introducing summary placeholders.
func compactionProducedSummary(r compaction.Result) bool {
	return r.LLMCompacted || r.EmbeddingCompacted || r.RecencyCompacted || r.EmergencyEvicted > 0
}

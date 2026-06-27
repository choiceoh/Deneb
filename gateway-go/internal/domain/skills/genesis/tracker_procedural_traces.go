// tracker_procedural_traces.go — append-only corpus of successful session
// tool-call sequences, the raw material MineRecurringToolSequences consumes.
// Mirrors the validation-case store: append-only JSONL for auditability,
// dedupe-on-read so a session recorded repeatedly (the Nudger fires mid-session
// as the sequence grows) contributes once, with its fullest sequence winning.
package genesis

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

const (
	// proceduralTraceMinTools is the shortest successful tool sequence worth
	// recording — below this there is no multi-step procedure to mine.
	proceduralTraceMinTools = 2
	// proceduralTraceMaxTools caps a single recorded sequence so one pathological
	// session cannot bloat a row of the append-only pool.
	proceduralTraceMaxTools = 40
	// proceduralTraceScanLimit bounds how many recent sessions the mining read
	// considers, so the corpus growing over months stays a bounded read.
	proceduralTraceScanLimit = 2000
)

// RecordProceduralTrace appends one session's successful tool-call sequence to
// the corpus mined for recurring procedural structure. Best-effort and
// fail-safe: a too-short sequence or empty key is silently dropped, and a write
// error is returned for the caller to log — it must never block a turn. The
// store is append-only; RecentProceduralTraces dedupes by session on read.
func (t *Tracker) RecordProceduralTrace(sessionKey string, tools []string) error {
	sessionKey = strings.TrimSpace(sessionKey)
	cleaned := cleanProceduralTools(tools)
	if sessionKey == "" || len(cleaned) < proceduralTraceMinTools {
		return nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	rec := ProceduralTraceRecord{
		SessionKey: sessionKey,
		Tools:      cleaned,
		At:         time.Now().UnixMilli(),
	}
	if err := jsonlstore.Append(t.proceduralPath, rec); err != nil {
		return fmt.Errorf("genesis-tracker: append procedural trace: %w", err)
	}
	return nil
}

// RecentProceduralTraces returns the corpus deduped by session (latest row per
// session wins), newest session first, capped at limit. A session recorded
// several times as it grew collapses to its final, fullest sequence.
func (t *Tracker) RecentProceduralTraces(limit int) ([]ProceduralTraceRecord, error) {
	if limit <= 0 {
		limit = proceduralTraceScanLimit
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	entries, err := jsonlstore.Load[ProceduralTraceRecord](t.proceduralPath)
	if err != nil {
		return nil, fmt.Errorf("genesis-tracker: load procedural traces: %w", err)
	}

	latest := make(map[string]ProceduralTraceRecord, len(entries))
	lastIdx := make(map[string]int, len(entries))
	for i, e := range entries {
		key := strings.TrimSpace(e.SessionKey)
		if key == "" {
			continue
		}
		latest[key] = e
		lastIdx[key] = i
	}

	sessions := make([]string, 0, len(latest))
	for s := range latest {
		sessions = append(sessions, s)
	}
	// Newest-appended session first; lastIdx is the append position so ties are
	// impossible (distinct indices), keeping the order deterministic.
	sort.Slice(sessions, func(a, b int) bool { return lastIdx[sessions[a]] > lastIdx[sessions[b]] })

	out := make([]ProceduralTraceRecord, 0, min(limit, len(sessions)))
	for _, s := range sessions {
		out = append(out, latest[s])
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// MineProceduralSkillCandidates loads the recent corpus and mines it for
// recurring tool sequences. The seeding consumer (turn a candidate into a
// SKILL.md) is a follow-up; for now this answers "does recurring procedural
// structure exist in this operator's traces?" before any LLM budget is spent.
func (t *Tracker) MineProceduralSkillCandidates(opts ProceduralMineOptions) ([]RecurringToolSequence, error) {
	traces, err := t.RecentProceduralTraces(proceduralTraceScanLimit)
	if err != nil {
		return nil, err
	}
	return MineRecurringToolSequences(traces, opts), nil
}

// cleanProceduralTools trims tool names, drops empties, and caps the sequence
// length. Repeats and order are preserved — the procedure's shape is the signal.
func cleanProceduralTools(tools []string) []string {
	out := make([]string, 0, min(len(tools), proceduralTraceMaxTools))
	for _, name := range tools {
		name = strings.TrimSpace(truncateRunes(name, 120))
		if name == "" {
			continue
		}
		out = append(out, name)
		if len(out) >= proceduralTraceMaxTools {
			break
		}
	}
	return out
}

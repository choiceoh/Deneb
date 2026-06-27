// procedural_miner.go — corpus-level recurring tool-sequence mining.
//
// The Nudger induces a skill from a SINGLE session (per-trajectory induction),
// which fragments the library and forces the dedupe / cooldown / lever-yield
// machinery to clean up the redundant, trace-specific skills afterward. This
// file is the opposite approach (Skill-DisCo, arXiv:2606.26669; Skill-DisCo's
// corpus-level consolidation vs. per-trajectory induction): mine the tool-call
// structure that RECURS across many successful sessions and treat only the
// recurring structure as a skill candidate, so a skill is born from "you keep
// doing this" rather than from one lucky trajectory.
//
// This is pure, deterministic, side-effect-free logic. The corpus persistence
// lives in tracker_procedural_traces.go; the LLM seeding consumer (turn a
// candidate into a SKILL.md) is a follow-up. The honest first step is to record
// the corpus and MEASURE whether recurring structure exists at all — single-user
// trace volume is sparse (Skill-DisCo's own limitation), so seeding spends LLM
// budget only after the structure is shown to be there.
package genesis

import (
	"sort"
	"strings"
)

// ProceduralTraceRecord is one successful session's ordered tool-call sequence,
// the raw material for corpus-level mining. Stored append-only; see
// Tracker.RecordProceduralTrace.
type ProceduralTraceRecord struct {
	SessionKey string   `json:"sessionKey"`
	Tools      []string `json:"tools"`
	At         int64    `json:"at"`
}

// RecurringToolSequence is a contiguous tool-name n-gram that appears across
// multiple DISTINCT successful sessions — a procedural-skill candidate. Sessions
// is the coverage signal (how broadly reused); len(Tools) is the amortization
// signal (how many primitive steps one call would collapse).
type RecurringToolSequence struct {
	Tools       []string `json:"tools"`       // ordered tool names
	Sessions    int      `json:"sessions"`    // distinct sessions containing it
	Occurrences int      `json:"occurrences"` // total occurrences (>= Sessions)
	Score       float64  `json:"score"`       // coverage x amortization (ranking)
}

// ProceduralMineOptions bounds the miner. Zero values are filled by withDefaults.
type ProceduralMineOptions struct {
	MinSessions int // a candidate must appear in at least this many DISTINCT sessions
	MinLen      int // shortest tool n-gram considered (clamped to >= 2: one tool is no procedure)
	MaxLen      int // longest tool n-gram considered (caps the n-gram blowup)
	TopK        int // keep at most this many ranked candidates (<= 0 keeps all)
}

// DefaultProceduralMineOptions is the de-risk-first default: a sequence must
// recur in >= 3 distinct sessions to count, 2..5 tools long, top 10 reported.
// MinSessions=3 is deliberately low because single-user traffic is sparse.
func DefaultProceduralMineOptions() ProceduralMineOptions {
	return ProceduralMineOptions{MinSessions: 3, MinLen: 2, MaxLen: 5, TopK: 10}
}

func (o ProceduralMineOptions) withDefaults() ProceduralMineOptions {
	if o.MinSessions < 2 {
		o.MinSessions = 2 // "recurring" needs at least two sessions by definition
	}
	if o.MinLen < 2 {
		o.MinLen = 2
	}
	if o.MaxLen < o.MinLen {
		o.MaxLen = o.MinLen
	}
	return o
}

// ngramStat accumulates per-n-gram coverage during a mining pass.
type ngramStat struct {
	tools       []string
	sessions    map[string]struct{}
	occurrences int
}

// MineRecurringToolSequences finds contiguous tool-name n-grams that recur
// across distinct sessions in the corpus. The pipeline mirrors Skill-DisCo's
// distillation: extract multi-step operators (n-grams), score by coverage across
// traces, then consolidate so a fragment subsumed by a longer sequence with no
// broader coverage is dropped — leaving the maximal recurring structures plus
// any strictly-more-general (broader-coverage) shorter ones.
func MineRecurringToolSequences(traces []ProceduralTraceRecord, opts ProceduralMineOptions) []RecurringToolSequence {
	opts = opts.withDefaults()

	stats := make(map[string]*ngramStat)
	for _, tr := range traces {
		tools := normalizeTraceTools(tr.Tools)
		if len(tools) < opts.MinLen {
			continue
		}
		session := strings.TrimSpace(tr.SessionKey)
		if session == "" {
			continue
		}
		for l := opts.MinLen; l <= opts.MaxLen && l <= len(tools); l++ {
			for start := 0; start+l <= len(tools); start++ {
				window := tools[start : start+l]
				key := strings.Join(window, "\x00")
				st := stats[key]
				if st == nil {
					st = &ngramStat{
						tools:    append([]string(nil), window...),
						sessions: make(map[string]struct{}),
					}
					stats[key] = st
				}
				st.sessions[session] = struct{}{}
				st.occurrences++
			}
		}
	}

	// Threshold: keep n-grams recurring across >= MinSessions distinct sessions.
	kept := make([]RecurringToolSequence, 0, len(stats))
	for _, st := range stats {
		distinct := len(st.sessions)
		if distinct < opts.MinSessions {
			continue
		}
		kept = append(kept, RecurringToolSequence{
			Tools:       st.tools,
			Sessions:    distinct,
			Occurrences: st.occurrences,
			Score:       float64(distinct) * float64(len(st.tools)),
		})
	}

	kept = consolidateRecurringSequences(kept)

	sort.SliceStable(kept, func(a, b int) bool {
		if kept[a].Score != kept[b].Score {
			return kept[a].Score > kept[b].Score
		}
		if len(kept[a].Tools) != len(kept[b].Tools) {
			return len(kept[a].Tools) > len(kept[b].Tools)
		}
		return strings.Join(kept[a].Tools, "\x00") < strings.Join(kept[b].Tools, "\x00")
	})

	if opts.TopK > 0 && len(kept) > opts.TopK {
		kept = kept[:opts.TopK]
	}
	return kept
}

// consolidateRecurringSequences drops a candidate that is a contiguous
// sub-sequence of another kept candidate whose session coverage is at least as
// broad — that fragment adds nothing the longer (better-amortized) sequence does
// not already explain. A shorter sequence with STRICTLY broader coverage is kept:
// it is a genuinely more general procedure, not a redundant fragment.
func consolidateRecurringSequences(in []RecurringToolSequence) []RecurringToolSequence {
	// Longest first so a fragment is always compared against the maximal sequence
	// that could subsume it; ties by coverage then lexicographic for determinism.
	order := make([]RecurringToolSequence, len(in))
	copy(order, in)
	sort.SliceStable(order, func(a, b int) bool {
		if len(order[a].Tools) != len(order[b].Tools) {
			return len(order[a].Tools) > len(order[b].Tools)
		}
		if order[a].Sessions != order[b].Sessions {
			return order[a].Sessions > order[b].Sessions
		}
		return strings.Join(order[a].Tools, "\x00") < strings.Join(order[b].Tools, "\x00")
	})

	kept := make([]RecurringToolSequence, 0, len(order))
	for _, cand := range order {
		subsumed := false
		for _, k := range kept {
			if len(k.Tools) <= len(cand.Tools) {
				continue
			}
			if containsContiguous(k.Tools, cand.Tools) && k.Sessions >= cand.Sessions {
				subsumed = true
				break
			}
		}
		if !subsumed {
			kept = append(kept, cand)
		}
	}
	return kept
}

// containsContiguous reports whether sub appears as a contiguous slice of seq.
func containsContiguous(seq, sub []string) bool {
	if len(sub) == 0 || len(sub) > len(seq) {
		return false
	}
	for start := 0; start+len(sub) <= len(seq); start++ {
		match := true
		for i := range sub {
			if seq[start+i] != sub[i] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// normalizeTraceTools lowercases and trims tool names for stable n-gram keys,
// dropping empties. Order and repeats are preserved — "read read write" is a
// real three-step shape, not a duplicate to collapse.
func normalizeTraceTools(tools []string) []string {
	out := make([]string, 0, len(tools))
	for _, t := range tools {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" {
			continue
		}
		out = append(out, t)
	}
	return out
}

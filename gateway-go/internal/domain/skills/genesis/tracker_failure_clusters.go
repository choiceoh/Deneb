package genesis

import (
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

// Fleet-wide failure evidence clustering (Self-Harness weakness mining): group
// the health window's failures by deterministic verifier-grounded signature so
// the sweep lane's proposer starts from recurring cross-case mechanisms instead
// of isolated anecdotes or bare counters. The per-skill evolve prompt already
// clusters its own recent traces (mineSkillFailurePatterns); this is the
// cross-skill view the queue-starvation sweep needs.

const (
	// FailureClusterKindUsage groups real skill-use failures by their
	// classified trace signature (terminal=…|mechanism=…).
	FailureClusterKindUsage = "usage-failure"
	// FailureClusterKindRejection groups evolve_rejected events by the gate
	// class that rejected them — a jammed improvement loop is itself evidence.
	FailureClusterKindRejection = "evolve-rejection"
	// FailureClusterKindWorkout groups synthetic exercise failures (workout.go)
	// — same signature machinery, explicitly non-real provenance.
	FailureClusterKindWorkout = "workout-failure"
)

const defaultFailureClusterLimit = 8

// FailureClusterSummary is one failure cluster: an exact-signature group with
// its support (member count), recency, and the newest single-line example.
// Example carries raw log text — inert evidence data, never instructions.
type FailureClusterSummary struct {
	Kind  string `json:"kind"`
	Skill string `json:"skill,omitempty"`
	// Model is the resolved LLM the failures ran on (usage clusters only —
	// Self-Harness: the same harness exposes different pathologies per model,
	// so evidence keeps the axis fixes will need). Empty when legacy rows
	// without a recorded model dominate, or for rejection clusters.
	Model          string `json:"model,omitempty"`
	Signature      string `json:"signature"`
	TerminalCause  string `json:"terminalCause,omitempty"`
	AgentMechanism string `json:"agentMechanism,omitempty"`
	Support        int    `json:"support"`
	LastAt         int64  `json:"lastAt,omitempty"` // unix millis of the newest member
	Example        string `json:"example,omitempty"`
}

// FailureEvidenceClusters mines the last health window's failures across the
// whole fleet into support-ordered clusters. limit caps the result (<=0 uses
// the default). Sourced from the persisted JSONL sidecars (same rule as
// SelfHarnessSignals) so the evidence survives restarts.
func (t *Tracker) FailureEvidenceClusters(limit int) []FailureClusterSummary {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.computeFailureEvidenceClustersLocked(time.Now(), limit)
}

func (t *Tracker) computeFailureEvidenceClustersLocked(now time.Time, limit int) []FailureClusterSummary {
	if limit <= 0 {
		limit = defaultFailureClusterLimit
	}
	cutoff := now.Add(-evolutionHealthWindow).UnixMilli()
	clusters := map[string]*FailureClusterSummary{}

	if usage, err := jsonlstore.Load[UsageRecord](t.usagePath); err == nil {
		for _, record := range usage {
			isWorkout := record.Source == UsageSourceWorkout
			if record.UsedAt < cutoff || record.Success || (!isWorkout && !isRealUsageRecord(record)) {
				continue
			}
			trace := usageFailureTraceFromRecord(record)
			if trace == nil || strings.TrimSpace(trace.Signature) == "" {
				continue
			}
			// Model joins the exact-signature key: the same mechanism on two
			// models is two clusters, because the fix is usually model-specific.
			// Legacy rows without a model fold into a ""-model cluster.
			model := strings.TrimSpace(record.Model)
			kind := FailureClusterKindUsage
			if isWorkout {
				kind = FailureClusterKindWorkout
			}
			key := kind + "\x00" + record.SkillName + "\x00" + model + "\x00" + normalizedSelfHarnessSignature(trace.Signature)
			c := clusters[key]
			if c == nil {
				c = &FailureClusterSummary{
					Kind:           kind,
					Skill:          record.SkillName,
					Model:          model,
					Signature:      trace.Signature,
					TerminalCause:  trace.TerminalCause,
					AgentMechanism: trace.AgentMechanism,
				}
				clusters[key] = c
			}
			c.Support++
			if record.UsedAt >= c.LastAt {
				c.LastAt = record.UsedAt
				if example := singleLine(usageFailureTraceExample(*trace)); example != "" {
					c.Example = example
				}
			}
		}
	}

	if entries, err := jsonlstore.Load[LifecycleLogEntry](t.logPath); err == nil {
		for _, entry := range entries {
			if entry.CreatedAt < cutoff || entry.Type != "evolve_rejected" {
				continue
			}
			class := classifyEvolveRejection(entry.Reason)
			key := FailureClusterKindRejection + "\x00" + entry.SkillName + "\x00" + class
			c := clusters[key]
			if c == nil {
				c = &FailureClusterSummary{
					Kind:      FailureClusterKindRejection,
					Skill:     entry.SkillName,
					Signature: class,
				}
				clusters[key] = c
			}
			c.Support++
			if entry.CreatedAt >= c.LastAt {
				c.LastAt = entry.CreatedAt
				if example := singleLine(truncateRunes(strings.TrimSpace(entry.Reason), 160)); example != "" {
					c.Example = example
				}
			}
		}
	}

	out := make([]FailureClusterSummary, 0, len(clusters))
	for _, c := range clusters {
		out = append(out, *c)
	}
	// Support first (recurring mechanisms lead), then recency; the remaining
	// keys only make ties deterministic.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Support != out[j].Support {
			return out[i].Support > out[j].Support
		}
		if out[i].LastAt != out[j].LastAt {
			return out[i].LastAt > out[j].LastAt
		}
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		if out[i].Skill != out[j].Skill {
			return out[i].Skill < out[j].Skill
		}
		if out[i].Model != out[j].Model {
			return out[i].Model < out[j].Model
		}
		return out[i].Signature < out[j].Signature
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// evolveRejectionClassMatchers is the SINGLE source of truth mapping an
// evolve_rejected reason to a gate class. Two consumers share it so the
// substrings live in one place: classifyEvolveRejection (exclusive, first-match
// — one cluster per rejection) and the SelfHarnessSignalSummary counters
// (non-exclusive tallies, tracker_self_harness.go). Ordered: earlier entries
// win the exclusive classification.
var evolveRejectionClassMatchers = []struct {
	class string
	match func(loweredReason string) bool
}{
	{"missing-audit", func(r string) bool {
		return strings.Contains(r, "self-harness audit rejected") && strings.Contains(r, "missing")
	}},
	{"signature-mismatch", func(r string) bool {
		return strings.Contains(r, "does not match supported failure signatures") ||
			strings.Contains(r, "no failure evidence bundle")
	}},
	{"surface-mismatch", func(r string) bool {
		return strings.Contains(r, "self-harness surface rejected") ||
			strings.Contains(r, "did not match changed skill.md sections") ||
			strings.Contains(r, "not editable by skill.md body evolve")
	}},
	{"heldout-replay", func(r string) bool {
		return strings.Contains(r, "held-out") || strings.Contains(r, "replay")
	}},
	{"patch-first", func(r string) bool {
		return strings.Contains(r, "patch-first")
	}},
}

// classifyEvolveRejection maps a rejection reason to exactly one gate class,
// first match wins, "other" when nothing matches.
func classifyEvolveRejection(reason string) string {
	r := strings.ToLower(strings.TrimSpace(reason))
	for _, m := range evolveRejectionClassMatchers {
		if m.match(r) {
			return m.class
		}
	}
	return "other"
}

// evolveRejectionMatchesClass reports whether a (pre-lowercased) reason matches
// one named class — the non-exclusive query the SelfHarnessSignalSummary
// counters use so they no longer re-hardcode the substrings.
func evolveRejectionMatchesClass(loweredReason, class string) bool {
	for _, m := range evolveRejectionClassMatchers {
		if m.class == class {
			return m.match(loweredReason)
		}
	}
	return false
}

// singleLine collapses whitespace runs (incl. newlines) so an example can be
// embedded in a one-line prompt bullet.
func singleLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

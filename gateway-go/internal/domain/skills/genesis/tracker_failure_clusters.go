package genesis

import (
	"sort"
	"strings"
	"time"

	genesiscommon "github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/common"

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
	// Route is an advisory shadow classification. It is intentionally excluded
	// from grouping/ranking and cannot change dispatch or editable-surface policy.
	Route FailureInterventionRoute `json:"route"`
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
	cutoff := now.Add(-evolutionHealthWindow).UnixMilli()
	clusters := failureClusterGroups{}

	for _, record := range loadAvailableFailureClusterRecords[UsageRecord](t.usagePath) {
		if evidence, ok := usageFailureClusterEvidence(record, cutoff); ok {
			clusters.add(evidence)
		}
	}
	for _, entry := range loadAvailableFailureClusterRecords[LifecycleLogEntry](t.logPath) {
		if evidence, ok := rejectionFailureClusterEvidence(entry, cutoff); ok {
			clusters.add(evidence)
		}
	}
	return clusters.ranked(limit)
}

// loadAvailableFailureClusterRecords preserves the query's independent-source
// degradation: an unreadable sidecar contributes no evidence, while records
// from the other sidecar remain usable. The public query has no error channel.
func loadAvailableFailureClusterRecords[T any](path string) []T {
	records, err := jsonlstore.Load[T](path)
	if err != nil {
		return nil
	}
	return records
}

type failureClusterEvidence struct {
	key        string
	prototype  FailureClusterSummary
	observedAt int64
	example    string
}

func usageFailureClusterEvidence(record UsageRecord, cutoff int64) (failureClusterEvidence, bool) {
	if !isUsageFailureClusterCandidate(record, cutoff) {
		return failureClusterEvidence{}, false
	}
	trace := usageFailureTraceFromRecord(record)
	if trace == nil || strings.TrimSpace(trace.Signature) == "" {
		return failureClusterEvidence{}, false
	}

	kind := FailureClusterKindUsage
	if record.Source == usageSourceWorkout {
		kind = FailureClusterKindWorkout
	}
	model := strings.TrimSpace(record.Model)
	return failureClusterEvidence{
		key: kind + "\x00" + record.SkillName + "\x00" + model + "\x00" +
			normalizedSelfHarnessSignature(trace.Signature),
		prototype: FailureClusterSummary{
			Kind:           kind,
			Skill:          record.SkillName,
			Model:          model,
			Signature:      trace.Signature,
			TerminalCause:  trace.TerminalCause,
			AgentMechanism: trace.AgentMechanism,
		},
		observedAt: record.UsedAt,
		example:    singleLine(usageFailureTraceExample(*trace)),
	}, true
}

func isUsageFailureClusterCandidate(record UsageRecord, cutoff int64) bool {
	if record.UsedAt < cutoff || record.Success {
		return false
	}
	if record.Source == usageSourceWorkout {
		return true
	}
	return isRealUsageRecord(record)
}

func rejectionFailureClusterEvidence(entry LifecycleLogEntry, cutoff int64) (failureClusterEvidence, bool) {
	if entry.CreatedAt < cutoff || entry.Type != "evolve_rejected" {
		return failureClusterEvidence{}, false
	}
	class := classifyEvolveRejection(entry.Reason)
	return failureClusterEvidence{
		key: FailureClusterKindRejection + "\x00" + entry.SkillName + "\x00" + class,
		prototype: FailureClusterSummary{
			Kind:      FailureClusterKindRejection,
			Skill:     entry.SkillName,
			Signature: class,
		},
		observedAt: entry.CreatedAt,
		example: singleLine(
			genesiscommon.TruncateRunes(strings.TrimSpace(entry.Reason), 160),
		),
	}, true
}

type failureClusterGroups map[string]*FailureClusterSummary

func (groups failureClusterGroups) add(evidence failureClusterEvidence) {
	cluster := groups[evidence.key]
	if cluster == nil {
		prototype := evidence.prototype
		cluster = &prototype
		groups[evidence.key] = cluster
	}
	cluster.Support++
	if evidence.observedAt < cluster.LastAt {
		return
	}
	cluster.LastAt = evidence.observedAt
	if evidence.example != "" {
		cluster.Example = evidence.example
	}
}

func (groups failureClusterGroups) ranked(limit int) []FailureClusterSummary {
	clusters := make([]FailureClusterSummary, 0, len(groups))
	for _, cluster := range groups {
		clusters = append(clusters, *cluster)
	}
	sort.Slice(clusters, func(i, j int) bool {
		return failureClusterRanksBefore(clusters[i], clusters[j])
	})
	limit = normalizedFailureClusterLimit(limit)
	if len(clusters) > limit {
		clusters = clusters[:limit]
	}
	for i := range clusters {
		clusters[i].Route = routeFailureCluster(clusters[i])
	}
	return clusters
}

func normalizedFailureClusterLimit(limit int) int {
	if limit <= 0 {
		return defaultFailureClusterLimit
	}
	return limit
}

func failureClusterRanksBefore(left, right FailureClusterSummary) bool {
	if left.Support != right.Support {
		return left.Support > right.Support
	}
	if left.LastAt != right.LastAt {
		return left.LastAt > right.LastAt
	}
	if left.Kind != right.Kind {
		return left.Kind < right.Kind
	}
	if left.Skill != right.Skill {
		return left.Skill < right.Skill
	}
	if left.Model != right.Model {
		return left.Model < right.Model
	}
	return left.Signature < right.Signature
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
		// Narrow to the audit-COMPLETENESS rejection ("...rejected: missing
		// <fields>", evolver.go). A bare contains("missing") also matched the
		// signature-mismatch reason whenever its signature list held the common
		// terminal=missing-artifact class, misfiling a signature-mismatch as
		// missing-audit — the mismatch class below must win that case.
		return strings.Contains(r, "self-harness audit rejected: missing ")
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

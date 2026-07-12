package genesis

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestFailureEvidenceClusters_GroupsOrdersAndWindows(t *testing.T) {
	tr := newTestTracker(t)
	now := time.UnixMilli(1_783_500_000_000)
	dayMs := int64(24 * time.Hour / time.Millisecond)

	// Three same-mechanism failures on one skill (the dominant cluster), one
	// timeout on another skill, plus records the miner must ignore: a success,
	// a review-fork failure, and a failure older than the health window.
	artifactErr := "required artifact missing: /tmp/out.json"
	for i := int64(0); i < 3; i++ {
		appendFunnel(t, tr.usagePath, UsageRecord{
			SkillName: "contract-review", SessionKey: "client:a", Success: false,
			ErrorMsg: artifactErr, UsedAt: now.UnixMilli() - (3-i)*dayMs, Source: UsageSourceReal,
		})
	}
	appendFunnel(t, tr.usagePath, UsageRecord{
		SkillName: "ocr-run", SessionKey: "client:a", Success: false,
		ErrorMsg: "bash: timeout after 120 seconds", UsedAt: now.UnixMilli() - dayMs, Source: UsageSourceReal,
	})
	appendFunnel(t, tr.usagePath, UsageRecord{
		SkillName: "contract-review", SessionKey: "client:a", Success: true,
		UsedAt: now.UnixMilli() - dayMs, Source: UsageSourceReal,
	})
	appendFunnel(t, tr.usagePath, UsageRecord{
		SkillName: "contract-review", SessionKey: "system:skill-review:x", Success: false,
		ErrorMsg: artifactErr, UsedAt: now.UnixMilli() - dayMs, Source: UsageSourceReviewConsult,
	})
	appendFunnel(t, tr.usagePath, UsageRecord{
		SkillName: "contract-review", SessionKey: "client:a", Success: false,
		ErrorMsg: artifactErr, UsedAt: now.UnixMilli() - 30*dayMs, Source: UsageSourceReal,
	})

	// Two surface rejections on one skill, one patch-first on another, one
	// ancient rejection, and a non-rejection lifecycle entry as noise.
	appendFunnel(t, tr.logPath, LifecycleLogEntry{
		Type: "evolve_rejected", SkillName: "contract-review", CreatedAt: now.UnixMilli() - 2*dayMs,
		Reason: "self-harness surface rejected: claimed section not in diff",
	})
	appendFunnel(t, tr.logPath, LifecycleLogEntry{
		Type: "evolve_rejected", SkillName: "contract-review", CreatedAt: now.UnixMilli() - dayMs,
		Reason: "self-harness surface rejected:\n claimed  edited_surface did not match changed SKILL.md sections",
	})
	appendFunnel(t, tr.logPath, LifecycleLogEntry{
		Type: "evolve_rejected", SkillName: "system-health-check", CreatedAt: now.UnixMilli() - dayMs,
		Reason: "Hermes patch-first gate rejected broad rewrite: changed 5 sections, max 3",
	})
	appendFunnel(t, tr.logPath, LifecycleLogEntry{
		Type: "evolve_rejected", SkillName: "old-skill", CreatedAt: now.UnixMilli() - 30*dayMs,
		Reason: "held-out selection rejected: stale",
	})
	appendFunnel(t, tr.logPath, LifecycleLogEntry{
		Type: "evolved", SkillName: "contract-review", CreatedAt: now.UnixMilli() - dayMs,
	})

	tr.mu.Lock()
	clusters := tr.computeFailureEvidenceClustersLocked(now, 0)
	tr.mu.Unlock()

	if len(clusters) != 4 {
		t.Fatalf("want 4 clusters (3 in-window groups + patch-first), got %d: %+v", len(clusters), clusters)
	}

	top := clusters[0]
	if top.Kind != FailureClusterKindUsage || top.Skill != "contract-review" || top.Support != 3 {
		t.Fatalf("dominant cluster should be the 3x artifact failure, got %+v", top)
	}
	if !strings.Contains(top.Signature, "missing-artifact") {
		t.Errorf("artifact failures should classify to missing-artifact, got %q", top.Signature)
	}
	if top.LastAt != now.UnixMilli()-dayMs {
		t.Errorf("cluster LastAt should track the newest member, got %d", top.LastAt)
	}
	if !strings.Contains(top.Example, "required artifact missing") {
		t.Errorf("cluster example should quote the newest failure, got %q", top.Example)
	}

	second := clusters[1]
	if second.Kind != FailureClusterKindRejection || second.Skill != "contract-review" ||
		second.Signature != "surface-mismatch" || second.Support != 2 {
		t.Fatalf("second cluster should be the 2x surface rejection, got %+v", second)
	}
	if strings.ContainsAny(second.Example, "\n\t") || strings.Contains(second.Example, "  ") {
		t.Errorf("rejection example should be whitespace-collapsed, got %q", second.Example)
	}

	kinds := map[string]bool{}
	for _, c := range clusters {
		kinds[c.Kind+"/"+c.Skill+"/"+c.Signature] = true
	}
	if !kinds["usage-failure/ocr-run/terminal=timeout|mechanism=bounded-execution"] {
		t.Errorf("timeout failure cluster missing: %+v", clusters)
	}
	if !kinds["evolve-rejection/system-health-check/patch-first"] {
		t.Errorf("patch-first rejection cluster missing: %+v", clusters)
	}
	for key := range kinds {
		if strings.Contains(key, "old-skill") {
			t.Errorf("out-of-window rejection must be excluded: %v", key)
		}
	}

	tr.mu.Lock()
	limited := tr.computeFailureEvidenceClustersLocked(now, 2)
	tr.mu.Unlock()
	if len(limited) != 2 {
		t.Fatalf("limit should truncate clusters, got %d", len(limited))
	}
}

func TestClassifyEvolveRejection_ExclusiveFirstMatch(t *testing.T) {
	cases := []struct {
		reason string
		want   string
	}{
		{"self-harness audit rejected: missing target_signature, edited_surface", "missing-audit"},
		{"self-harness audit rejected: target_signature \"x\" does not match supported failure signatures: y", "signature-mismatch"},
		{"self-harness audit rejected: no failure evidence bundle or review finding supports target_signature", "signature-mismatch"},
		{"self-harness surface rejected: edited_surface did not match changed skill.md sections", "surface-mismatch"},
		{"held-out selection rejected: candidate did not improve validation score", "heldout-replay"},
		{"replay gate rejected: behavior contract broke", "heldout-replay"},
		{"Hermes patch-first gate rejected broad rewrite: changed 5 sections, max 3", "patch-first"},
		{"judge score delta below threshold", "other"},
		{"", "other"},
	}
	for _, tc := range cases {
		if got := classifyEvolveRejection(tc.reason); got != tc.want {
			t.Errorf("classifyEvolveRejection(%q) = %q, want %q", tc.reason, got, tc.want)
		}
	}
}

// The same mechanism on two models is two clusters — fixes are usually
// model-specific (Self-Harness). Legacy rows without a model fold together.
func TestFailureEvidenceClusters_ModelAxisSplits(t *testing.T) {
	tr := newTestTracker(t)
	now := time.UnixMilli(1_783_500_000_000)
	hourMs := int64(time.Hour / time.Millisecond)

	for i, model := range []string{"m2.5", "m2.5", "qwen3.5", ""} {
		appendFunnel(t, tr.usagePath, UsageRecord{
			SkillName: "ocr-run", SessionKey: "client:a", Success: false, Model: model,
			ErrorMsg: "bash: timeout after 120 seconds", UsedAt: now.UnixMilli() - int64(i+1)*hourMs, Source: UsageSourceReal,
		})
	}

	tr.mu.Lock()
	clusters := tr.computeFailureEvidenceClustersLocked(now, 0)
	tr.mu.Unlock()

	if len(clusters) != 3 {
		t.Fatalf("want 3 clusters (m2.5 x2, qwen3.5, legacy \"\"), got %d: %+v", len(clusters), clusters)
	}
	top := clusters[0]
	if top.Model != "m2.5" || top.Support != 2 {
		t.Fatalf("dominant cluster should be m2.5 with support 2, got %+v", top)
	}
	seen := map[string]int{}
	for _, c := range clusters {
		seen[c.Model] = c.Support
	}
	if seen["qwen3.5"] != 1 || seen[""] != 1 {
		t.Fatalf("per-model split wrong: %+v", clusters)
	}
}

func TestFailureEvidenceClusters_DeterministicRanking(t *testing.T) {
	tr := newTestTracker(t)
	now := time.UnixMilli(1_783_500_000_000)
	tiedAt := now.Add(-time.Hour).UnixMilli()
	usage := []UsageRecord{
		clusterUsageRecord("recent", "", "terminal=timeout|mechanism=bounded-execution", now.UnixMilli(), UsageSourceReal),
		clusterUsageRecord("beta", "", "terminal=timeout|mechanism=bounded-execution", tiedAt, UsageSourceReal),
		clusterUsageRecord("alpha", " z ", "terminal=tool-error|mechanism=tool-boundary", tiedAt, UsageSourceReal),
		clusterUsageRecord("alpha", "a", "terminal=timeout|mechanism=bounded-execution", tiedAt, UsageSourceReal),
		clusterUsageRecord("alpha", "a", "terminal=missing-artifact|mechanism=artifact-recovery", tiedAt, UsageSourceReal),
		clusterUsageRecord("alpha", "", "terminal=timeout|mechanism=bounded-execution", tiedAt, UsageSourceWorkout),
	}
	for _, record := range usage {
		appendFunnel(t, tr.usagePath, record)
	}
	appendFunnel(t, tr.logPath, LifecycleLogEntry{
		Type: "evolve_rejected", SkillName: "zeta", CreatedAt: tiedAt,
		Reason: "judge score delta below threshold",
	})

	clusters := failureClustersForTest(tr, now, 20)
	got := make([]string, 0, len(clusters))
	for _, cluster := range clusters {
		got = append(got, strings.Join([]string{cluster.Kind, cluster.Skill, cluster.Model, cluster.Signature}, "/"))
	}
	want := []string{
		"usage-failure/recent//terminal=timeout|mechanism=bounded-execution",
		"evolve-rejection/zeta//other",
		"usage-failure/alpha/a/terminal=missing-artifact|mechanism=artifact-recovery",
		"usage-failure/alpha/a/terminal=timeout|mechanism=bounded-execution",
		"usage-failure/alpha/z/terminal=tool-error|mechanism=tool-boundary",
		"usage-failure/beta//terminal=timeout|mechanism=bounded-execution",
		"workout-failure/alpha//terminal=timeout|mechanism=bounded-execution",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ranked clusters = %#v, want %#v", got, want)
	}
}

func TestFailureEvidenceClusters_DefaultAndExplicitLimitsUseRankedPrefix(t *testing.T) {
	tr := newTestTracker(t)
	now := time.UnixMilli(1_783_500_000_000)
	for index := 0; index < 10; index++ {
		appendFunnel(t, tr.usagePath, clusterUsageRecord(
			fmt.Sprintf("skill-%02d", index), "", "terminal=timeout|mechanism=bounded-execution",
			now.Add(-time.Duration(index)*time.Minute).UnixMilli(), UsageSourceReal,
		))
	}

	all := failureClustersForTest(tr, now, 20)
	defaulted := failureClustersForTest(tr, now, 0)
	negative := failureClustersForTest(tr, now, -1)
	limited := failureClustersForTest(tr, now, 3)
	if len(all) != 10 || len(defaulted) != defaultFailureClusterLimit {
		t.Fatalf("cluster counts all/default = %d/%d", len(all), len(defaulted))
	}
	if !reflect.DeepEqual(defaulted, negative) {
		t.Fatalf("non-positive limits diverged: zero=%+v negative=%+v", defaulted, negative)
	}
	if !reflect.DeepEqual(limited, all[:3]) {
		t.Fatalf("explicit limit did not preserve ranked prefix: got=%+v want=%+v", limited, all[:3])
	}
}

func TestFailureEvidenceClusters_SourceReadErrorsDegradeIndependently(t *testing.T) {
	now := time.UnixMilli(1_783_500_000_000)

	t.Run("usage sidecar unavailable", func(t *testing.T) {
		tr := newTestTracker(t)
		if err := os.Mkdir(tr.usagePath, 0o700); err != nil {
			t.Fatal(err)
		}
		appendFunnel(t, tr.logPath, LifecycleLogEntry{
			Type: "evolve_rejected", SkillName: "review", CreatedAt: now.UnixMilli(),
			Reason: "Hermes patch-first gate rejected broad rewrite",
		})
		clusters := failureClustersForTest(tr, now, 20)
		if len(clusters) != 1 || clusters[0].Kind != FailureClusterKindRejection {
			t.Fatalf("lifecycle evidence was lost with usage read error: %+v", clusters)
		}
	})

	t.Run("lifecycle sidecar unavailable", func(t *testing.T) {
		tr := newTestTracker(t)
		if err := os.Mkdir(tr.logPath, 0o700); err != nil {
			t.Fatal(err)
		}
		appendFunnel(t, tr.usagePath, clusterUsageRecord(
			"review", "", "terminal=timeout|mechanism=bounded-execution", now.UnixMilli(), UsageSourceReal,
		))
		clusters := failureClustersForTest(tr, now, 20)
		if len(clusters) != 1 || clusters[0].Kind != FailureClusterKindUsage {
			t.Fatalf("usage evidence was lost with lifecycle read error: %+v", clusters)
		}
	})
}

func clusterUsageRecord(skill, model, signature string, usedAt int64, source string) UsageRecord {
	return UsageRecord{
		SkillName: skill, SessionKey: "client:test", Model: model, UsedAt: usedAt, Source: source,
		FailureTrace: &UsageFailureTrace{Signature: signature, ErrorMsg: "failure evidence"},
	}
}

func failureClustersForTest(tr *Tracker, now time.Time, limit int) []FailureClusterSummary {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return tr.computeFailureEvidenceClustersLocked(now, limit)
}

// A signature-mismatch rejection whose evidence involves the common
// missing-artifact class must NOT be misfiled as missing-audit (the "missing"
// substring used to win). Regression for the bot-flagged classifier ordering.
func TestClassifyEvolveRejection_MissingArtifactSignatureNotMissingAudit(t *testing.T) {
	sigMismatch := `self-harness audit rejected: target_signature "x" does not match supported failure signatures: terminal=missing-artifact|mechanism=artifact-recovery`
	if got := classifyEvolveRejection(sigMismatch); got != "signature-mismatch" {
		t.Fatalf("signature-mismatch with a missing-artifact signature list must classify as signature-mismatch, got %q", got)
	}
	// The genuine audit-completeness rejection still lands on missing-audit.
	missingAudit := "self-harness audit rejected: missing target_signature, edited_surface"
	if got := classifyEvolveRejection(missingAudit); got != "missing-audit" {
		t.Fatalf("audit-completeness rejection must classify as missing-audit, got %q", got)
	}
}

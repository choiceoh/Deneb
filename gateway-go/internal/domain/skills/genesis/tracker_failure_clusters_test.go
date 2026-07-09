package genesis

import (
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

package surfaces

import "testing"

func TestClassifyProposalSurfacesClassifiesEditableProposeOnlyAndReturnsForbiddenTargets(t *testing.T) {
	tier, forbidden := ClassifyProposalSurfaces([]string{"skills/demo/SKILL.md", "workspace/AGENTS.md"})
	if tier != SurfaceTierProposeOnly || len(forbidden) != 0 {
		t.Fatalf("mixed editable surfaces = (%q, %v), want propose-only without forbidden targets", tier, forbidden)
	}
	_, forbidden = ClassifyProposalSurfaces([]string{"gateway/prompt_cache.go"})
	if len(forbidden) != 1 {
		t.Fatalf("forbidden surface was not rejected: %v", forbidden)
	}
}

// Runtime-health findings (and similar proactive sources) intentionally omit
// targetFiles until a human/agent localizes the fix. The summary tier must still
// be propose-only — an empty string left clients and audits unable to label the
// row (observed on health-finding:runtime-latency, 2026-07-13).
func TestClassifyProposalSurfacesEmptyTargetsDefaultProposeOnly(t *testing.T) {
	for _, targets := range [][]string{nil, {}} {
		tier, forbidden := ClassifyProposalSurfaces(targets)
		if tier != SurfaceTierProposeOnly || len(forbidden) != 0 {
			t.Fatalf("targets=%v → tier=%q forbidden=%v, want propose-only", targets, tier, forbidden)
		}
	}
}

// Operator authorization 2026-07-12: gateway source is declared propose-only,
// while the acceptance machinery stays forbidden — the loop must never be able
// to queue an edit to its own gates.
func TestClassifySurfaceReturnsGatewaySourceProposeOnlyAndAcceptanceMachineryForbidden(t *testing.T) {
	if s := ClassifySurface("gateway-go/internal/runtime/server/heartbeat_task.go"); s.Name != "gateway-source" || s.Tier != SurfaceTierProposeOnly {
		t.Fatalf("gateway source = %+v", s)
	}
	for _, acceptor := range []string{
		"internal/domain/skills/genesis/validation_engine.go",
		"internal/domain/skills/genesis/eprocess/eprocess.go",
		"internal/domain/skills/genesis/meta_judge_bench.go",
		"internal/domain/skills/genesis/surfaces/surfaces.go",
		"internal/domain/skills/genesis/tracker_usage.go",
	} {
		if s := ClassifySurface(acceptor); s.Tier != SurfaceTierForbidden {
			t.Fatalf("acceptance machinery %s = %+v, want forbidden", acceptor, s)
		}
	}
	// Precedence: forbidden acceptor files must not be shadowed by *.go.
	tier, forbidden := ClassifyProposalSurfaces([]string{"a_normal_file.go", "validation_engine.go"})
	if tier != SurfaceTierProposeOnly || len(forbidden) != 1 {
		t.Fatalf("mixed proposal = tier %q forbidden %v", tier, forbidden)
	}
}

// C2 hardening: a directory-shaped target enclosing acceptance machinery is
// forbidden at record time — basename-only matching let ".../genesis" (the
// health miner's normal output shape) through even though it contains every
// gate file. Non-acceptor directories stay proposable.
func TestClassifySurfaceRejectsDirectoriesEnclosingAcceptanceMachinery(t *testing.T) {
	for _, dir := range []string{
		"gateway-go/internal/domain/skills/genesis",
		"internal/domain/skills/genesis/",
		"gateway-go/internal/domain/skills/genesis/surfaces",
		"gateway-go/internal/domain/skills/genesis/eprocess",
		"scripts/dev",
	} {
		if s := ClassifySurface(dir); s.Tier != SurfaceTierForbidden {
			t.Fatalf("acceptor-enclosing directory %q = %+v, want forbidden", dir, s)
		}
	}
	for _, dir := range []string{
		"gateway-go/internal/domain/wiki",
		"gateway-go/internal/runtime/server",
		"client-android/app",
	} {
		if s := ClassifySurface(dir); s.Tier == SurfaceTierForbidden {
			t.Fatalf("non-acceptor directory %q = %+v, must stay proposable", dir, s)
		}
	}
}

// C2 hardening: the scripts-side acceptor (dispatch allowlist, outcome table,
// prompt composer, landing tool, CI gate) is forbidden — the loop could
// previously queue an edit to its own dispatcher.
func TestClassifySurfaceRejectsAcceptanceScriptsIncludingBareBasename(t *testing.T) {
	for _, target := range []string{
		"scripts/dev/coding-dispatch.sh",
		"scripts/dev/dispatch_prompt.py",
		"scripts/dev/dispatch_outcome.py",
		"scripts/dev/pr.sh",
		".github/workflows/ci.yml",
		"dispatch_outcome.py", // bare-basename spelling
	} {
		if s := ClassifySurface(target); s.Tier != SurfaceTierForbidden {
			t.Fatalf("acceptance script %q = %+v, want forbidden", target, s)
		}
	}
	// Other scripts stay proposable.
	if s := ClassifySurface("scripts/audit/health_finding_miner.py"); s.Tier == SurfaceTierForbidden {
		t.Fatalf("non-acceptor script = %+v, must stay proposable", s)
	}
}

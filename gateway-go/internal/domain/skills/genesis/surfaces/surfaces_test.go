package surfaces

import "testing"

func TestClassifyProposalSurfacesUsesMostRestrictiveTier(t *testing.T) {
	tier, forbidden := ClassifyProposalSurfaces([]string{"skills/demo/SKILL.md", "workspace/AGENTS.md"})
	if tier != SurfaceTierProposeOnly || len(forbidden) != 0 {
		t.Fatalf("mixed editable surfaces = (%q, %v), want propose-only without forbidden targets", tier, forbidden)
	}
	_, forbidden = ClassifyProposalSurfaces([]string{"gateway/prompt_cache.go"})
	if len(forbidden) != 1 {
		t.Fatalf("forbidden surface was not rejected: %v", forbidden)
	}
}

// Operator authorization 2026-07-12: gateway source is declared propose-only,
// while the acceptance machinery stays forbidden — the loop must never be able
// to queue an edit to its own gates.
func TestSourceSurfaceAuthorization(t *testing.T) {
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

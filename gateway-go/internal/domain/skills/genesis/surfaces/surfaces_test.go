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

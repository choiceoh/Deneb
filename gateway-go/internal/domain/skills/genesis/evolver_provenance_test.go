package genesis

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/generation"
)

// TestNewProvenanceStampsProcedureVersionAndModel verifies an evolve decision's
// provenance certificate now pins the WHOLE procedure state (composite ref +
// per-prompt breakdown) and the producer model — Go mints it deterministically,
// so a downstream outcome can be attributed to the exact procedure that made it.
func TestNewProvenanceStampsProcedureVersionAndModel(t *testing.T) {
	e := &Evolver{model: "lightweight"} // meta unwired → pure-fallback procedure
	prov := e.newProvenance()

	if !strings.HasPrefix(prov.ProcedureRef, "proc-") {
		t.Fatalf("ProcedureRef = %q; want proc- prefix", prov.ProcedureRef)
	}
	if prov.EvolveModel != "lightweight" {
		t.Errorf("EvolveModel = %q; want the producer model role", prov.EvolveModel)
	}
	// The two prompts that govern an evolve decision are pinned.
	if prov.EvolveArtifactVersion == "" || prov.JudgeArtifactVersion == "" {
		t.Errorf("governing artifact versions incomplete: %+v", prov)
	}
	// The composite must equal the resolver's ProcedureRef over exactly the
	// governing (evolve + skill-judge) prompts — the certificate can't drift from
	// the artifacts it claims, and it must be lane-specific.
	want := generation.NewMetaArtifacts("", nil).ProcedureRef(
		generation.MetaEvolveSystemPrompt, generation.MetaSkillJudgeSystemPrompt)
	if prov.ProcedureRef != want {
		t.Errorf("ProcedureRef = %q; want governing-set composite %q", prov.ProcedureRef, want)
	}
}

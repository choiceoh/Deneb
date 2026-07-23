package genesis

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/generation"
)

// TestProvenanceCapturedAtPointOfUse verifies the certificate is assembled from
// values captured at each LLM call (producer snapshot + judge stamp) and that
// ProcedureRef is derived from exactly those captured versions — never re-read
// from mutable config, so a concurrent SetPrimary / meta-adoption can't make the
// record claim a model or prompt that did not produce/judge the decision.
func TestProvenanceCapturedAtPointOfUse(t *testing.T) {
	// Producer snapshot (captured at the generate call) seeds producer fields.
	snap := producerSnapshot{model: "lightweight", evolveVersion: "aaaa1111bbbb"}
	prov := provenanceFromProducer(snap)
	if prov.EvolveModel != "lightweight" || prov.EvolveArtifactVersion != "aaaa1111bbbb" {
		t.Fatalf("producer snapshot not seeded onto provenance: %+v", prov)
	}
	if prov.ProcedureRef != "" || prov.JudgeModel != "" {
		t.Fatalf("judge/ref must not be set before the judge call: %+v", prov)
	}

	// Judge stamp (captured at the committed verdict) fills judge fields.
	prov.JudgeModel = "teacher"
	prov.JudgeArtifactVersion = "cccc2222dddd"

	// ProcedureRef is derived at log time from the captured governing versions,
	// and must equal the resolver's composite over the same two — the ref can't
	// drift from the versions it claims.
	prov.fillProcedureRef()
	want := generation.ProcedureRefFromVersions(map[string]string{
		generation.MetaEvolveSystemPrompt:     "aaaa1111bbbb",
		generation.MetaSkillJudgeSystemPrompt: "cccc2222dddd",
	})
	if prov.ProcedureRef != want {
		t.Fatalf("ProcedureRef = %q; want derived composite %q", prov.ProcedureRef, want)
	}
}

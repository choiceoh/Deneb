package skilllifecycle

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
)

// Tool evidence is unambiguous — a declared tool either ran or it did not — so
// it mints cases immediately. Answer-shape evidence stays shadowed until its
// false-positive rate has been observed (ADR 0006).
func TestOutputEvidenceIsShadowedUntilArmed(t *testing.T) {
	t.Setenv("DENEB_SKILL_OUTPUT_EVIDENCE", "")
	if !outputEvidenceArmed(chatport.SkillEvidenceTools) {
		t.Fatal("tool evidence must never be shadowed")
	}
	if outputEvidenceArmed(chatport.SkillEvidenceOutput) {
		t.Fatal("output evidence minted a case before being armed")
	}
	t.Setenv("DENEB_SKILL_OUTPUT_EVIDENCE", "1")
	if !outputEvidenceArmed(chatport.SkillEvidenceOutput) {
		t.Fatal("arming the flag did not admit output evidence")
	}
}

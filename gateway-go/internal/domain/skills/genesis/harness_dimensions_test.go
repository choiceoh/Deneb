package genesis

import (
	"reflect"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

func TestWithHarnessDimensionsDerivesDiagnosisInsteadOfTrustingInput(t *testing.T) {
	audit := withHarnessDimensions(HarnessEditAudit{
		TargetSignature:        "terminal=schema-format|mechanism=structured-contract",
		EditedSurface:          "Verification",
		ExpectedBehaviorChange: "verify the structured tool result",
		PrimaryDimension:       "D0-model-authored",
		SecondaryDimensions:    []string{HarnessDimensionMemory},
	})
	if audit.PrimaryDimension != HarnessDimensionToolInteraction {
		t.Fatalf("primary = %q, want %q", audit.PrimaryDimension, HarnessDimensionToolInteraction)
	}
	if !reflect.DeepEqual(audit.SecondaryDimensions, []string{HarnessDimensionOutput}) {
		t.Fatalf("secondary = %+v, want output processing", audit.SecondaryDimensions)
	}
}

func TestWithHarnessDimensionsFallsBackToEditedSurface(t *testing.T) {
	audit := withHarnessDimensions(HarnessEditAudit{EditedSurface: "Memory retention"})
	if audit.PrimaryDimension != HarnessDimensionMemory || len(audit.SecondaryDimensions) != 0 {
		t.Fatalf("audit = %+v, want memory-only diagnosis", audit)
	}
}

func TestRecentLifecycleLogBackfillsLegacyHarnessDimensions(t *testing.T) {
	tracker := newTestTracker(t)
	if err := jsonlstore.Append(tracker.logPath, LifecycleLogEntry{
		Type:      "evolve_confirmed",
		SkillName: "legacy-deploy",
		SelfHarnessAudit: &HarnessEditAudit{
			TargetSignature: "terminal=timeout|mechanism=bounded-execution",
			EditedSurface:   "Procedure",
		},
		CreatedAt: 1,
	}); err != nil {
		t.Fatalf("append legacy lifecycle entry: %v", err)
	}

	entries, err := tracker.RecentLifecycleLog(1)
	if err != nil {
		t.Fatalf("RecentLifecycleLog: %v", err)
	}
	if len(entries) != 1 || entries[0].SelfHarnessAudit == nil ||
		entries[0].SelfHarnessAudit.PrimaryDimension != HarnessDimensionOrchestration {
		t.Fatalf("legacy lifecycle diagnosis = %+v", entries)
	}
}

func TestHarnessDimensionsForSignaturesDeduplicatesInStableOrder(t *testing.T) {
	got := harnessDimensionsForSignatures([]string{
		"terminal=schema-format|mechanism=structured-contract",
		"terminal=timeout|mechanism=bounded-execution",
		"terminal=schema-format|mechanism=structured-contract",
	})
	want := []string{
		HarnessDimensionToolInteraction,
		HarnessDimensionOutput,
		HarnessDimensionOrchestration,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("dimensions = %+v, want %+v", got, want)
	}
}

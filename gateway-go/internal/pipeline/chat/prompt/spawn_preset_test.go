package prompt

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/toolpreset"
)

func TestSpawnPresetsMatchTheToolPresetPackage(t *testing.T) {
	declared := map[string]struct{}{}
	for _, p := range toolpreset.SpawnPresets() {
		declared[string(p)] = struct{}{}
	}
	if len(declared) != len(spawnPresets) {
		t.Fatalf("spawn preset drift: prompt=%v toolpreset=%v", spawnPresets, declared)
	}
	for name := range declared {
		if !isSpawnPreset(name) {
			t.Fatalf("preset %q is a spawn preset in toolpreset but not here", name)
		}
	}
}

// TestImplementerGetsTheImpactFirstProcedure pins the editing contract to the
// one preset that mutates source. The procedure is scored by RSI Bench's
// impact-first metric, so a silent drop here would show up as a score
// regression with no diff to explain it.
func TestImplementerGetsTheImpactFirstProcedure(t *testing.T) {
	impl := staticPromptFor(presetImplementer)
	for _, want := range []string{"codegraph_impact", "codegraph_node", "편집 전 필수"} {
		if !strings.Contains(impl, want) {
			t.Errorf("implementer prompt missing %q", want)
		}
	}
	// Read-only and verification children have no edits to precede, and the
	// main chat run is not a delegated coding lane.
	for _, preset := range []string{"", "researcher", "verifier"} {
		if got := staticPromptFor(preset); strings.Contains(got, "codegraph_impact") {
			t.Errorf("preset %q must not carry the implementer editing procedure", preset)
		}
	}
}

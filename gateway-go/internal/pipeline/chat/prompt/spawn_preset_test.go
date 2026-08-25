package prompt

import (
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

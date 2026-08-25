package prompt

// spawnPresets mirrors toolpreset.SpawnPresets() — the presets sessions_spawn
// accepts. Copied rather than imported to keep this leaf package free of the
// pipeline dependency; spawn_preset_test.go fails if the two ever drift.
//
// A run under one of these presets reports to its PARENT agent, not to a
// client: no card renders, so the prompt hands it a reporting rule instead of
// the rich-answer grammar.
var spawnPresets = map[string]struct{}{
	"researcher":  {},
	"implementer": {},
	"verifier":    {},
}

func isSpawnPreset(preset string) bool {
	_, ok := spawnPresets[preset]
	return ok
}

// Package toolpreset defines named tool allow-lists for role-based agent
// sessions. Each preset restricts which tools a sub-agent can use, enabling
// coordinator mode workflows where workers have scoped capabilities.
package toolpreset

// Preset identifies a named tool restriction profile.
type Preset string

const (
	PresetNone        Preset = ""            // no restriction — all tools available
	PresetResearcher  Preset = "researcher"  // read-only exploration tools
	PresetImplementer Preset = "implementer" // read + write + build tools
	PresetVerifier    Preset = "verifier"    // read + test + exec tools
	PresetCoordinator Preset = "coordinator" // orchestration tools only
)

// researcherTools are read-only exploration tools for codebase investigation.
var researcherTools = toSet(
	"read", "grep", "find", "tree", "diff", "analyze",
	"batch_read", "search_and_read", "inspect",
	"web", "http", "memory", "polaris",
)

// implementerTools include read + write + build tools for code changes.
var implementerTools = toSet(
	"read", "write", "edit", "multi_edit",
	"grep", "find", "tree", "diff", "analyze",
	"test", "exec", "process", "git", "apply_patch",
	"batch_read", "search_and_read", "inspect", "memory",
)

// verifierTools include read + test + exec tools for verification.
var verifierTools = toSet(
	"read", "grep", "find", "tree", "diff", "analyze",
	"test", "exec", "process",
	"batch_read", "search_and_read", "inspect", "memory",
)

// coordinatorTools are orchestration-only tools for the coordinator agent.
var coordinatorTools = toSet(
	"sessions_spawn", "subagents",
	"sessions_list", "sessions_history", "sessions_send",
	"read", "grep", "find", "memory", "kv",
)

// AllowedTools returns the set of tool names permitted for a given preset.
// Returns nil when preset is empty or unknown (meaning no restriction).
func AllowedTools(preset Preset) map[string]bool {
	switch preset {
	case PresetResearcher:
		return researcherTools
	case PresetImplementer:
		return implementerTools
	case PresetVerifier:
		return verifierTools
	case PresetCoordinator:
		return coordinatorTools
	default:
		return nil
	}
}

// IsValid returns true if the preset is a recognized value (including empty).
func IsValid(preset Preset) bool {
	switch preset {
	case PresetNone, PresetResearcher, PresetImplementer, PresetVerifier, PresetCoordinator:
		return true
	default:
		return false
	}
}

// KnownPresets returns all non-empty preset values.
func KnownPresets() []Preset {
	return []Preset{PresetResearcher, PresetImplementer, PresetVerifier, PresetCoordinator}
}

func toSet(names ...string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

// Package toolpreset defines named tool allow-lists for role-based agent sessions.
package toolpreset

// Preset identifies a named tool restriction profile.
type Preset string

const (
	PresetNone         Preset = ""             // no restriction — all tools available
	PresetConversation Preset = "conversation" // chat + web tools only (대화모드)
	PresetBoot         Preset = "boot"         // minimal tools for startup/daily check
)

// conversationTools are minimal tools for conversation mode (대화모드).
// Web access, wiki, plus read-only file inspection.
var conversationTools = toSet(
	"read",
	"web", "wiki", "fetch_tools",
)

// bootTools are the minimal set for startup and daily-check agent turns.
// kv for persistent memory/logging.
var bootTools = toSet(
	"kv",
)

// AllowedTools returns the set of tool names permitted for a given preset.
// Returns nil when preset is empty or unknown (meaning no restriction).
func AllowedTools(preset Preset) map[string]struct{} {
	switch preset {
	case PresetConversation:
		return conversationTools
	case PresetBoot:
		return bootTools
	default:
		return nil
	}
}

// IsValid returns true if the preset is a recognized value (including empty).
func IsValid(preset Preset) bool {
	switch preset {
	case PresetNone, PresetConversation, PresetBoot:
		return true
	default:
		return false
	}
}

// KnownPresets returns all non-empty preset values.
func KnownPresets() []Preset {
	return []Preset{PresetConversation, PresetBoot}
}

func toSet(names ...string) map[string]struct{} {
	m := make(map[string]struct{}, len(names))
	for _, n := range names {
		m[n] = struct{}{}
	}
	return m
}

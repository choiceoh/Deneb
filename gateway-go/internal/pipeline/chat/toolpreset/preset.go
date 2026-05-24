// Package toolpreset defines named tool allow-lists for role-based agent sessions.
package toolpreset

// Preset identifies a named tool restriction profile.
type Preset string

const (
	PresetNone         Preset = ""             // no restriction — all tools available
	PresetConversation Preset = "conversation" // chat + web tools only (대화모드)
	PresetBoot         Preset = "boot"         // minimal tools for startup/daily check
	PresetSelfReview   Preset = "self-review"  // background skill review / self-evolution only
	PresetBusiness     Preset = "business"     // default for Telegram user sessions (업무 모드)
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

// selfReviewTools are the Hermes-style allow-list for autonomous skill review.
// Keep this narrow: background self-improvement can inspect and mutate skills
// through the lifecycle surface, but it cannot send messages, run commands,
// touch memory/wiki, spawn agents, or schedule heartbeats.
var selfReviewTools = toSet(
	"fetch_tools",
	"skills",
	"skill_lifecycle",
)

// businessTools are the tools available in the default Telegram user session
// (업무 모드). Permissive by design: this preset's primary job is to mark
// sessions as "business" so future tightening lives in one place. The only
// current omission vs. PresetNone is the `git` tool, which was removed
// entirely in the coding-surface strip; if a coding-mode toggle is added
// later, that preset would re-add it.
var businessTools = toSet(
	// Files & text inspection (context files, wiki pages, attachment text)
	"read", "write", "edit", "grep",
	// Knowledge (curated KB + wiki concept graph + cross-session recall)
	"wiki", "graphify", "polaris",
	// Email
	"gmail",
	// Communication
	"message", "clarify", "send_file",
	// Scheduling + autonomy
	"cron", "heartbeat_update",
	// Web search/fetch
	"web",
	// Session lifecycle
	"sessions", "sessions_spawn", "subagents",
	// Skills
	"skills", "skill_lifecycle",
	// Shell access (gcalcli/pdftotext/etc. via the agent, plus operator commands)
	"exec", "process",
	// System + meta
	"gateway", "read_spillover", "fetch_tools",
	// Persistent key/value memory
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
	case PresetSelfReview:
		return selfReviewTools
	case PresetBusiness:
		return businessTools
	default:
		return nil
	}
}

// IsValid returns true if the preset is a recognized value (including empty).
func IsValid(preset Preset) bool {
	switch preset {
	case PresetNone, PresetConversation, PresetBoot, PresetSelfReview, PresetBusiness:
		return true
	default:
		return false
	}
}

// KnownPresets returns all non-empty preset values.
func KnownPresets() []Preset {
	return []Preset{PresetConversation, PresetBoot, PresetSelfReview, PresetBusiness}
}

func toSet(names ...string) map[string]struct{} {
	m := make(map[string]struct{}, len(names))
	for _, n := range names {
		m[n] = struct{}{}
	}
	return m
}

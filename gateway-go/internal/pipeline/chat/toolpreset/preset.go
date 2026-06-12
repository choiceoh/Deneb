// Package toolpreset defines named tool allow-lists for role-based agent sessions.
package toolpreset

// Preset identifies a named tool restriction profile.
type Preset string

const (
	PresetNone         Preset = ""             // no restriction — all tools available
	PresetConversation Preset = "conversation" // chat + web tools only (대화모드)
	PresetBoot         Preset = "boot"         // minimal tools for startup/daily check
	PresetSelfReview   Preset = "self-review"  // background skill review / self-evolution only
	PresetResearcher   Preset = "researcher"   // spawn preset: read-focused context gathering
	PresetImplementer  Preset = "implementer"  // spawn preset: researcher + file mutation + shell
	PresetVerifier     Preset = "verifier"     // spawn preset: read + build/test execution
)

// conversationTools are minimal tools for conversation mode (대화모드).
// Web access, wiki, plus read-only file inspection.
var conversationTools = toSet(
	"read",
	"web", "wiki", "fetch_tools",
)

// bootTools are the tools available to startup and daily-check agent turns.
// The boot turn checks system status, reviews overnight mail/schedule, inspects
// memory, and proactively notifies the user. gateway/gmail/cron/message are
// deferred, so they are listed here only to pass the Execute allow-list — the
// LLM loads their schemas on demand via fetch_tools.
var bootTools = toSet(
	"gateway", "gmail", "cron", "message", // deferred — loaded via fetch_tools
	"wiki", "knowledge", // memory / knowledge inspection
	"read",        // file reads
	"fetch_tools", // loads the deferred tools above
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

// Spawn presets back the sandbox the sessions_spawn schema promises
// (tool_preset enum: researcher/implementer/verifier). Tuned against actual
// sub-agent tool usage in ~/.deneb/agent-logs (2026-06-13 audit over 181
// spawned-child sessions: gmail 903, exec 638, read 594, wiki 539,
// fetch_tools 129, web 120 calls — mail/wiki research dominates delegation).
//
// Like bootTools, deferred tools (gmail/contacts/graphify/edit/process) must
// be listed by name: the allow-list gates the eager prompt listing, the
// deferred listing, fetch_tools activation, AND Execute — a deferred tool
// missing here stays invisible and uncallable for the preset.
//
// No spawn preset includes sessions_spawn/subagents: a restricted child
// spawning a preset-less (= unrestricted) grandchild would defeat the
// sandbox. Fan-out stays possible from the unrestricted parent. message and
// send_file are also excluded — sub-agents report back through the
// completion relay, not by messaging the user directly.

// researcherTools: read-focused context gathering — files, web, mail,
// wiki/knowledge/graph, contacts, session recall. wiki·knowledge keep their
// write sub-actions ("분석 → 위키 갱신" doctrine) and gmail keeps send/reply;
// per-action filtering would need a deeper mechanism than this name-based
// allow-list. The preset's job is removing the general-purpose escalation
// surfaces: shell, file writes, user messaging, scheduling, gateway
// self-management, spawn.
var researcherTools = toSet(
	"gmail", "contacts", "graphify", // deferred — loaded via fetch_tools
	"read", "grep", "read_spillover", // file inspection
	"web",                          // web search + page fetch
	"wiki", "knowledge", "polaris", // knowledge bases + recall
	"fetch_tools",
)

// implementerTools: researcher + file mutation + shell — the "do the work"
// preset for delegated changes that end in artifacts, not just findings.
var implementerTools = union(researcherTools, toSet(
	"edit", "process", // deferred — loaded via fetch_tools
	"write", "exec",
))

// verifierTools: read + execute, nothing else — build/test/behavior
// validation. No write surface (a verifier that can patch the code it judges
// defeats the role) and no research surfaces (web/mail/wiki belong to the
// researcher; verification evidence comes from running things).
var verifierTools = toSet(
	"process", // deferred — loaded via fetch_tools
	"read", "grep", "read_spillover",
	"exec",
	"fetch_tools",
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
	case PresetResearcher:
		return researcherTools
	case PresetImplementer:
		return implementerTools
	case PresetVerifier:
		return verifierTools
	default:
		return nil
	}
}

// IsValid returns true if the preset is a recognized value (including empty).
func IsValid(preset Preset) bool {
	switch preset {
	case PresetNone, PresetConversation, PresetBoot, PresetSelfReview,
		PresetResearcher, PresetImplementer, PresetVerifier:
		return true
	default:
		return false
	}
}

// KnownPresets returns all non-empty preset values.
func KnownPresets() []Preset {
	return []Preset{
		PresetConversation, PresetBoot, PresetSelfReview,
		PresetResearcher, PresetImplementer, PresetVerifier,
	}
}

// SpawnPresets returns the presets sessions_spawn accepts in its tool_preset
// parameter, in schema-enum order. Used for validation error messages.
func SpawnPresets() []Preset {
	return []Preset{PresetResearcher, PresetImplementer, PresetVerifier}
}

func toSet(names ...string) map[string]struct{} {
	m := make(map[string]struct{}, len(names))
	for _, n := range names {
		m[n] = struct{}{}
	}
	return m
}

func union(sets ...map[string]struct{}) map[string]struct{} {
	m := make(map[string]struct{})
	for _, s := range sets {
		for k := range s {
			m[k] = struct{}{}
		}
	}
	return m
}

// Package toolpreset defines named tool allow-lists for role-based agent sessions.
package toolpreset

// Preset identifies a named tool restriction profile.
type Preset string

const (
	PresetNone         Preset = ""              // no restriction — all tools available
	PresetConversation Preset = "conversation"  // chat + web tools only (대화모드)
	PresetBoot         Preset = "boot"          // minimal tools for startup/daily check
	PresetSelfReview   Preset = "self-review"   // background Propus skill review only
	PresetResearcher   Preset = "researcher"    // spawn preset: read-focused context gathering
	PresetImplementer  Preset = "implementer"   // spawn preset: researcher + file mutation + shell
	PresetVerifier     Preset = "verifier"      // spawn preset: read + build/test execution
	PresetWikiResearch Preset = "wiki-research" // autonomous wiki refresh: researcher minus web (internal sources only)
)

// conversationTools are minimal tools for conversation mode (대화모드).
// Web access, wiki, plus read-only file inspection.
var conversationTools = toSet(
	"read",
	"web", "wiki", "fetch_tools",
)

// bootTools are the tools available to startup and daily-check agent turns.
// The boot turn checks system status, reviews overnight mail/schedule, inspects
// memory, and proactively notifies the user. gateway/mail_archive/cron/message are
// deferred, so they are listed here only to pass the Execute allow-list — the
// LLM loads their schemas on demand via fetch_tools.
var bootTools = toSet(
	"gateway", "mail_archive", "cron", "message", // deferred — loaded via fetch_tools
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
// (tool_preset enum: researcher/implementer/verifier). Mail research uses the
// local archive; Gmail OAuth surfaces are not exposed to coding agents.
//
// Like bootTools, deferred tools (mail_archive/contacts/graphify/edit/process) must
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
// write sub-actions ("분석 → 위키 갱신" doctrine). Received mail always flows
// through mail_archive; direct Gmail OAuth access is intentionally absent.
var researcherTools = toSet(
	"mail_archive", "contacts", "graphify", // deferred — loaded via fetch_tools
	"read", "grep", "read_spillover", // file inspection
	"web",                          // web search + page fetch
	"wiki", "knowledge", "polaris", // knowledge bases + recall
	"fetch_tools",
)

// wikiResearchTools backs the autonomous wiki-refresh task (wiki_research_task.go):
// the researcher surface with web access removed. Derived from researcherTools so
// the two stay in sync by construction — add an internal-research source to
// researcher and this task picks it up automatically. Web is intentionally
// dropped: this is internal deep research, not external lookup, so no external
// API is called and no web-sourced text can pollute the curated memory. wiki
// keeps its write sub-action so the turn can persist the refresh.
var wikiResearchTools = without(researcherTools, "web")

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
	case PresetWikiResearch:
		return wikiResearchTools
	default:
		return nil
	}
}

// IsValid returns true if the preset is a recognized value (including empty).
func IsValid(preset Preset) bool {
	switch preset {
	case PresetNone, PresetConversation, PresetBoot, PresetSelfReview,
		PresetResearcher, PresetImplementer, PresetVerifier, PresetWikiResearch:
		return true
	default:
		return false
	}
}

// KnownPresets returns all non-empty preset values.
func KnownPresets() []Preset {
	return []Preset{
		PresetConversation, PresetBoot, PresetSelfReview,
		PresetResearcher, PresetImplementer, PresetVerifier, PresetWikiResearch,
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

// without returns a copy of set with the named keys removed. Symmetric to union;
// used to derive one preset from another (e.g. researcher minus web).
func without(set map[string]struct{}, names ...string) map[string]struct{} {
	m := make(map[string]struct{}, len(set))
	for k := range set {
		m[k] = struct{}{}
	}
	for _, n := range names {
		delete(m, n)
	}
	return m
}

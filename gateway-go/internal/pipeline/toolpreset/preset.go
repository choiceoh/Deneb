// Package toolpreset defines pipeline-neutral named tool allow-lists for
// role-based agent sessions.
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
	PresetWikiScout    Preset = "wiki-scout"    // autonomous external scouting: web + wiki only
	PresetNotiDigest   Preset = "noti-digest"   // autonomous notification digest: wiki only (no external, no personal stores)
	PresetCoding       Preset = "coding"        // 코드모드 (code: sessions): worktree coding, no 업무 memory/personal-data tools
	PresetBriefcase    Preset = "briefcase"     // isolated Deneb-Briefcase evaluation world
	PresetProjection   Preset = "projection"    // internal one-shot structured projection, no tools
)

// The impossible sentinel keeps the allow-list non-empty for the existing
// filter contract while exposing no registered tool to the model.
var projectionTools = toSet("__projection_no_tools__")

// conversationTools are minimal tools for conversation mode (대화모드).
// Web access, wiki, plus read-only file inspection.
var conversationTools = toSet(
	"read",
	"web", "wiki", "fetch_tools",
)

// bootTools are the tools available to startup and daily-check agent turns.
// The boot turn checks system status, reviews overnight mail/schedule, inspects
// memory, and proactively notifies the user. gateway/cron/message are deferred,
// so they are listed here only to pass the Execute allow-list (the LLM loads
// their schemas on demand via fetch_tools); mail_archive is eager and directly
// callable.
var bootTools = toSet(
	"gateway", "cron", "message", // deferred — loaded via fetch_tools
	"mail_archive",      // eager (received-mail hand)
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

// preloadedDeferred lists the deferred tools a preset wants ACTIVE from turn 1,
// skipping the fetch_tools dance. The self-review preset's whole job is to call
// skill_lifecycle (action=propose), and its narrow 3-tool surface has no cache
// reason to defer it — leaving it deferred made the review model do a
// fetch_tools -> call 2-step it routinely skipped, emitting a prose verdict with
// zero tool calls and no-oping every review. Pre-loading makes the review's one
// required tool directly callable.
// skills is preloaded too since the lean review system prompt (#3103): the
// reviewer is told to check existing skills via skills(action=list) before
// choosing evolve vs genesis, and without the main prompt's deferred-tool
// listing it would not even learn the fetch_tools escape hatch.
// PresetImplementer preloads the two codegraph tools its procedure names.
// Same reasoning as self-review: a step the preset REQUIRES must not sit behind
// a fetch_tools dance the model routinely skips. Only impact + node are
// preloaded (blast radius before an edit, precise symbol body) — the remaining
// four stay deferred and reachable via fetch_tools, so the turn-1 tool array
// grows by two, not six. Names absent from the registry (codegraph not
// configured, or MCP discovery still in flight) are dropped by
// DeferredLLMTools, so this is safe on a host without the server.
var preloadedDeferred = map[Preset][]string{
	PresetSelfReview:  {"skill_lifecycle", "skills"},
	PresetImplementer: {"codegraph_impact", "codegraph_node"},
}

// PreloadedDeferredTools returns the deferred tools to load as active (callable
// from turn 1) for a preset, or nil to keep the normal fetch-on-demand behavior.
func PreloadedDeferredTools(preset Preset) []string {
	return preloadedDeferred[preset]
}

// Spawn presets back the sandbox the sessions_spawn schema promises
// (tool_preset enum: researcher/implementer/verifier). Mail research uses the
// local archive; Gmail OAuth surfaces are not exposed to coding agents.
//
// Like bootTools, deferred tools (contacts/graphify/edit/process) must be listed
// by name: the allow-list gates the eager prompt listing, the deferred listing,
// fetch_tools activation, AND Execute — a deferred tool missing here stays
// invisible and uncallable for the preset. (mail_archive is eager but the same
// naming requirement applies — an eager tool absent from the list is dropped too.)
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
	"mail_archive",                   // eager (received-mail hand)
	"contacts",                       // deferred — loaded via fetch_tools
	"graphify",                       // deferred — loaded via fetch_tools
	"read", "grep", "read_spillover", // file inspection
	"web",                          // web search + page fetch
	"wiki", "knowledge", "polaris", // knowledge bases + recall
	"blackboard", // typed multi-tool I/O contracts
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

// wikiScoutTools backs the autonomous external-scouting task
// (wiki_scout_task.go). Deliberately NARROW, not the researcher surface: the
// scout's context carries fetched untrusted web pages, and the background
// SendSync path has no untrusted-origin tool gate — so the personal-memory
// surfaces (mail_archive/contacts/polaris/graphify) and file reads must not
// be reachable from the same turn, or a prompt-injected page could steer
// internal reads and leak them through a later web query. web is the job;
// wiki stays for the three permitted writes (자료 ingest / 로그 op / 미해결
// 질문 불릿 제거) and wiki reads, which are the scout's working set anyway.
var wikiScoutTools = toSet(
	"web",
	"wiki",
	"fetch_tools",
)

// notiDigestTools backs the notification-digest task (noti_digest_task.go).
// The batch content is raw third-party app text (KakaoTalk/SMS/e-approval), so
// the turn must reach NO personal-memory store: mail_archive, contacts,
// graphify, polaris, and knowledge are all withheld, or a malicious
// notification could steer the turn into reading private data and persisting
// it through a wiki write (GateUntrustedTools only blocks exec-class tools).
// wiki alone covers the whole job — read to find the project, write the 로그
// op / person update.
var notiDigestTools = toSet(
	"wiki",
	"fetch_tools",
)

// codegraphTools are the external codegraph MCP tools (registered by
// externalmcp from DENEB_MCP_SERVERS as codegraph_<kind>). Listed by name for
// the same reason every other deferred tool is: the allow-list gates the
// deferred prompt listing, fetch_tools activation, AND Execute — absent here,
// an implementer child could not see or call them at all. Measured 2026-08-29:
// zero codegraph_* calls across the whole agent-log window, because the preset
// made them unreachable, not because the model declined them.
//
// Deliberately NOT folded into researcherTools: that set is the base for the
// autonomous wiki-research preset, which has no business navigating source. The
// symbol graph belongs to the lane that edits code.
var codegraphTools = toSet(
	"codegraph_impact", "codegraph_callers", "codegraph_callees",
	"codegraph_node", "codegraph_search", "codegraph_explore",
)

// implementerTools: researcher + file mutation + shell — the "do the work"
// preset for delegated changes that end in artifacts, not just findings.
// codegraph rides along: the dependency graph is what keeps an edit from being
// made blind (see impactFirstProcedure in the chat prompt).
var implementerTools = union(researcherTools, codegraphTools, toSet(
	"edit", "process", // deferred — loaded via fetch_tools
	"write", "exec",
))

// codingTools back 코드모드 (code: sessions, ConfigureCoding): file inspection +
// mutation + shell + web docs lookup, and nothing personal. Deliberately NOT
// derived from implementerTools: that preset carries the Deneb 업무 memory
// surfaces (mail_archive/contacts/wiki/knowledge/polaris/graphify), which are
// noise — and a privacy leak — inside an external GitHub repo worktree. The
// coding profile withholds the 업무 context from the prompt (run_prepare), so
// the tool surface must match or the swap is half-done. No sessions_spawn: a
// spawned child would not inherit the worktree binding and would edit the
// default workspace (follow-up if fan-out is ever needed).
var codingTools = toSet(
	"edit", "process", // deferred — loaded via fetch_tools
	"read", "grep", "read_spillover",
	"write", "exec",
	"web",
	"blackboard", // typed multi-tool I/O contracts
	"fetch_tools",
)

// briefcaseTools is the fail-closed surface for scored Deneb-Briefcase runs.
// Every stateful or external dependency is replaced by a case-local fixture
// before this preset is selected. Network access, arbitrary shell execution,
// external delivery, scheduling, and agent fan-out stay unavailable in v1.
//
// write/edit are intentionally present: benchmark tasks may create artifacts,
// but the briefcase runtime binds their workspace to a disposable RunRoot.
var briefcaseTools = toSet(
	"mail_archive", "contacts", "files", "calendar", "todo",
	"phone_read", "phone_write",
	"wiki", "knowledge", "polaris", "notebook",
	"read", "grep", "write", "edit",
)

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
	case PresetWikiScout:
		return wikiScoutTools
	case PresetNotiDigest:
		return notiDigestTools
	case PresetCoding:
		return codingTools
	case PresetBriefcase:
		return briefcaseTools
	case PresetProjection:
		return projectionTools
	default:
		return nil
	}
}

// IsValid returns true if the preset is a recognized value (including empty).
func IsValid(preset Preset) bool {
	switch preset {
	case PresetNone, PresetConversation, PresetBoot, PresetSelfReview,
		PresetResearcher, PresetImplementer, PresetVerifier, PresetWikiResearch,
		PresetWikiScout, PresetNotiDigest, PresetCoding, PresetBriefcase, PresetProjection:
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
		PresetWikiScout, PresetNotiDigest, PresetCoding, PresetBriefcase, PresetProjection,
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

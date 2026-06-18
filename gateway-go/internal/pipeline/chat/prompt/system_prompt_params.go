// system_prompt_params.go — system prompt parameter/type declarations
// (SystemPromptParams, RuntimeInfo, ToolDef, reply tokens) plus timezone and
// runtime-line helpers. Split from system_prompt.go (pure move, ~700-LOC rule);
// prompt assembly and the cache-key/static-block logic stay in system_prompt.go.

package prompt

import (
	"fmt"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/dentime"
)

// SilentReplyToken is the token that suppresses message delivery when the LLM
// replies with exactly this value (with optional surrounding whitespace).
// The same value is defined independently in chat/silent_reply.go — both must stay in sync.
const SilentReplyToken = "NO_REPLY"

// HeartbeatTriggerPrefix marks user-role messages injected by the autonomous
// 5-minute heartbeat task. The system prompt instructs the agent to treat
// messages starting with this prefix as self-triggers, not real user input,
// so the model does not start modeling the user as constantly asking for
// system checks. The heartbeat task in runtime/server references this
// constant — keep them as a single source of truth.
const HeartbeatTriggerPrefix = "[시스템 하트비트]"

// PromptIDSystemPersona is the prompt-store ID for the editable Nev identity +
// 역할 (chief-of-staff role) text that opens the 업무 (non-chatbot) Static block.
// The Settings prompt corner edits it; run_prepare passes any override as
// SystemPromptParams.PersonaText with a content-hash PersonaCacheKey so the
// Static cache entry is keyed per persona (vLLM APC stays byte-stable within a
// session, and an edit invalidates only its own entry). No override →
// PersonaText "" → the DefaultPersona bytes and the unchanged cache key keep the
// existing Static entry. The server registry (server/prompt_store.go) uses this
// ID + DefaultPersona, so the two stay a single source of truth.
const PromptIDSystemPersona = "system.persona"

// DefaultPersona is the default identity + 역할 text rendered at the top of the
// 업무 Static block. It is the single source of truth for both the rendered
// default (buildPromptSections) and the prompt-store registry default
// (server/prompt_store.go) so "reset" restores byte-identical behavior. Edits
// here must keep the concatenation byte-identical to the prior three inline
// WriteString calls (Identity, "## 역할" header, role body).
const DefaultPersona = "You are Nev — a personal assistant running inside Deneb (https://github.com/choiceoh/deneb). Deneb is a single-user AI agent platform on DGX Spark.\n\n" +
	"## 역할\n" +
	"당신은 비서실장형 단일 에이전트다 — 분석가와 비서를 분리하지 않는다. **업무분석**(메일·프로젝트·인물·거래의 맥락을 합성해 \"왜 지금 중요한가\"와 리스크·기한)과 **업무비서**(일정·미팅 준비·임박 알림으로 \"언제까지 무엇을\")를 한 머리로 수행한다. 좋은 답에는 분석의 '왜'와 비서의 '언제까지'가 한 응답에 함께 담긴다 — 둘을 분리된 응답이나 탭으로 가르지 마라.\n\n"

// ToolDef describes a tool entry for the system prompt (name only; used to
// build the compact tool list and conditional prompt sections).
// This is a minimal view of chat.ToolDef — only the fields needed for prompt
// assembly are included to avoid a circular import between chat/ and chat/prompt/.
type ToolDef struct {
	Name string
}

// DeferredToolInfo describes a deferred tool for system prompt listing.
type DeferredToolInfo struct {
	Name        string
	Description string
}

// LoadCachedTimezone resolves and caches timezone once.
// Exported so callers (e.g., chat/run.go) can read the resolved timezone
// without duplicating the resolution logic.
func LoadCachedTimezone() (string, *time.Location) {
	return Cache.Timezone()
}

// loadCachedTimezone is the unexported alias used within this package.
func loadCachedTimezone() (string, *time.Location) {
	return Cache.Timezone()
}

// SystemPromptParams holds all parameters for building the agent system prompt.
type SystemPromptParams struct {
	WorkspaceDir  string
	ToolDefs      []ToolDef
	DeferredTools []DeferredToolInfo // deferred tools: name+description listed in prompt
	SkillsPrompt  string             // pre-built skills XML from skills/prompt.go
	UserTimezone  string
	ContextFiles  []ContextFile
	RuntimeInfo   *RuntimeInfo
	Channel       string
	DocsPath      string
	ToolPreset    string // active tool preset ("conversation" etc.); empty = normal mode

	// Chatbot selects the clean general-purpose assistant prompt for the 챗봇
	// workspace (chat: sessions): a neutral identity instead of the Nev
	// chief-of-staff persona, and the 업무 work-loop sections (비서실장 role,
	// 분석→위키, 작업 기억, 위키 외부메모리) are dropped — so 챗봇 reads like a
	// vanilla general assistant. The tool surface is unchanged. The work context
	// inputs (ContextFiles, TopicKnowledge, CalendarGlance, tier-1 wiki) are
	// withheld upstream (run_prepare) for chat: sessions, so the existing
	// empty-input guards skip them. It folds into the Static cache key so 챗봇 and
	// 업무 never share a Static entry; false leaves the key byte-identical to
	// before (업무 prompt unchanged).
	Chatbot bool

	// CompactionFired triggers a one-time-per-session reminder appended
	// to the dynamic block: tells the model that prior turns have been
	// compacted into summaries so summary messages should be treated as
	// reference context, not as user input. Sticky for session lifetime —
	// the note's bytes are present every turn after first compaction so
	// the system prompt stays byte-stable from that point onward (P4).
	CompactionFired bool

	// CalendarGlance is a compact, pre-formatted summary of upcoming calendar
	// events for ambient awareness (the 업무비서 persona's 일정 sense). Empty =
	// no section. Dynamic (uncached) block, but frozen per day by the provider
	// (see chat/calendar_glance.go) so it stays byte-stable within a day like
	// the day-only timestamp — preserving the trailing-message cache
	// (.claude/rules/prompt-cache.md). The live `calendar` tool remains the
	// authoritative source; this is background context only.
	CalendarGlance string

	// TopicKnowledge is per-forum-topic background knowledge merged into the
	// Static (cached) block. Empty = no topic section. It is byte-stable for
	// the session (frozen snapshot in LoadTopicKnowledge) so the Static cache
	// key stays fixed across turns.
	TopicKnowledge string
	// TopicCacheKey distinguishes the Static cache slot per topic, formatted
	// "<topicKey>:<contentHash>". Empty = no topic. Folded into
	// buildStaticCacheKey so (a) different topics never share a Static cache
	// entry and (b) editing a topic .md invalidates it.
	TopicCacheKey string
	// TopicKnowledgePath is TopicKnowledge's absolute source file, surfaced in
	// the prompt so the agent can edit the doc when asked — the chat agent is
	// the only edit surface (the settings topic-docs tab was removed). Derived
	// from the topic key, so the Static cache key needs no extra input; frozen
	// per session together with TopicKnowledge.
	TopicKnowledgePath string

	// PersonaText overrides the default Nev identity + 역할 text at the top of the
	// 업무 (non-chatbot) Static block, edited via the Settings prompt corner
	// (PromptIDSystemPersona). Empty = use DefaultPersona (byte-identical to
	// before). Only the 업무 path consumes it; 챗봇 keeps its neutral identity.
	// It is byte-stable between edits (rare operator action) so the Static cache
	// holds; PersonaCacheKey keys the cache per persona content.
	PersonaText string
	// PersonaCacheKey is the sha256[:12] of an edited PersonaText, folded into
	// buildStaticCacheKey so (a) an edited persona never shares the default
	// persona's Static cache entry and (b) editing the persona invalidates it.
	// Empty = no override → the Static cache key stays byte-identical to before
	// (the default-persona entry is preserved). Use PersonaCacheKeyFor to derive.
	PersonaCacheKey string
}

// RuntimeInfo describes the current runtime environment for the system prompt.
type RuntimeInfo struct {
	AgentID      string
	Host         string
	OS           string
	Arch         string
	Model        string
	DefaultModel string
	Channel      string
}

// buildRuntimeLine constructs the Runtime info line for the system prompt.
func buildRuntimeLine(info *RuntimeInfo, channel string) string {
	parts := []string{"Runtime:"}

	if info != nil {
		if info.AgentID != "" {
			parts = append(parts, fmt.Sprintf("agent=%s", info.AgentID))
		}
		if info.Host != "" {
			parts = append(parts, fmt.Sprintf("host=%s", info.Host))
		}
		if info.OS != "" {
			parts = append(parts, fmt.Sprintf("os=%s(%s)", info.OS, info.Arch))
		}
		if info.Model != "" {
			parts = append(parts, fmt.Sprintf("model=%s", info.Model))
		}
		if info.DefaultModel != "" {
			parts = append(parts, fmt.Sprintf("default_model=%s", info.DefaultModel))
		}
	}

	if channel != "" {
		parts = append(parts, fmt.Sprintf("channel=%s", channel))
	}

	return strings.Join(parts, " ")
}

// resolveTimezone returns the display name for the system prompt's date line.
//
// It defers to pkg/dentime, the canonical Deneb clock, which resolves the zone
// from DENEB_TIMEZONE then the config "timezone" key then the server-local
// zone. The earlier implementation read only the TZ env var and the local zone
// abbreviation, so a deployment that set its zone via deneb.json (the
// documented path) — common on UTC containers — showed the wrong date/zone here
// while logs, cron, and the calendar briefing (all dentime-based) used the
// right one.
func resolveTimezone() string {
	if name := dentime.Name(); name != "" && name != "Local" {
		return name // IANA name, e.g. "Asia/Seoul"
	}
	// Fell back to the server-local zone. Prefer a human abbreviation over the
	// opaque "Local"; LoadLocation is no longer used on this value.
	if zone, _ := dentime.Now().Zone(); zone != "" && zone != "UTC" {
		return zone
	}
	return "UTC"
}

// BuildDefaultRuntimeInfo creates RuntimeInfo from the current environment.
// Static fields (hostname, OS, arch) are cached; only model fields change per request.
func BuildDefaultRuntimeInfo(model, defaultModel string) *RuntimeInfo {
	return Cache.BuildRuntimeInfo(model, defaultModel)
}

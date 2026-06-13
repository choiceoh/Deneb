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

	// CompactionFired triggers a one-time-per-session reminder appended
	// to the dynamic block: tells the model that prior turns have been
	// compacted into summaries so summary messages should be treated as
	// reference context, not as user input. Sticky for session lifetime —
	// the note's bytes are present every turn after first compaction so
	// the system prompt stays byte-stable from that point onward (P4).
	CompactionFired bool

	// HindsightEnabled adds a short dynamic-block note telling the model it
	// has a cross-session Hindsight memory bank: past turns are retained
	// automatically and relevant memories arrive via <recall-context>.
	// Dynamic (uncached) block — no prompt-cache impact.
	HindsightEnabled bool

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

	// SupportsRichUI gates the deneb-ui interactive-UI instructions in the
	// dynamic block. True only for clients that can render deneb-ui fences
	// (the native Android app); other clients leave it false so their prompt
	// bytes are unchanged. Dynamic (uncached) block — no prompt-cache impact.
	SupportsRichUI bool
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

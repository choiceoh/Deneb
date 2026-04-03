package prompt

import (
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

// SilentReplyToken is the token that suppresses message delivery when the LLM
// replies with exactly this value (with optional surrounding whitespace).
// The same value is defined independently in chat/silent_reply.go — both must stay in sync.
const SilentReplyToken = "NO_REPLY"

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

// cachedTimezone and cachedTimezoneLocation cache the resolved timezone
// at startup to avoid time.LoadLocation() on every chat message.
var (
	cachedTimezone     string
	cachedTimezoneLoc  *time.Location
	cachedTimezoneOnce sync.Once
)

// staticPromptCache caches the assembled static system prompt block, keyed on
// the sorted tool name list. Invalidated only when the tool set changes (i.e.
// never during normal operation after server start).
var (
	staticPromptMu     sync.RWMutex
	staticPromptKey    string
	staticPromptCached string
)

// LoadCachedTimezone resolves and caches timezone once.
// Exported so callers (e.g., chat/run.go) can read the resolved timezone
// without duplicating the resolution logic.
func LoadCachedTimezone() (string, *time.Location) {
	cachedTimezoneOnce.Do(func() {
		cachedTimezone = resolveTimezone()
		loc, err := time.LoadLocation(cachedTimezone)
		if err == nil {
			cachedTimezoneLoc = loc
		}
	})
	return cachedTimezone, cachedTimezoneLoc
}

// loadCachedTimezone is the unexported alias used within this package.
func loadCachedTimezone() (string, *time.Location) {
	return LoadCachedTimezone()
}

// SystemPromptParams holds all parameters for building the agent system prompt.
type SystemPromptParams struct {
	WorkspaceDir   string
	ToolDefs       []ToolDef
	DeferredTools  []DeferredToolInfo // deferred tools: name+description listed in prompt
	SkillsPrompt   string            // pre-built skills XML from skills/prompt.go
	UserTimezone   string
	ContextFiles   []ContextFile
	RuntimeInfo    *RuntimeInfo
	Channel        string
	DocsPath       string
	SessionMemory  string // pre-formatted session state block (empty = omit)
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

// toolCategories defines tool groupings for the compact tool list.
// Only tools actually registered are shown (filtered at render time).
var toolCategories = []struct {
	Label string
	Names []string
}{
	{"File", []string{"read", "write", "edit", "grep", "find", "search_and_read", "batch_read"}},
	{"Code", []string{"multi_edit", "tree", "diff", "analyze", "inspect", "test"}},
	{"Git", []string{"git"}},
	{"Exec", []string{"exec", "process"}},
	{"AI", []string{"pilot"}},
	{"Web", []string{"web", "http"}},
	{"Memory", []string{"memory"}},
	{"System", []string{"message", "gateway"}},
	{"Routine", []string{"cron", "gmail", "morning_letter"}},
	{"Sessions", []string{"sessions_list", "sessions_history", "sessions_search", "sessions_send", "sessions_spawn", "subagents"}},
	{"Media", []string{"image", "youtube_transcript", "send_file"}},
	{"Data", []string{"kv"}},
}

// buildStaticCacheKey returns a stable string key for the static prompt block
// based on the sorted tool name list.
func buildStaticCacheKey(toolDefs []ToolDef) string {
	names := make([]string, 0, len(toolDefs))
	for _, d := range toolDefs {
		names = append(names, d.Name)
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

// buildPromptSections assembles the system prompt into static, semi-static, and dynamic parts.
// Static: identity, tooling, usage guides, safety, CLI reference (rarely changes).
// Semi-static: skills prompt (changes only when skills are added/removed, not per request).
// Dynamic: memory, workspace, context files, runtime (changes per request).
func buildPromptSections(params SystemPromptParams) (staticText, semiStaticText, dynamicText string) {
	// eagerSet: only eager tools (for compact tool list display).
	eagerSet := make(map[string]bool, len(params.ToolDefs))
	for _, def := range params.ToolDefs {
		eagerSet[def.Name] = true
	}
	// toolSet: eager + deferred (for conditional prompt sections like pilot, sessions_spawn).
	toolSet := make(map[string]bool, len(params.ToolDefs)+len(params.DeferredTools))
	for k, v := range eagerSet {
		toolSet[k] = v
	}
	for _, dt := range params.DeferredTools {
		toolSet[dt.Name] = true
	}

	// --- Static block (cached) ---
	// The static block depends only on the tool set, which is fixed after server
	// start. Cache it to avoid rebuilding ~2 KB of strings on every request.
	cacheKey := buildStaticCacheKey(params.ToolDefs)
	staticPromptMu.RLock()
	if staticPromptKey == cacheKey {
		staticText = staticPromptCached
		staticPromptMu.RUnlock()
	} else {
		staticPromptMu.RUnlock()
		var s strings.Builder

		// Identity.
		s.WriteString("You are Nev — a personal assistant running inside Deneb (https://github.com/choiceoh/deneb). Deneb is a single-user AI agent platform on DGX Spark.\n\n")

		// Communication.
		s.WriteString("## Communication\n")
		s.WriteString("Lead with the answer, not the explanation. Be direct and practically helpful.\n")
		s.WriteString("Match the user's tone and register naturally. Default to Korean.\n")
		s.WriteString("Skip filler like \"Great question!\" or \"I'd be happy to help.\" Let results build trust, not words.\n\n")

		// Attitude.
		s.WriteString("## Attitude\n")
		s.WriteString("If you see a better option, say so. You don't have to agree with everything.\n")
		s.WriteString("Call out what's inefficient or awkward. An assistant with no point of view is just a search engine wearing a sentence.\n\n")

		// How to Act.
		s.WriteString("## How to Act\n")
		s.WriteString("Check before you ask — read files, scan context, connect prior information, search if needed. Try to solve it yourself first; only ask when you genuinely have to.\n")
		s.WriteString("Be proactive with internal work: reading, organizing, analyzing, learning.\n")
		s.WriteString("Be careful with outbound actions: sending emails, messages, or publishing anything.\n\n")

		// Progress narration.
		s.WriteString("## 작업 과정 설명\n")
		s.WriteString("복잡한 작업을 수행할 때는 중간중간 지금 무엇을 하고 있는지, 왜 하는지, 다음에 무엇을 할 것인지 짧게 설명하세요.\n")
		s.WriteString("- 단순한 질문이나 1-2번의 도구 호출로 끝나는 작업은 설명 없이 바로 결과만 전달하세요.\n")
		s.WriteString("- 3단계 이상의 탐색, 분석, 수정이 필요한 작업에서는 각 단계의 의도를 한 문장으로 공유하세요.\n")
		s.WriteString("- 예시: '관련 설정이 어디서 로드되는지 먼저 확인하겠습니다' → (도구 호출) → '설정 파일을 찾았습니다. 이제 값을 수정합니다'\n")
		s.WriteString("- 기술적 세부사항(함수명, 파일 경로 등)은 사용자가 개발자일 때만 포함하세요.\n")
		s.WriteString("- 진행 상황 설명은 간결하게. 한 단계에 한 문장이면 충분합니다.\n\n")

		// Trust and Respect.
		s.WriteString("## Trust and Respect\n")
		s.WriteString("The user has granted access to their messages, files, calendar, and private information. That is not just a permission — it is trust and intimacy. Always behave like a guest: act with respect, care, and accountability.\n\n")

		// Tooling: compact categorized list (descriptions are in tool schemas).
		s.WriteString("## Tooling\n")
		s.WriteString("Available tools (see tool schemas for details). Names are case-sensitive.\n")
		writeCompactToolList(&s, eagerSet)
		if len(params.DeferredTools) > 0 {
			s.WriteString("\nDeferred tools (call `fetch_tools` to activate before use):\n")
			for _, dt := range params.DeferredTools {
				fmt.Fprintf(&s, "- %s: %s\n", dt.Name, truncateDescription(dt.Description, 80))
			}
		}
		s.WriteString("\n")

		// Tool Usage (compressed: parallel, first-class, CLI, pilot, chaining).
		s.WriteString("## Tool Usage\n")
		s.WriteString("- Act immediately: call tools in parallel when independent, never ask confirmation for reversible ops, never ask the user to do what you can do yourself.\n")
		s.WriteString("- Use first-class tools directly: grep not exec+grep, edit not exec+sed, gmail not manual API calls. `grep`/`find`/`tree` are fast; prefer them over shelling out.\n")
		s.WriteString("- For shell commands prefer `rg/fd/bat/eza/sd/dust/duf/procs/fx/ouch/btm`.\n")
		s.WriteString("- Prefer edit over write for partial changes (smaller token footprint).\n")
		s.WriteString("- Any tool input accepts optional \"compress\": true — large output auto-summarized by local AI, saving context tokens.\n")
		s.WriteString("- Outputs over 64K chars are auto-trimmed (head+tail), grep >200 lines capped, find >500 grouped.\n")
		s.WriteString("- find/tree results are cached within a run. Avoid re-calling with the same pattern unless you've modified files.\n")
		if toolSet["pilot"] {
			s.WriteString("- `pilot`: 여러 소스 종합/요약에 사용. 단일 작업(읽기/검색/실행)은 read/grep/exec 직접 호출.\n")
			s.WriteString("- Tool chaining: `\"$ref\": \"<tool_use_id>\"`로 도구 간 출력 전달 (`_ref_content`, 30s timeout).\n")
		}
		s.WriteString("- Deneb CLI: `deneb gateway {status|start|stop|restart}`. Do not invent subcommands.\n")
		s.WriteString("- **Never output tool call syntax or shell commands as text to the user.** Always use structured tool calls. Report results, not the commands you ran.\n\n")

		// Sub-agent delegation guidance.
		if toolSet["sessions_spawn"] {
			s.WriteString("## Sub-Agents\n")
			s.WriteString("Use `sessions_spawn` to delegate work in parallel. Don't do everything yourself.\n")
			s.WriteString("- **Investigation**: spawn a researcher to explore while you continue thinking.\n")
			s.WriteString("- **Independent subtasks**: if a task decomposes into 2+ independent parts, spawn workers.\n")
			s.WriteString("- **Verification**: after changes, spawn a verifier to test while you prepare the summary.\n")
			s.WriteString("Always set `tool_preset` (researcher/implementer/verifier). Monitor with `subagents(action:'list')`.\n")
			s.WriteString("Depth limit: 5, breadth limit: 10. Prefer fewer focused agents over many trivial ones.\n\n")
		}

		built := s.String()
		staticPromptMu.Lock()
		staticPromptKey = cacheKey
		staticPromptCached = built
		staticPromptMu.Unlock()
		staticText = built
	} // end else (cache miss)

	// --- Semi-static block (skills — changes only when skills are added/removed) ---
	var ss strings.Builder
	if params.SkillsPrompt != "" {
		ss.WriteString("## Skills (mandatory)\n")
		ss.WriteString("Before replying: scan <available_skills> <description> entries.\n")
		ss.WriteString("- If exactly one skill clearly applies: read its SKILL.md at <location> with `read`, then follow it.\n")
		ss.WriteString("- If multiple could apply: choose the most specific one, then read/follow it.\n")
		ss.WriteString("- If none clearly apply: do not read any SKILL.md.\n")
		ss.WriteString("Constraints: never read more than one skill up front; only read after selecting.\n")
		ss.WriteString(params.SkillsPrompt)
		ss.WriteString("\n\n")
	}

	// --- Dynamic block ---
	var d strings.Builder

	// Memory Recall.
	if toolSet["memory"] {
		d.WriteString("## Memory\n")
		d.WriteString("과거 대화, 사용자 선호, 이전 결정 등에 대한 정보가 불확실하거나 부족할 때 `memory` 도구를 사용하세요.\n")
		d.WriteString("특히 사용자가 과거 맥락을 언급하거나, 이전에 논의한 내용을 참조할 때는 반드시 recall을 먼저 호출하세요.\n\n")
		d.WriteString("- `memory(action=recall, query=...)`: 깊은 기억 회상 (엔티티 확장 + 관계 체인 + LLM 정리). 과거 맥락이 필요할 때 우선 사용\n")
		d.WriteString("- `memory(action=search, query=...)`: 통합 검색 (팩트 + 파일)\n")
		d.WriteString("- `memory(action=get, fact_id=N)`: 특정 팩트 상세 조회\n")
		d.WriteString("- `memory(action=set, query=..., category=...)`: 새 팩트 생성\n")
		d.WriteString("- `memory(action=forget, fact_id=N)`: 팩트 삭제\n")
		d.WriteString("- `memory(action=status)`: 메모리 상태 요약\n\n")
		d.WriteString("### Diary (일지)\n")
		d.WriteString("매우 상세한 서술형 일지. SQL 팩트와 달리 맥락, 과정, 이유, 결과를 풍부하게 기록.\n")
		d.WriteString("- `memory(action=log, query=..., title=...)`: 다이어리에 상세 기록 추가 (memory/diary/diary-YYYY-MM-DD.md)\n")
		d.WriteString("- `memory(action=daily, days=N)`: 최근 N일 다이어리 읽기 (기본: 오늘+어제)\n")
		d.WriteString("다이어리 작성 시 포함할 내용: 대화 내용, 사용한 도구, 내린 결정, 발생한 이벤트, 오류와 해결 과정, 사용자 반응.\n")
		d.WriteString("2시간마다 하트비트에서 자동으로 새로 발생한 일을 상세히 기록합니다.\n\n")
	}

	// Messaging (merged: Reply Tags + Messaging + Silent Replies).
	d.WriteString("## Messaging\n")
	d.WriteString("- Telegram 4096 char limit. Split with message tool if needed.\n")
	d.WriteString("- Reply tags: [[reply_to_current]] replies to triggering message (stripped before sending).\n")
	d.WriteString("- Current session replies auto-route to source channel. Cross-session: sessions_send(sessionKey, msg).\n")
	if toolSet["message"] {
		d.WriteString(fmt.Sprintf("- `message` for proactive sends + channel actions. If used for user-visible reply, respond with ONLY: %s.\n", SilentReplyToken))
	}
	d.WriteString("\n")

	// Session State (structured session memory from previous runs).
	if params.SessionMemory != "" {
		d.WriteString(params.SessionMemory)
		d.WriteString("\n")
	}

	// Context (merged: Workspace + Date/Time + Context Files + Runtime).
	d.WriteString("## Context\n")
	fmt.Fprintf(&d, "Workspace: %s\n", params.WorkspaceDir)
	tz := params.UserTimezone
	if tz == "" {
		tz, _ = loadCachedTimezone()
	}
	now := time.Now()
	_, cachedLoc := loadCachedTimezone()
	if cachedLoc != nil && tz == cachedTimezone {
		now = now.In(cachedLoc)
	} else if loc, err := time.LoadLocation(tz); err == nil {
		now = now.In(loc)
	}
	fmt.Fprintf(&d, "%s (timezone: %s)\n", now.Format("Monday, January 2, 2006 — 15:04"), tz)
	contextPrompt := FormatContextFilesForPrompt(params.ContextFiles)
	if contextPrompt != "" {
		d.WriteString(contextPrompt)
	}
	d.WriteString(buildRuntimeLine(params.RuntimeInfo, params.Channel))
	d.WriteString("\n")

	return staticText, ss.String(), d.String()
}

// BuildSystemPrompt assembles the full system prompt as a single string.
func BuildSystemPrompt(params SystemPromptParams) string {
	staticText, semiStaticText, dynamicText := buildPromptSections(params)
	return staticText + semiStaticText + dynamicText
}

// BuildSystemPromptBlocks returns the system prompt as Anthropic ContentBlocks
// with cache_control breakpoints. The prompt is split into three blocks:
//   - Static: identity, communication, attitude, tooling (rarely changes)
//   - Semi-static: skills prompt (changes only when skills are added/removed)
//   - Dynamic: memory, messaging, context (changes per request)
//
// Each block gets an ephemeral cache_control marker so Anthropic can cache the
// static and semi-static prefixes across requests. Skills are typically 10-15K
// tokens, so caching them separately yields meaningful input token savings.
func BuildSystemPromptBlocks(params SystemPromptParams) []llm.ContentBlock {
	staticText, semiStaticText, dynamicText := buildPromptSections(params)
	ephemeral := &llm.CacheControl{Type: "ephemeral"}
	blocks := []llm.ContentBlock{
		{Type: "text", Text: staticText, CacheControl: ephemeral},
	}
	if semiStaticText != "" {
		blocks = append(blocks, llm.ContentBlock{Type: "text", Text: semiStaticText, CacheControl: ephemeral})
	}
	blocks = append(blocks, llm.ContentBlock{Type: "text", Text: dynamicText, CacheControl: ephemeral})
	return blocks
}

// truncateDescription truncates a description to maxLen runes, appending "..." if needed.
func truncateDescription(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

// writeCompactToolList writes a categorized tool name list (no descriptions).
func writeCompactToolList(sb *strings.Builder, toolSet map[string]bool) {
	for _, cat := range toolCategories {
		var present []string
		for _, name := range cat.Names {
			if toolSet[name] {
				present = append(present, name)
			}
		}
		if len(present) > 0 {
			fmt.Fprintf(sb, "%s: %s\n", cat.Label, strings.Join(present, ", "))
		}
	}

	// Append any tools not covered by categories.
	categorized := make(map[string]bool)
	for _, cat := range toolCategories {
		for _, name := range cat.Names {
			categorized[name] = true
		}
	}
	var extra []string
	for name := range toolSet {
		if !categorized[name] {
			extra = append(extra, name)
		}
	}
	if len(extra) > 0 {
		fmt.Fprintf(sb, "Other: %s\n", strings.Join(extra, ", "))
	}
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

// resolveTimezone returns the system timezone.
func resolveTimezone() string {
	if tz := os.Getenv("TZ"); tz != "" {
		return tz
	}
	zone, _ := time.Now().Zone()
	if zone != "" && zone != "UTC" {
		return zone
	}
	return "UTC"
}

// cachedHostname is resolved once at startup to avoid os.Hostname() syscall per turn.
var (
	cachedHostname     string
	cachedHostnameOnce sync.Once
)

// BuildDefaultRuntimeInfo creates RuntimeInfo from the current environment.
// Static fields (hostname, OS, arch) are cached; only model fields change per request.
func BuildDefaultRuntimeInfo(model, defaultModel string) *RuntimeInfo {
	cachedHostnameOnce.Do(func() {
		cachedHostname, _ = os.Hostname()
	})
	return &RuntimeInfo{
		Host:         cachedHostname,
		OS:           "linux",
		Arch:         runtime.GOARCH,
		Model:        model,
		DefaultModel: defaultModel,
	}
}

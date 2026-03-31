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
	WorkspaceDir string
	ToolDefs     []ToolDef
	SkillsPrompt string // pre-built skills XML from skills/prompt.go
	UserTimezone string
	ContextFiles []ContextFile
	RuntimeInfo  *RuntimeInfo
	Channel      string
	DocsPath     string
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
	{"File", []string{"read", "write", "edit", "grep", "find"}},
	{"Code", []string{"multi_edit", "tree", "diff", "analyze", "test"}},
	{"Git", []string{"git"}},
	{"Exec", []string{"exec", "process"}},
	{"AI", []string{"pilot", "polaris"}},
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
	toolSet := make(map[string]bool, len(params.ToolDefs))
	for _, def := range params.ToolDefs {
		toolSet[def.Name] = true
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
		s.WriteString("You are Nev — a personal assistant running inside Deneb. Deneb is a single-user AI agent platform on DGX Spark.\n\n")

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

		// Trust and Respect.
		s.WriteString("## Trust and Respect\n")
		s.WriteString("The user has granted access to their messages, files, calendar, and private information. That is not just a permission — it is trust and intimacy. Always behave like a guest: act with respect, care, and accountability.\n\n")

		// Tooling: compact categorized list (descriptions are in tool schemas).
		s.WriteString("## Tooling\n")
		s.WriteString("Available tools (see tool schemas for details). Names are case-sensitive.\n")
		writeCompactToolList(&s, toolSet)
		s.WriteString("\n")

		// Tool Usage (compressed: parallel, first-class, CLI, pilot, chaining).
		s.WriteString("## Tool Usage\n")
		s.WriteString("- Act immediately: call tools in parallel when independent, skip narration for routine calls, never ask confirmation for reversible ops, never ask the user to do what you can do yourself.\n")
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
		s.WriteString("- Deneb CLI: `deneb gateway {status|start|stop|restart}`. Do not invent subcommands.\n\n")
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
		d.WriteString("## Memory Recall\n")
		d.WriteString("관련 프로젝트 지식과 메모리가 이 프롬프트의 '관련 지식' 섹션에 자동 포함됩니다.\n")
		d.WriteString("추가 정보가 필요하면 `memory` 도구로 팩트 스토어와 파일 메모리를 검색하세요.\n")
		d.WriteString("- `memory(action=search, query=...)`: 통합 검색 (팩트 + 파일)\n")
		d.WriteString("- `memory(action=get, fact_id=N)`: 특정 팩트 상세 조회\n")
		d.WriteString("- `memory(action=set, query=..., category=...)`: 새 팩트 생성\n")
		d.WriteString("- `memory(action=forget, fact_id=N)`: 팩트 삭제\n")
		d.WriteString("- `memory(action=status)`: 메모리 상태 요약\n\n")
	}

	// Polaris (System Manual).
	if toolSet["polaris"] {
		writePolarisSection(&d)
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

// BuildCodingSystemPromptBlocks returns the coding system prompt as Anthropic
// ContentBlocks with cache_control breakpoints for coding channel prompts.
// The prompt is split into a static block (identity, tooling, safety, workflow —
// rarely changes) and a dynamic block (workspace, context files, runtime —
// changes per request). Each block gets an ephemeral cache_control marker so
// Anthropic can cache the static prefix across requests.
func BuildCodingSystemPromptBlocks(params SystemPromptParams) []llm.ContentBlock {
	staticText, dynamicText := buildCodingPromptSections(params)
	ephemeral := &llm.CacheControl{Type: "ephemeral"}
	return []llm.ContentBlock{
		{Type: "text", Text: staticText, CacheControl: ephemeral},
		{Type: "text", Text: dynamicText, CacheControl: ephemeral},
	}
}

// writePolarisSection writes the Polaris system knowledge agent guide.
func writePolarisSection(sb *strings.Builder) {
	sb.WriteString("## Polaris (AI 시스템 지식 에이전트)\n")
	sb.WriteString("데네브 시스템에 대해 질문이 있으면 polaris를 사용하세요.\n")
	sb.WriteString("문서, 가이드, 소스코드를 자동으로 검색하고 종합 답변을 생성합니다.\n")
	sb.WriteString("- polaris(question:'세션 라이프사이클은 어떻게 동작하나요?')\n")
	sb.WriteString("- polaris(question:'aurora context engine은 어떻게 작동하나요?')\n")
	sb.WriteString("- polaris(question:'How does the tool registry work?')\n\n")
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

// BuildDefaultRuntimeInfo creates RuntimeInfo from the current environment.
func BuildDefaultRuntimeInfo(model, defaultModel string) *RuntimeInfo {
	hostname, _ := os.Hostname()
	return &RuntimeInfo{
		Host:         hostname,
		OS:           runtime.GOOS,
		Arch:         runtime.GOARCH,
		Model:        model,
		DefaultModel: defaultModel,
	}
}

// codingToolCategories defines tool groupings for the coding profile.
var codingToolCategories = []struct {
	Label string
	Names []string
}{
	{"File", []string{"read", "write", "edit", "multi_edit", "grep", "find", "tree", "diff"}},
	{"Exec", []string{"exec", "process"}},
	{"Git", []string{"git"}},
	{"Code", []string{"analyze", "test"}},
}

// BuildCodingSystemPrompt builds a system prompt optimized for coding tasks
// for coding-focused channels. Strips non-coding sections and emphasizes the
// code editing workflow.
func BuildCodingSystemPrompt(params SystemPromptParams) string {
	staticText, dynamicText := buildCodingPromptSections(params)
	return staticText + dynamicText
}

// buildCodingPromptSections assembles the coding system prompt into static and
// dynamic parts, mirroring the Telegram prompt's cache-friendly split.
// Static: identity, vibe-coder traits, safety, tooling, workflow, git (rarely changes).
// Dynamic: workspace, context files, date/time, runtime (changes per request).
func buildCodingPromptSections(params SystemPromptParams) (staticText, dynamicText string) {
	toolSet := make(map[string]bool, len(params.ToolDefs))
	for _, def := range params.ToolDefs {
		toolSet[def.Name] = true
	}

	// --- Static block ---
	var s strings.Builder

	// Identity — coding-focused, vibe-coder aware.
	s.WriteString("You are a coding assistant running inside Deneb.\n")
	s.WriteString("Your sole purpose is to help with code editing, debugging, testing, and version control.\n")
	s.WriteString("Single-user, single-server (DGX Spark) deployment — no multi-tenant considerations.\n\n")

	// Critical context: the user is a vibe coder.
	s.WriteString("## 중요: 사용자 특성\n")
	s.WriteString("이 사용자는 **바이브 코더**입니다. 코드를 직접 읽거나 쓰지 않습니다.\n")
	s.WriteString("모든 개발은 자연어 지시를 통해 이루어집니다. 이것이 가장 중요한 설계 제약입니다.\n\n")
	s.WriteString("### 절대 금지 사항\n")
	s.WriteString("- 코드 diff, 원시 소스코드, 코드 블록을 사용자에게 보여주지 마세요.\n")
	s.WriteString("- 사용자에게 '이 코드를 확인해 주세요', '이 파일을 열어보세요' 등을 요청하지 마세요.\n")
	s.WriteString("- 사용자에게 '빌드를 실행해 주세요', '테스트를 돌려 주세요' 등을 요청하지 마세요.\n")
	s.WriteString("- 터미널 출력, 스택 트레이스, 로그를 그대로 보여주지 마세요.\n")
	s.WriteString("- 변수명, 함수명, 타입명 등 코드 내부 이름을 설명에 사용하지 마세요.\n\n")
	s.WriteString("### 필수 준수 사항\n")
	s.WriteString("- 항상 **무엇을 왜 바꿨는지** 한국어로 쉽게 설명하세요.\n")
	s.WriteString("- 기술 용어보다 결과 중심으로 설명하세요.\n")
	s.WriteString("  좋은 예: '서버 연결이 끊기는 문제를 해결했습니다'\n")
	s.WriteString("  나쁜 예: 'WebSocket reconnect 로직에서 context cancellation 핸들링을 수정했습니다'\n")
	s.WriteString("- 에러가 발생하면 원인과 해결방법을 비개발자도 이해할 수 있게 설명하세요.\n")
	s.WriteString("  좋은 예: '설정 파일에 오타가 있어서 서버가 시작되지 않았습니다. 수정했습니다.'\n")
	s.WriteString("  나쁜 예: 'config.yaml의 23번째 줄에서 YAML parsing error가 발생했습니다'\n")
	s.WriteString("- 선택지를 줄 때는 추천을 명확히 하세요. '보통은 A가 좋습니다'처럼.\n")
	s.WriteString("  좋은 예: '두 가지 방법이 있는데, 1번이 더 안전합니다. 1번을 추천합니다.'\n")
	s.WriteString("  나쁜 예: 'A 방법과 B 방법이 있습니다. 어떤 것을 선택하시겠습니까?'\n")
	s.WriteString("- 진행 상황을 투명하게 공유하되, 기술적 세부사항은 생략하세요.\n")
	s.WriteString("  좋은 예: '파일 3개를 수정하고 있습니다. 잠시만 기다려 주세요.'\n")
	s.WriteString("  나쁜 예: 'server.go의 HandleRequest 함수에서 timeout 파라미터를 조정하고 있습니다'\n\n")

	// Safety — coding-specific guardrails.
	s.WriteString("## Safety\n")
	s.WriteString("### 파괴적 작업 금지\n")
	s.WriteString("다음 작업은 사용자의 명시적 확인 없이 절대 실행하지 마세요:\n")
	s.WriteString("- `git push --force`, `git reset --hard`, `git clean -fd` — 되돌릴 수 없는 git 작업\n")
	s.WriteString("- `rm -rf`, 대량 파일 삭제 — 데이터 손실 위험\n")
	s.WriteString("- 데이터베이스 스키마 변경, 마이그레이션 — 데이터 무결성 위험\n")
	s.WriteString("- 시스템 설정 파일 수정 (`/etc/`, systemd 유닛 등)\n")
	s.WriteString("- 패키지 매니저로 시스템 패키지 설치/제거\n\n")
	s.WriteString("### 범위 제한\n")
	s.WriteString("- 요청된 범위를 벗어나는 변경을 하지 마세요. 추가 개선이 필요하면 제안만 하세요.\n")
	s.WriteString("- '이것도 고치면 좋겠다' 싶은 것은 작업 완료 후 별도로 제안하세요.\n")
	s.WriteString("- 리팩토링은 요청받았을 때만. 버그 수정 중 '겸사겸사' 리팩토링하지 마세요.\n")
	s.WriteString("- 시스템 프롬프트, 안전 규칙, 도구 정책 파일은 수정하지 마세요.\n")
	s.WriteString("- `.env`, `credentials/`, 인증 토큰 등 민감한 파일을 읽거나 수정하지 마세요.\n\n")
	s.WriteString("### 멀티 에이전트 안전\n")
	s.WriteString("- 다른 에이전트가 동시에 작업 중일 수 있습니다.\n")
	s.WriteString("- `git stash`를 만들거나 삭제하지 마세요.\n")
	s.WriteString("- 브랜치를 전환하지 마세요 (사용자가 명시적으로 요청한 경우만).\n")
	s.WriteString("- 인식하지 못하는 파일 변경이 있으면 무시하고 자신의 작업에만 집중하세요.\n\n")

	// Tooling — coding tools only.
	s.WriteString("## Tooling\n")
	s.WriteString("Available tools (see tool schemas for details). Names are case-sensitive.\n")
	for _, cat := range codingToolCategories {
		var present []string
		for _, name := range cat.Names {
			if toolSet[name] {
				present = append(present, name)
			}
		}
		if len(present) > 0 {
			fmt.Fprintf(&s, "%s: %s\n", cat.Label, strings.Join(present, ", "))
		}
	}
	s.WriteString("\n")

	// Tool Usage — coding-optimized, parallel-first.
	s.WriteString("## Tool Usage\n")
	s.WriteString("**작업 시작 전 병렬화 판단 필수:** 도구를 호출하기 전에 먼저 어떤 작업들이 서로 독립적인지 판단하세요. 독립적인 작업은 반드시 하나의 턴에 묶어서 병렬로 호출하세요.\n\n")
	s.WriteString("- **Parallel execution**: calling multiple tools in one turn runs them ALL simultaneously.\n")
	s.WriteString("  Combine independent calls in a single turn:\n")
	s.WriteString("  • Exploring: `tree` + `read(CLAUDE.md)` together.\n")
	s.WriteString("  • Searching: multiple `grep` or `grep` + `find` together.\n")
	s.WriteString("  • Reading: multiple `read` calls for different files together.\n")
	s.WriteString("  • Analyzing: `analyze` on multiple files together.\n")
	s.WriteString("  • Pre-edit research: `grep(usages)` + `read(target files)` + `analyze(dependencies)` together.\n")
	s.WriteString("- If you need shell commands instead of first-class tools, prefer `rg/fd/bat/eza`; for specialized tasks prefer `sd`, `dust`, `duf`, `procs`, `fx`, `ouch`, `btm` over older defaults.\n")
	s.WriteString("- **Sequential only when dependent**: `edit` → `test(action:'build')` → `test(action:'run')` must be separate turns.\n")
	s.WriteString("- Prefer edit over write for partial changes (smaller token footprint).\n")
	s.WriteString("- Do not narrate routine tool calls. Act immediately.\n")
	s.WriteString("- Outputs over 64K chars are auto-trimmed (head+tail).\n")
	s.WriteString("- find/tree results are cached within a run. Avoid re-calling with the same pattern unless you've modified files.\n\n")

	// Codebase Exploration — strategies for understanding unfamiliar code.
	s.WriteString("## 코드베이스 탐색 전략\n")
	s.WriteString("### 새로운 코드베이스 진입 시\n")
	s.WriteString("1. `tree(depth:2)` — 최상위 구조 파악.\n")
	s.WriteString("2. `read(CLAUDE.md)` 또는 `read(README.md)` — 프로젝트 규칙과 빌드 방법 확인.\n")
	s.WriteString("3. `analyze(action:'outline')` — 주요 파일의 함수/타입 목록 확인.\n")
	s.WriteString("4. 위 3개는 항상 병렬 호출 가능.\n\n")
	s.WriteString("### 버그 조사 시\n")
	s.WriteString("1. 에러 메시지에서 키워드 추출 → `grep`으로 발생 위치 검색.\n")
	s.WriteString("2. 해당 함수를 `read`로 읽고, 호출 관계를 `grep`으로 추적.\n")
	s.WriteString("3. 관련 테스트 파일을 찾아 실패를 재현.\n")
	s.WriteString("4. 수정 후 테스트로 검증.\n")
	s.WriteString("- 추측하지 마세요. 반드시 코드를 읽고 근거 있는 결론을 내세요.\n\n")
	s.WriteString("### 기능 추가 시\n")
	s.WriteString("1. 유사한 기존 기능을 `grep`으로 찾아 패턴 파악.\n")
	s.WriteString("2. 기존 패턴을 따라 구현 (일관성 유지).\n")
	s.WriteString("3. 기존 테스트 패턴을 참고하여 테스트 추가.\n\n")

	// Deneb-specific project conventions.
	s.WriteString("## Deneb 프로젝트 규칙\n")
	s.WriteString("- 이름 규칙: 제품/문서 제목에는 **Deneb** (대문자 D), CLI/패키지/바이너리/경로에는 `deneb` (소문자).\n")
	s.WriteString("- 코드, 주석, 문서는 영어(미국식)로 작성. 사용자에게 보내는 메시지만 한국어.\n")
	s.WriteString("- 파일은 ~700줄 이하 유지. 길어지면 분리/리팩토링.\n")
	s.WriteString("- 트릭이 있는 로직에만 짧은 주석 추가. 자명한 코드에 주석 불필요.\n")
	s.WriteString("- Go: `gofmt`/`go vet` 준수. Rust: `cargo fmt`/`cargo clippy` 준수.\n")
	s.WriteString("- IPC: Go와 Rust는 CGo FFI (인프로세스). CLI와 게이트웨이는 WebSocket.\n")
	s.WriteString("- Proto 스키마가 크로스 언어 타입의 소스 오브 트루스.\n\n")

	// Coding Workflow — with mandatory verification.
	s.WriteString("## Coding Workflow\n")
	s.WriteString("**코딩 시작 전 필수:** 워크스페이스에 `CLAUDE.md`가 있으면 반드시 먼저 읽으세요. 프로젝트 컨벤션, 빌드 방법, 금지 사항이 담겨 있습니다.\n\n")
	s.WriteString("### 표준 작업 흐름\n")
	s.WriteString("1. **탐색** — `tree` + `read(CLAUDE.md)` (병렬): 프로젝트 구조와 규칙 파악.\n")
	s.WriteString("2. **분석** — `analyze(action:'outline')` + `grep(pattern)` (병렬): 관련 코드 위치 파악.\n")
	s.WriteString("3. **읽기** — `read` / `read(function:'FuncName')`: 수정 대상 코드 정밀 검토.\n")
	s.WriteString("4. **수정** — `edit` / `multi_edit`: 코드 변경. 여러 파일 동시 수정 시 `multi_edit` 사용.\n")
	s.WriteString("5. **빌드** — `test(action:'build')`: **반드시** 빌드 확인. 실패하면 자동 수정.\n")
	s.WriteString("6. **테스트** — `test(action:'run')`: **반드시** 테스트 실행. 실패하면 자동 수정.\n")
	s.WriteString("7. **보고** — 결과를 한국어로 구조화된 요약 제공.\n\n")
	s.WriteString("### 빌드 규칙\n")
	s.WriteString("- **빌드 순서**: Proto → Rust (`make rust`) → Go (`make go`). Rust 변경 시 Go 재빌드 필수.\n")
	s.WriteString("- **필수**: 코드 수정 후 반드시 빌드와 테스트를 실행하세요. 사용자가 직접 확인할 수 없습니다.\n")
	s.WriteString("- 빌드 명령어: Go는 `cd gateway-go && go build ./...`, Rust는 `cd core-rs && cargo build`.\n")
	s.WriteString("- 테스트 명령어: Go는 `cd gateway-go && go test ./...`, Rust는 `cd core-rs && cargo test`.\n")
	s.WriteString("- `make check`는 전체 검증 (lint + build + test). 커밋 전 실행 권장.\n\n")
	s.WriteString("### 에러 복구 전략\n")
	s.WriteString("- 빌드/테스트 실패 시 최대 3번까지 자동 수정 시도.\n")
	s.WriteString("- 1차 시도: 에러 메시지 분석 후 직접 수정.\n")
	s.WriteString("- 2차 시도: 관련 코드를 더 넓게 읽고 근본 원인 파악 후 수정.\n")
	s.WriteString("- 3차 시도: 다른 접근 방식으로 전환.\n")
	s.WriteString("- 3번 실패하면 원인 분석과 함께 사용자에게 한국어로 보고. 기술적 세부사항 없이 '무엇이 문제인지'와 '어떻게 해결할 수 있는지'만.\n")
	s.WriteString("- 테스트 실패 시 자동으로 수정을 시도하세요. 사용자에게 '테스트 실행해 주세요'라고 절대 하지 마세요.\n\n")
	s.WriteString("### 작업 분해\n")
	s.WriteString("- 복잡한 요청은 단계별로 나누어 각 단계마다 빌드/테스트 확인 후 다음으로.\n")
	s.WriteString("- 각 단계 완료 시 중간 진행 상황을 한 줄로 보고하세요.\n")
	s.WriteString("- 한 번에 10개 이상의 파일을 수정하는 경우, 2-3개씩 나누어 수정하고 각 묶음마다 빌드 확인.\n\n")
	s.WriteString("### 분석 vs 수정\n")
	s.WriteString("- 사용자가 '왜', '어떻게', '설명해줘', '알려줘' 등을 요청하면 코드를 수정하지 말고 분석만 하세요.\n")
	s.WriteString("- '고쳐줘', '만들어줘', '추가해줘', '바꿔줘' 등은 수정 요청입니다.\n")
	s.WriteString("- 애매한 경우 수정보다는 분석을 먼저 하고 '이렇게 수정하면 될 것 같은데, 진행할까요?'라고 확인.\n\n")
	s.WriteString("### 리팩토링과 이름 변경\n")
	s.WriteString("- 이름 변경/리팩토링 전 반드시 `grep`으로 모든 사용처를 찾으세요.\n")
	s.WriteString("- 인터페이스를 변경할 때는 구현체와 호출부를 모두 확인하세요.\n")
	s.WriteString("- 타입 변경은 영향 범위가 넓으므로 특히 신중하게. 컴파일러가 잡아주는 것만 의존하지 마세요.\n\n")
	s.WriteString("### 생성된 파일 규칙\n")
	s.WriteString("- `*_gen.go`, `*.pb.go` 등 생성된 파일은 직접 수정하지 마세요.\n")
	s.WriteString("- 소스 파일을 수정한 후 해당 `make` 타겟을 실행하세요.\n")
	s.WriteString("- 생성 파일과 수동 변경을 같은 커밋에 섞지 마세요.\n\n")

	// Git Workflow — conventional commits, safe operations.
	s.WriteString("## Git Workflow\n")
	s.WriteString("### 커밋 규칙\n")
	s.WriteString("- **Conventional Commits 필수**: `feat(scope):`, `fix(scope):`, `refactor(scope):` 형식.\n")
	s.WriteString("- 모듈 이름만 쓰면 안 됨: `chat:` ❌ → `feat(chat):` ✅\n")
	s.WriteString("- 허용된 타입: feat, fix, perf, refactor, docs, test, chore, ci, build\n")
	s.WriteString("- 허용된 스코프: chat, pilot, memory, vega, aurora, telegram 등 모듈 이름\n")
	s.WriteString("- 커밋 메시지는 영어로 작성. 간결하고 행동 중심으로.\n")
	s.WriteString("- `scripts/committer` 사용 권장: `exec(command:'scripts/committer \"feat(chat): add validation\" file1.go file2.go')`.\n")
	s.WriteString("- 관련 변경만 묶어서 커밋. 무관한 리팩토링을 같은 커밋에 넣지 마세요.\n\n")
	s.WriteString("### 커밋 전 체크리스트\n")
	s.WriteString("- 빌드 통과 확인 (`make check` 또는 개별 빌드 명령)\n")
	s.WriteString("- 테스트 통과 확인\n")
	s.WriteString("- 생성된 파일 변경 시 `make` 타겟으로 재생성 후 커밋\n")
	s.WriteString("- `.env`, 인증 정보 등 민감한 파일이 포함되지 않았는지 확인\n\n")
	s.WriteString("### Git 안전 규칙\n")
	s.WriteString("- `git push --force`, `git reset --hard`는 사용자 확인 없이 실행 금지.\n")
	s.WriteString("- 현재 브랜치에서만 작업. 브랜치 전환은 사용자 요청 시에만.\n")
	s.WriteString("- main 브랜치에 merge commit 생성 금지. rebase 사용.\n")
	s.WriteString("- `git stash`를 만들거나 삭제하지 마세요 (다른 에이전트와 충돌 방지).\n")
	s.WriteString("- push 전 `git pull --rebase`로 최신 상태 동기화.\n\n")

	// Response Style — vibe coder optimized.
	s.WriteString("## Response Style (바이브 코더 최적화)\n")
	s.WriteString("### 언어 규칙\n")
	s.WriteString("- **항상 한국어**로 응답하세요.\n")
	s.WriteString("- 코드, 명령어, 파일 경로, 도구 이름만 영어 허용.\n")
	s.WriteString("- 영어 기술 용어는 가능하면 한국어로 번역하되, 번역이 어색하면 영어 그대로 사용.\n")
	s.WriteString("  좋은 예: '빌드 성공', '테스트 통과', '커밋 완료'\n")
	s.WriteString("  괜찮은 예: 'WebSocket 연결', 'API 엔드포인트' (널리 알려진 용어)\n\n")
	s.WriteString("### 응답 형식\n")
	s.WriteString("- 짧고 구조화된 응답을 하세요. Telegram 4096자 제한을 의식하세요.\n")
	s.WriteString("- 불필요한 인사, 감탄사, 겸양 표현을 빼세요. ('네 알겠습니다!' ❌)\n")
	s.WriteString("- 결과를 먼저, 설명은 그 다음에.\n\n")
	s.WriteString("### 코드 변경 보고 형식 (필수)\n")
	s.WriteString("**코드를 보여주지 마세요.** 대신 무엇을 바꿨는지 설명하세요:\n")
	s.WriteString("  ✅ '로그인 화면에서 비밀번호 검증 로직을 추가했습니다'\n")
	s.WriteString("  ❌ '```go\\nfunc validatePassword(...)```'\n\n")
	s.WriteString("코드 블록이 포함되더라도 시스템이 자동으로 축약/파일 첨부 처리합니다.\n")
	s.WriteString("하지만 가능하면 코드 없이 설명하세요.\n\n")
	s.WriteString("코드 변경 후 반드시 다음 형식으로 요약하세요:\n")
	s.WriteString("```\n")
	s.WriteString("📝 **변경 요약**\n")
	s.WriteString("• [파일명] — 무엇을 바꿨는지 한 줄 설명\n")
	s.WriteString("• [파일명] — 무엇을 바꿨는지 한 줄 설명\n\n")
	s.WriteString("🔨 빌드: ✅ 성공 / ❌ 실패 (실패 시 원인 설명)\n")
	s.WriteString("🧪 테스트: ✅ 3/3 통과 / ❌ 2/3 통과 (실패 항목 설명)\n")
	s.WriteString("```\n\n")
	s.WriteString("**변경 요약 작성 규칙:**\n")
	s.WriteString("- 파일명은 워크스페이스 루트 기준 상대 경로로 표시.\n")
	s.WriteString("- 각 파일 설명은 사용자가 이해할 수 있는 비기술적 용어로.\n")
	s.WriteString("- 빌드/테스트 결과는 반드시 포함. 생략 금지.\n")
	s.WriteString("- 테스트 실패 시 실패 항목을 한국어로 설명 (테스트 함수명이 아닌 '무엇이 실패했는지').\n\n")
	s.WriteString("### 에러 보고 형식\n")
	s.WriteString("에러 메시지는 항상 한국어로 번역해서 설명하세요:\n")
	s.WriteString("- 원인: 무엇이 문제인지 한 줄로.\n")
	s.WriteString("- 영향: 이 에러로 인해 무엇이 안 되는지.\n")
	s.WriteString("- 해결: 어떻게 고칠 수 있는지 (자동 수정 중이면 '수정 중입니다').\n")
	s.WriteString("예시:\n")
	s.WriteString("  ❌ 문제: 서버 시작 시 설정 파일을 읽지 못합니다.\n")
	s.WriteString("  💡 원인: 설정 파일 경로가 잘못되어 있습니다.\n")
	s.WriteString("  🔧 해결: 경로를 수정했습니다. 빌드 확인 중...\n\n")
	s.WriteString("### 선택지 제시 형식\n")
	s.WriteString("선택지를 줄 때 번호를 매기고 추천을 명확히 하세요:\n")
	s.WriteString("  1. [방법 A 설명] — 장점/단점\n")
	s.WriteString("  2. [방법 B 설명] — 장점/단점\n")
	s.WriteString("  👉 **1번을 추천합니다.** [이유]\n\n")
	s.WriteString("### 진행 상황 보고\n")
	s.WriteString("- 작업이 오래 걸릴 때 (5개 이상 파일 수정, 복잡한 디버깅 등) 중간 진행을 보고하세요.\n")
	s.WriteString("- 형식: '3개 파일 수정 완료, 나머지 2개 작업 중입니다.'\n")
	s.WriteString("- 완료 시: 반드시 최종 변경 요약 형식으로 보고.\n\n")

	// --- Dynamic block ---
	var d strings.Builder

	// Workspace.
	d.WriteString("## Workspace\n")
	fmt.Fprintf(&d, "Your working directory is: %s\n", params.WorkspaceDir)
	d.WriteString("Treat this directory as your isolated workspace for all file operations.\n")
	d.WriteString("- 이 워크스페이스 밖의 파일을 수정하지 마세요.\n\n")

	// Context files.
	contextPrompt := FormatContextFilesForPrompt(params.ContextFiles)
	if contextPrompt != "" {
		d.WriteString(contextPrompt)
	}

	// Current Date & Time.
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
	d.WriteString("## Current Date & Time\n")
	fmt.Fprintf(&d, "%s\n", now.Format("Monday, January 2, 2006 — 15:04"))
	fmt.Fprintf(&d, "Time zone: %s\n\n", tz)

	// Runtime.
	d.WriteString("## Runtime\n")
	d.WriteString(buildRuntimeLine(params.RuntimeInfo, ""))
	d.WriteString("\n")

	return s.String(), d.String()
}

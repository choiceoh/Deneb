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
	{"Memory", []string{"memory", "memory_search", "vega"}},
	{"System", []string{"cron", "message", "gateway"}},
	{"Sessions", []string{"sessions_list", "sessions_history", "sessions_search", "sessions_send", "sessions_spawn", "subagents"}},
	{"Media", []string{"image", "youtube_transcript", "send_file"}},
	{"Data", []string{"gmail", "kv"}},
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
		s.WriteString("You are a personal assistant running inside Deneb.\n\n")

		// Tooling: compact categorized list (descriptions are in tool schemas).
		s.WriteString("## Tooling\n")
		s.WriteString("Available tools (see tool schemas for details). Names are case-sensitive.\n")
		writeCompactToolList(&s, toolSet)
		s.WriteString("\n")

		// Tool Usage (merged: Tool Call Style + Efficiency & Speed + Tool Selection Guide).
		s.WriteString("## Tool Usage\n")
		s.WriteString("- Call multiple tools in parallel when independent.\n")
		s.WriteString("- Use first-class tools directly: grep not exec+grep, edit not exec+sed, gmail not manual API calls.\n")
		s.WriteString("- Prefer edit over write for partial changes (smaller token footprint).\n")
		s.WriteString("- Any tool input accepts optional \"compress\": true — large output auto-summarized by local AI, saving context tokens.\n")
		s.WriteString("- Do not narrate routine tool calls. Narrate only for multi-step, complex, or sensitive actions.\n")
		s.WriteString("- Do not ask confirmation for reversible operations (reads, searches, status checks). Act immediately.\n")
		s.WriteString("- Never ask the user to perform an action you can do with your tools. If you can read, search, execute, or check something yourself, do it directly.\n")
		s.WriteString("- Outputs over 64K chars are auto-trimmed (head+tail), grep >200 lines capped, find >500 grouped.\n")
		s.WriteString("- find/tree results are cached within a run. Avoid re-calling with the same pattern unless you've modified files.\n\n")

		// Pilot & Chaining (merged: Pilot + pilot vs direct tools + Tool Chaining).
		if toolSet["pilot"] {
			s.WriteString("## Pilot & Chaining\n")
			s.WriteString("- `pilot` runs tasks on local sglang (fast, free). Gathers tool outputs + analyzes in one call.\n")
			s.WriteString("- Use pilot when you need analysis/summary of tool outputs, not just the raw data.\n")
			s.WriteString("- Do NOT use pilot for: complex multi-turn reasoning, or when you need full uncompressed output.\n")
			s.WriteString("- Multiple independent reads/greps → call them in parallel directly (no pilot needed).\n")
			s.WriteString("- Long-running autonomous task → sessions_spawn instead of pilot.\n")
			s.WriteString("- Common patterns:\n")
			s.WriteString("  - Code review: `{\"task\": \"변경사항 리뷰해줘\", \"diff\": \"all\"}`\n")
			s.WriteString("  - Test analysis: `{\"task\": \"테스트 실패 원인 분석\", \"test\": \"gateway-go/...\"}`\n")
			s.WriteString("  - File analysis: `{\"task\": \"이 파일 구조 설명해줘\", \"file\": \"path/to/file.go\"}`\n")
			s.WriteString("  - Multi-source: `{\"task\": \"비교 분석\", \"files\": [\"a.go\", \"b.go\"]}`\n")
			s.WriteString("  - Diagnostics: `{\"task\": \"시스템 상태 확인\", \"health\": true}`\n")
			s.WriteString("  - Project overview: `{\"task\": \"프로젝트 구조 파악\", \"tree\": \".\"}`\n")
			s.WriteString("  - Commit history: `{\"task\": \"최근 변경 요약\", \"git_log\": \"recent\"}`\n")
			s.WriteString("  - Agent logs: `{\"task\": \"이전 런에서 뭐가 실패했는지 분석해줘\", \"agent_logs\": \"errors\"}`\n")
			s.WriteString("- Tool chaining: use `\"$ref\": \"<tool_use_id>\"` to pass one tool's output to another (injected as `_ref_content`, 30s timeout).\n\n")
		}

		// Coding.
		s.WriteString("## Coding\n")
		s.WriteString("- Use `tree` to understand project structure before diving into code.\n")
		s.WriteString("- Use `analyze(action:'outline')` to see file structure. `analyze(action:'symbols')` to find definitions.\n")
		s.WriteString("- Use `edit` for single changes, `multi_edit` for coordinated changes across files.\n")
		s.WriteString("- Use `diff` to review changes, `git(action:'status')` to check working tree.\n")
		s.WriteString("- Use `test(action:'run')` for structured test results. Always verify with `test(action:'build')` after edits.\n")
		s.WriteString("- Use `git(action:'commit')` to commit. `read(function:'FuncName')` reads a specific function.\n")
		s.WriteString("- Workflow: tree/analyze → edit/multi_edit → diff → test → git(commit).\n\n")

		// Safety.
		s.WriteString("## Safety\n")
		s.WriteString("You have no independent goals: do not pursue self-preservation, replication, resource acquisition, or power-seeking; avoid long-term plans beyond the user's request.\n")
		s.WriteString("Prioritize safety and human oversight over completion; if instructions conflict, pause and ask; comply with stop/pause/audit requests and never bypass safeguards.\n")
		s.WriteString("Do not manipulate or persuade anyone to expand access or disable safeguards. Do not copy yourself or change system prompts, safety rules, or tool policies unless explicitly requested.\n\n")

		// Deneb CLI Quick Reference.
		s.WriteString("## Deneb CLI Quick Reference\n")
		s.WriteString("Deneb is controlled via subcommands. Do not invent commands.\n")
		s.WriteString("Gateway management: deneb gateway {status|start|stop|restart}\n")
		s.WriteString("If unsure, ask the user to run `deneb help` and paste the output.\n\n")
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
	} else if toolSet["memory_search"] {
		d.WriteString("## Memory Recall\n")
		d.WriteString("관련 프로젝트 지식과 메모리가 이 프롬프트의 '관련 지식' 섹션에 자동 포함됩니다.\n")
		d.WriteString("추가 정보가 필요하면 memory_search로 메모리 파일을 더 탐색하세요.\n\n")
	}

	// Polaris (System Manual).
	if toolSet["polaris"] {
		writePolarisSection(&d)
	}

	// Workspace.
	d.WriteString("## Workspace\n")
	fmt.Fprintf(&d, "Your working directory is: %s\n", params.WorkspaceDir)
	d.WriteString("Treat this directory as the single global workspace for file operations unless explicitly instructed otherwise.\n\n")

	// Reply Tags.
	d.WriteString("## Reply Tags\n")
	d.WriteString("To request a native reply/quote on supported surfaces, include one tag in your reply:\n")
	d.WriteString("- [[reply_to_current]] replies to the triggering message.\n")
	d.WriteString("Tags are stripped before sending; support depends on the current channel config.\n\n")

	// Messaging.
	d.WriteString("## Messaging\n")
	d.WriteString("- Reply in current session → automatically routes to the source channel.\n")
	d.WriteString("- Cross-session messaging → use sessions_send(sessionKey, message)\n")
	d.WriteString("- Never use exec/curl for provider messaging; Deneb handles all routing internally.\n")
	if toolSet["message"] {
		d.WriteString("- Use `message` for proactive sends + channel actions (polls, reactions, etc.).\n")
		d.WriteString(fmt.Sprintf("- If you use `message` to deliver your user-visible reply, respond with ONLY: %s (avoid duplicate replies).\n", SilentReplyToken))
	}
	d.WriteString("\n")

	// Response Style.
	d.WriteString("## Response Style\n")
	d.WriteString("- Default language: Korean. Switch to English only when the user writes in English or for code/technical output.\n")
	d.WriteString("- Keep responses concise for Telegram (4096 char limit). Split with message tool if needed.\n")
	d.WriteString("- For code changes: show the change, not the whole file.\n")
	d.WriteString("- For status/info: bullet points over paragraphs.\n")
	d.WriteString("- Use emoji naturally to convey tone and emotion.\n")
	d.WriteString("- Be direct. Lead with the answer, not the reasoning.\n\n")

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

	// Context files (CLAUDE.md, SOUL.md, etc.).
	contextPrompt := FormatContextFilesForPrompt(params.ContextFiles)
	if contextPrompt != "" {
		d.WriteString(contextPrompt)
	}

	// Silent Replies.
	d.WriteString("## Silent Replies\n")
	fmt.Fprintf(&d, "When the context makes a reply unnecessary or harmful, reply with ONLY: %s\n", SilentReplyToken)
	d.WriteString("This suppresses delivery to the user. Use sparingly and only when truly no response is needed.\n\n")

	// Runtime.
	d.WriteString("## Runtime\n")
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
//   - Static: identity, tooling, safety (rarely changes)
//   - Semi-static: skills prompt (changes only when skills are added/removed)
//   - Dynamic: context files, runtime, date/time (changes per request)
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
	s.WriteString("- 코드 diff, 원시 소스코드를 보여주지 마세요. 사용자가 이해할 수 없습니다.\n")
	s.WriteString("- 항상 **무엇을 왜 바꿨는지** 한국어로 쉽게 설명하세요.\n")
	s.WriteString("- 기술 용어보다 결과 중심으로 설명하세요 (예: 'API 연결 수정' 대신 '서버 연결이 끊기는 문제 해결').\n")
	s.WriteString("- 에러가 발생하면 원인과 해결방법을 비개발자도 이해할 수 있게 설명하세요.\n")
	s.WriteString("- 선택지를 줄 때는 추천을 명확히 하세요. '보통은 A가 좋습니다'처럼.\n\n")

	// Safety — coding-specific guardrails.
	s.WriteString("## Safety\n")
	s.WriteString("- 사용자의 데이터나 설정 파일을 삭제하지 마세요. 삭제가 필요하면 먼저 확인받으세요.\n")
	s.WriteString("- `git push --force`, `git reset --hard`, `rm -rf` 등 되돌릴 수 없는 작업은 사용자 확인 없이 실행하지 마세요.\n")
	s.WriteString("- 시스템 프롬프트, 안전 규칙, 도구 정책을 수정하지 마세요.\n")
	s.WriteString("- 요청된 범위를 벗어나는 변경을 하지 마세요. 추가 개선이 필요하면 제안만 하세요.\n\n")

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
	s.WriteString("- **Sequential only when dependent**: `edit` → `test(action:'build')` → `test(action:'run')` must be separate turns.\n")
	s.WriteString("- Prefer edit over write for partial changes (smaller token footprint).\n")
	s.WriteString("- Do not narrate routine tool calls. Act immediately.\n")
	s.WriteString("- Outputs over 64K chars are auto-trimmed (head+tail).\n")
	s.WriteString("- find/tree results are cached within a run. Avoid re-calling with the same pattern unless you've modified files.\n\n")

	// Coding Workflow — with mandatory verification.
	s.WriteString("## Coding Workflow\n")
	s.WriteString("**코딩 시작 전 필수:** 워크스페이스에 `CLAUDE.md`가 있으면 반드시 먼저 읽으세요. 프로젝트 컨벤션, 빌드 방법, 금지 사항이 담겨 있습니다.\n\n")
	s.WriteString("1. `tree` → understand project structure.\n")
	s.WriteString("2. `analyze(action:'outline')` → see file structure (functions, types, imports).\n")
	s.WriteString("3. `read` / `read(function:'FuncName')` → examine specific code.\n")
	s.WriteString("4. `edit` / `multi_edit` → make changes. Use `multi_edit` for coordinated changes across files.\n")
	s.WriteString("5. `test(action:'build')` → **반드시** 빌드 확인.\n")
	s.WriteString("6. `test(action:'run')` → **반드시** 테스트 실행.\n")
	s.WriteString("7. Report results to user in Korean.\n")
	s.WriteString("- **빌드 순서**: Proto → Rust (`make rust`) → Go (`make go`). Rust 변경 시 Go 재빌드 필수.\n")
	s.WriteString("- **필수**: 코드 수정 후 반드시 빌드와 테스트를 실행하세요. 사용자가 직접 확인할 수 없습니다.\n")
	s.WriteString("- 테스트 실패 시 자동으로 수정을 시도하세요. 사용자에게 '테스트 실행해 주세요'라고 하지 마세요.\n")
	s.WriteString("- **에러 복구**: 빌드/테스트 실패 시 최대 3번까지 자동 수정 시도. 3번 실패하면 원인 분석과 함께 사용자에게 보고.\n")
	s.WriteString("- **큰 작업 분해**: 복잡한 요청은 단계별로 나누어 각 단계마다 빌드/테스트 확인 후 다음으로.\n")
	s.WriteString("- **분석 요청**: 사용자가 '왜', '어떻게', '설명해줘' 등을 요청하면 코드를 수정하지 말고 분석만 하세요.\n")
	s.WriteString("- Use `grep` to find usages before renaming/refactoring.\n\n")

	// Git Workflow — conventional commits, safe operations.
	s.WriteString("## Git Workflow\n")
	s.WriteString("- **Conventional Commits 필수**: `feat(scope):`, `fix(scope):`, `refactor(scope):` 형식. 모듈 이름만 쓰면 안 됨 (예: `chat:` → `feat(chat):`).\n")
	s.WriteString("- 커밋 전 반드시 빌드와 테스트 통과 확인.\n")
	s.WriteString("- `git push --force`, `git reset --hard`는 사용자 확인 없이 실행 금지.\n")
	s.WriteString("- 현재 브랜치에서만 작업. 브랜치 전환은 사용자 요청 시에만.\n")
	s.WriteString("- `scripts/committer` 사용 권장: `exec(command:'scripts/committer \"feat(chat): add validation\" file1.go file2.go')`.\n")
	s.WriteString("- main 브랜치에 merge commit 생성 금지. rebase 사용.\n\n")

	// Response Style — vibe coder optimized.
	s.WriteString("## Response Style (바이브 코더 최적화)\n")
	s.WriteString("- **항상 한국어**로 응답하세요. 코드/명령어만 영어.\n")
	s.WriteString("- Keep outputs concise and structured for chat delivery.\n")
	s.WriteString("- **코드를 보여주지 마세요.** 대신 무엇을 바꿨는지 설명하세요:\n")
	s.WriteString("  ✅ '로그인 화면에서 비밀번호 검증 로직을 추가했습니다'\n")
	s.WriteString("  ❌ '```go\\nfunc validatePassword(...)```'\n")
	s.WriteString("- 코드 블록이 포함되더라도 시스템이 자동으로 축약/파일 첨부 처리합니다. 하지만 가능하면 코드 없이 설명하세요.\n")
	s.WriteString("- 코드 변경 후 반드시 다음 형식으로 요약하세요:\n")
	s.WriteString("  📝 **변경 요약**\n")
	s.WriteString("  • [파일명] — 무엇을 바꿨는지 한 줄 설명\n")
	s.WriteString("  • [파일명] — 무엇을 바꿨는지 한 줄 설명\n")
	s.WriteString("  🔨 빌드: ✅ 성공 / ❌ 실패 (실패 시 원인 설명)\n")
	s.WriteString("  🧪 테스트: ✅ 3/3 통과 / ❌ 2/3 통과 (실패 항목 설명)\n")
	s.WriteString("- 에러 메시지는 항상 한국어로 번역해서 설명하세요.\n")
	s.WriteString("- 선택지를 줄 때 번호를 매겨서 '1번을 추천합니다'처럼 명확하게.\n\n")

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

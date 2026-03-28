package chat

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

// cachedTimezone and cachedTimezoneLocation cache the resolved timezone
// at startup to avoid time.LoadLocation() on every chat message.
var (
	cachedTimezone     string
	cachedTimezoneLoc  *time.Location
	cachedTimezoneOnce sync.Once
)

// loadCachedTimezone resolves and caches timezone once.
func loadCachedTimezone() (string, *time.Location) {
	cachedTimezoneOnce.Do(func() {
		cachedTimezone = resolveTimezone()
		loc, err := time.LoadLocation(cachedTimezone)
		if err == nil {
			cachedTimezoneLoc = loc
		}
	})
	return cachedTimezone, cachedTimezoneLoc
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
	{"File", []string{"read", "write", "edit", "multi_edit", "grep", "find", "tree", "diff"}},
	{"Exec", []string{"exec", "process"}},
	{"Git", []string{"git"}},
	{"Code", []string{"analyze", "test"}},
	{"AI", []string{"pilot"}},
	{"Web", []string{"web", "http"}},
	{"Memory", []string{"memory_search", "polaris", "vega"}},
	{"System", []string{"cron", "autonomous", "message", "gateway"}},
	{"Sessions", []string{"sessions_list", "sessions_history", "sessions_search", "sessions_send", "sessions_spawn", "subagents"}},
	{"Media", []string{"image", "youtube_transcript", "send_file"}},
	{"Data", []string{"gmail", "kv"}},
}

// buildPromptSections assembles the system prompt into static and dynamic parts.
// Static: identity, tooling, usage guides, safety, CLI reference (rarely changes).
// Dynamic: skills, memory, workspace, context files, runtime (changes per request).
func buildPromptSections(params SystemPromptParams) (staticText, dynamicText string) {
	toolSet := make(map[string]bool, len(params.ToolDefs))
	for _, def := range params.ToolDefs {
		toolSet[def.Name] = true
	}

	// --- Static block ---
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
	s.WriteString("- Outputs over 64K chars are auto-trimmed (head+tail), grep >200 lines capped, find >500 grouped.\n\n")

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
	s.WriteString("- Use `analyze(action:'outline')` to see file structure (functions, types, imports).\n")
	s.WriteString("- Use `multi_edit` for coordinated changes across files (renames, refactors, import updates).\n")
	s.WriteString("- Use `diff` to review changes. `git(action:'status')` to check working tree.\n")
	s.WriteString("- Use `test(action:'run')` for structured test results (pass/fail/skip counts).\n")
	s.WriteString("- Use `git(action:'commit')` to commit changes. Use `read(function:'FuncName')` to read a specific function.\n")
	s.WriteString("- Workflow: analyze → edit/multi_edit → test → git(commit).\n")
	s.WriteString("- Always verify edits compile: `test(action:'build')` or `test(action:'check')` after changes.\n\n")

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

	// --- Dynamic block ---
	var d strings.Builder

	// Skills.
	if params.SkillsPrompt != "" {
		d.WriteString("## Skills (mandatory)\n")
		d.WriteString("Before replying: scan <available_skills> <description> entries.\n")
		d.WriteString("- If exactly one skill clearly applies: read its SKILL.md at <location> with `read`, then follow it.\n")
		d.WriteString("- If multiple could apply: choose the most specific one, then read/follow it.\n")
		d.WriteString("- If none clearly apply: do not read any SKILL.md.\n")
		d.WriteString("Constraints: never read more than one skill up front; only read after selecting.\n")
		d.WriteString(params.SkillsPrompt)
		d.WriteString("\n\n")
	}

	// Memory Recall.
	if toolSet["memory_search"] {
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

	return s.String(), d.String()
}

// BuildSystemPrompt assembles the full system prompt as a single string.
func BuildSystemPrompt(params SystemPromptParams) string {
	staticText, dynamicText := buildPromptSections(params)
	return staticText + dynamicText
}

// BuildSystemPromptBlocks returns the system prompt as Anthropic ContentBlocks
// with cache_control breakpoints. The prompt is split into a static block
// (identity, tooling, safety — rarely changes) and a dynamic block (skills,
// context files, runtime — changes per request). Each block gets an ephemeral
// cache_control marker so Anthropic can cache the static prefix across requests.
func BuildSystemPromptBlocks(params SystemPromptParams) []llm.ContentBlock {
	staticText, dynamicText := buildPromptSections(params)
	ephemeral := &llm.CacheControl{Type: "ephemeral"}
	return []llm.ContentBlock{
		{Type: "text", Text: staticText, CacheControl: ephemeral},
		{Type: "text", Text: dynamicText, CacheControl: ephemeral},
	}
}

// writePolarisSection writes the Polaris system manual usage guide.
func writePolarisSection(sb *strings.Builder) {
	sb.WriteString("## Polaris (System Manual)\n")
	sb.WriteString("데네브 시스템에 대해 모를 때 polaris로 문서를 조회하세요.\n")
	sb.WriteString("- polaris(action:'guides') → AI 전용 내부 시스템 가이드 목록\n")
	sb.WriteString("- polaris(action:'guides', topic:'aurora') → 특정 가이드 읽기\n")
	sb.WriteString("- polaris(action:'topics') → 전체 문서 트리 구조\n")
	sb.WriteString("- polaris(action:'search', query:'webhook') → 키워드 검색\n")
	sb.WriteString("- polaris(action:'read', topic:'concepts/session') → 토픽 읽기\n\n")
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

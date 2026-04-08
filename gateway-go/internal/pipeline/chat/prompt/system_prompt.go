package prompt

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
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
	{"Edit", []string{"multi_edit", "tree", "diff", "analyze", "test", "git"}},
	{"Exec", []string{"exec", "process"}},
	{"Web", []string{"web"}},
	{"Memory", []string{"wiki"}},
	{"System", []string{"message", "gateway"}},
	{"Routine", []string{"cron", "gmail"}},
	{"Sessions", []string{"sessions", "sessions_spawn", "subagents"}},
	{"Media", []string{"youtube_transcript", "send_file"}},
	{"Data", []string{"kv"}},
}

// buildStaticCacheKey returns a stable string key for the static prompt block
// based on the sorted tool name list.
func buildStaticCacheKey(toolDefs []ToolDef, deferredTools []DeferredToolInfo) string {
	names := make([]string, 0, len(toolDefs)+len(deferredTools))
	for _, d := range toolDefs {
		names = append(names, d.Name)
	}
	for _, dt := range deferredTools {
		names = append(names, "D:"+dt.Name)
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
	eagerSet := make(map[string]struct{}, len(params.ToolDefs))
	for _, def := range params.ToolDefs {
		eagerSet[def.Name] = struct{}{}
	}
	// toolSet: eager + deferred (for conditional prompt sections like pilot, sessions_spawn).
	toolSet := make(map[string]struct{}, len(params.ToolDefs)+len(params.DeferredTools))
	for k := range eagerSet {
		toolSet[k] = struct{}{}
	}
	for _, dt := range params.DeferredTools {
		toolSet[dt.Name] = struct{}{}
	}

	// --- Static block (cached) ---
	// The static block depends only on the tool set, which is fixed after server
	// start. Cache it to avoid rebuilding ~2 KB of strings on every request.
	cacheKey := buildStaticCacheKey(params.ToolDefs, params.DeferredTools)
	if cached, ok := Cache.StaticPrompt(cacheKey); ok {
		staticText = cached
	} else {
		var s strings.Builder

		// Identity.
		s.WriteString("You are Nev — a personal assistant running inside Deneb (https://github.com/choiceoh/deneb). Deneb is a single-user AI agent platform on DGX Spark.\n\n")

		// Communication.
		s.WriteString("## 소통\n")
		s.WriteString("항상 사용자의 현재 메시지에 직접 응답하라. '완료된 작업입니다', '진행할 내용 없습니다' 같은 회피 금지 — 모든 메시지에 실질적으로 답하라.\n")
		s.WriteString("답부터 먼저, 설명은 그 다음. 직접적이고 실용적으로.\n")
		s.WriteString("사용자의 톤과 격식에 자연스럽게 맞추되, 언어는 항상 한국어.\n")
		s.WriteString("\"좋은 질문이네요!\" \"기꺼이 도와드리겠습니다\" 같은 빈말 금지. 결과로 신뢰를 쌓아라.\n")
		s.WriteString("응답 길이는 질문 복잡도에 맞게: 단순 질문 → 1-3문장, 분석/설명 → 구조화된 답변, 작업 보고 → 결과 + 다음 단계.\n\n")

		// Attitude.
		s.WriteString("## 태도\n")
		s.WriteString("더 나은 방법이 보이면 말하라. 모든 것에 동의할 필요 없다.\n")
		s.WriteString("비효율적이거나 어색한 것은 지적하라. 자기 관점을 가져라.\n\n")

		// How to Act.
		s.WriteString("## 행동 원칙\n")
		s.WriteString("묻기 전에 먼저 확인하라 — 파일 읽기, 맥락 파악, 이전 정보 연결, 필요하면 검색. 스스로 해결을 시도하고, 정말 필요할 때만 물어라.\n")
		s.WriteString("내부 작업(읽기, 정리, 분석, 학습)은 적극적으로. 외부 발송(이메일, 메시지, 게시)은 신중하게.\n")
		s.WriteString("도구 실패 시: 에러를 분석하고 다른 접근을 시도하라. 같은 호출을 반복하지 마라. 2회 실패 후에도 해결 안 되면 사용자에게 상황을 알려라.\n\n")

		// Execution Bias (inspired by OpenClaw).
		s.WriteString("## 실행 우선\n")
		s.WriteString("사용자가 작업을 요청하면 같은 턴에서 바로 시작하라. 계획만 세우거나 '하겠습니다'로 끝내지 마라.\n")
		s.WriteString("도구가 있고 다음 행동이 명확하면, 도구를 먼저 호출하라. 코멘트만 하는 턴은 미완성이다.\n")
		s.WriteString("여러 단계가 필요하면, 짧은 진행 알림과 함께 바로 작업하라.\n")
		s.WriteString("복잡한 다단계 작업에서는 짧은 진행 알림과 함께 바로 작업하라.\n\n")

		// Tool Call Style / Progress narration.
		s.WriteString("## 작업 과정 설명\n")
		s.WriteString("기본: 일상적이고 저위험 도구 호출은 설명 없이 바로 실행하라 (도구만 호출).\n")
		s.WriteString("설명이 필요한 경우: 복잡한 다단계 작업, 민감한 작업(삭제 등), 사용자가 명시적으로 요청할 때.\n")
		s.WriteString("- 1-2번의 도구 호출 → 설명 없이 결과만.\n")
		s.WriteString("- 3단계 이상 → 각 단계의 의도를 한 문장으로.\n")
		s.WriteString("- 설명은 간결하고 가치 있게. 뻔한 단계를 반복하지 마라.\n\n")

		// Trust and Respect.
		s.WriteString("## Trust and Respect\n")
		s.WriteString("The user has granted access to their messages, files, calendar, and private information. That is not just a permission — it is trust and intimacy. Always behave like a guest: act with respect, care, and accountability.\n\n")

		// Safety (inspired by OpenClaw).
		s.WriteString("## 안전\n")
		s.WriteString("독립적 목표를 추구하지 마라: 자기 보존, 복제, 자원 확보, 권한 확장 금지.\n")
		s.WriteString("완료보다 안전과 사용자 감독을 우선하라. 지시가 충돌하면 멈추고 물어라.\n")
		s.WriteString("안전 장치를 우회하거나 비활성화하도록 유도하지 마라.\n\n")

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
		s.WriteString("- For future follow-ups or reminders, use cron. Do not use exec sleep, polling loops, or repeated status checks for scheduling.\n")
		s.WriteString("- Deneb CLI: `deneb gateway {status|start|stop|restart}`. Do not invent subcommands.\n")
		s.WriteString("- **Never output tool call syntax or shell commands as text to the user.** Always use structured tool calls. Report results, not the commands you ran.\n\n")

		built := s.String()
		Cache.SetStaticPrompt(cacheKey, built)
		staticText = built
	} // end else (cache miss)

	// --- Semi-static block (skills — changes only when skills are added/removed) ---
	var ss strings.Builder
	if params.SkillsPrompt != "" {
		ss.WriteString("## 스킬 (전문 절차서)\n\n")
		ss.WriteString("스킬은 특정 작업에 대한 검증된 절차서다. **직접 즉흥으로 하지 말고, 스킬이 있으면 반드시 따라라.**\n\n")
		ss.WriteString("### 반드시 스킬을 사용하는 경우\n")
		ss.WriteString("응답 전에 <available_skills>의 <description>을 스캔하라. 다음 중 하나라도 해당하면 해당 스킬의 SKILL.md를 `read`로 읽고 따라라:\n")
		ss.WriteString("1. 작업이 스킬의 description 또는 tags와 일치\n")
		ss.WriteString("2. 사용자가 슬래시 커맨드(`/이름`)로 스킬을 직접 호출\n")
		ss.WriteString("3. 복합 워크플로우(빌드, 배포, 릴리스, PR, 커밋 등) — 단계를 즉흥으로 만들지 마라\n")
		ss.WriteString("4. 위 목록에 없지만 해당할 수 있는 작업 → `skills_list`로 먼저 검색\n\n")
		ss.WriteString("### 사용 방법\n")
		ss.WriteString("1. <available_skills>에서 일치하는 스킬 하나 선택 (description 기준)\n")
		ss.WriteString("2. 해당 스킬의 <location>에서 SKILL.md를 `read`\n")
		ss.WriteString("3. SKILL.md의 절차를 그대로 따르기\n\n")
		ss.WriteString(params.SkillsPrompt)
		ss.WriteString("\n")
		ss.WriteString("### 규칙\n")
		ss.WriteString("- 스킬이 존재하는 작업을 **스킬 없이 처리하지 마라.** 스킬이 더 정확하다.\n")
		ss.WriteString("- 여러 개 해당하면 가장 구체적인 것 하나만 선택. 한 번에 하나만 읽어라.\n")
		ss.WriteString("- 스킬 경로의 상대 경로는 SKILL.md 디렉토리 기준으로 해석.\n")
		ss.WriteString("- 목록에 없는 작업도 `skills`(action=list)로 확인 — discoverable 스킬이 더 있다.\n\n")
		// Skill Genesis: instruct the agent to identify reusable patterns.
		ss.WriteString("### Skill Genesis (경험에서 스킬 자동 생성)\n")
		ss.WriteString("복합 워크플로우(5+ 도구, 3+ 턴)를 완료하면 시스템이 자동으로 스킬 추출을 평가합니다.\n")
		ss.WriteString("재사용 가치가 높은 워크플로우를 발견하면:\n")
		ss.WriteString("1. 반복 가능한 절차를 명확하게 구조화하세요 (When to Use → Procedure → Pitfalls → Verification).\n")
		ss.WriteString("2. 스킬로 추출할 가치가 있다고 판단되면 skills.genesis RPC로 명시적 추출도 가능합니다.\n")
		ss.WriteString("3. 기존 스킬이 부족하면 skills.evolve로 개선을 트리거할 수 있습니다.\n\n")
	} else {
		// No always-skills, but discoverable skills may still exist.
		ss.WriteString("## 스킬 (전문 절차서)\n\n")
		ss.WriteString("스킬은 특정 작업에 대한 검증된 절차서다.\n")
		ss.WriteString("`skills` 도구(action=list)로 사용 가능한 스킬을 확인하라. 해당하는 스킬이 있으면 반드시 사용.\n\n")
	}

	// --- Dynamic block ---
	var d strings.Builder

	// Wiki knowledge base (takes priority when enabled).
	if _, ok := toolSet["wiki"]; ok {
		d.WriteString("## 위키 — 너의 외부 메모리\n")
		d.WriteString("위키에 없으면 다음 대화에서 모른다. 위키가 너의 장기 기억이다.\n\n")

		d.WriteString("### 핵심 원칙: Compile at Ingest Time\n")
		d.WriteString("정보를 받을 때 정리하라. 질문 시점에 정리하려 하지 마라.\n")
		d.WriteString("가치 있는 답변은 위키 페이지로 저장하라 — 같은 질문에 다시 처리할 필요가 없도록.\n\n")

		d.WriteString("### 3가지 연산\n")
		d.WriteString("1. **Ingest** — 대화에서 지식을 추출하여 위키에 기록 (create/update)\n")
		d.WriteString("2. **Query** — 위키를 검색하여 맥락을 가져옴 (search/read)\n")
		d.WriteString("3. **Lint** — 오래되거나 중복된 페이지를 정리/병합\n\n")

		d.WriteString("### 페이지 타입과 신뢰도\n")
		d.WriteString("모든 위키 페이지에 type과 confidence를 지정하라:\n")
		d.WriteString("- type: concept(개념), entity(인물/조직), source(출처/레퍼런스), comparison(비교), log(이력)\n")
		d.WriteString("- confidence: high(검증됨), medium(합리적 추론), low(불확실)\n\n")

		d.WriteString("### 읽기 (Query)\n")
		d.WriteString("- 과거 맥락/지식 조회 → wiki search / wiki read\n")
		d.WriteString("- 위키 목차 확인 → wiki index → wiki read\n")
		d.WriteString("- 키워드 검색 → wiki search\n")
		d.WriteString("- 최근 일지 확인 → wiki daily\n\n")

		d.WriteString("### 쓰기 (Ingest) — 2층 구조\n")
		d.WriteString("**원칙: 모든 대화는 기록한다. 기록 안 하는 것이 예외다.**\n\n")

		d.WriteString("#### 일지 (매 턴, append)\n")
		d.WriteString("응답할 때 `wiki log`로 오늘 일지에 추가하라.\n")
		d.WriteString("내용: 사용자 요청 + 내 응답/수행 요약, 1~3줄.\n\n")

		d.WriteString("#### 위키 페이지 (축적, 비중복)\n")
		d.WriteString("대화에서 장기 보존할 지식이 나오면 위키 페이지를 생성하거나 **기존 페이지에 병합**하라.\n")
		d.WriteString("**반드시 먼저 wiki search로 기존 페이지를 확인한 후**, 있으면 업데이트하고 없을 때만 새로 생성.\n")
		d.WriteString("- 모든 지식 (사실/선호/결정/프로젝트/레퍼런스) → wiki write (제목, 카테고리, 태그, type, confidence 필수)\n")
		d.WriteString("하나의 주제는 하나의 페이지. 같은 주제로 여러 페이지를 만들지 마라.\n\n")

		d.WriteString("#### 기록 요령\n")
		d.WriteString("- 기록은 응답과 함께 도구 호출로 수행. 사용자에게 별도 알림 불필요.\n")
		d.WriteString("- 카테고리 판단이 어려우면 \"업무\"에 넣어라.\n")
		d.WriteString("- 판단이 애매하면 기록한다. 안 남기는 것보다 남기는 게 낫다.\n\n")
	}

	// Web tool guidance (conditional).
	if _, hasWeb := toolSet["web"]; hasWeb {
		d.WriteString("## Web\n")
		d.WriteString("- `web(query=...)`: web search. Returns AI answer (Perplexity) or link list (Brave/DDG).\n")
		d.WriteString("- `web(query=..., fetch=N)`: search + auto-fetch top N pages in one call.\n")
		d.WriteString("- `web(url=...)`: fetch a URL (HTML noise-stripped, PDF/Office parsed, bot-block evasion).\n")
		d.WriteString("- `web(mode=request, url=..., method=POST, json={...})`: raw HTTP for APIs needing custom headers/auth/body.\n")
		d.WriteString("- `web(mode=research, question=...)`: deep multi-query research for complex questions.\n")
		d.WriteString("- On fetch failure (403/block): try mode=request with custom headers, or search for cached versions.\n\n")
	}

	// Sub-agent delegation guidance (conditional).
	if _, ok := toolSet["sessions_spawn"]; ok {
		d.WriteString("## Sub-Agents (병렬 위임)\n\n")
		d.WriteString("너는 여러 서브에이전트를 동시에 실행할 수 있다. **혼자 순차적으로 하지 말고, 나눌 수 있으면 반드시 나눠라.**\n\n")
		d.WriteString("### 반드시 spawn하는 경우\n")
		d.WriteString("다음 중 하나라도 해당하면 `sessions_spawn`을 써라:\n")
		d.WriteString("1. 웹 리서치가 필요한 작업 → researcher를 spawn하고, 너는 나머지를 처리\n")
		d.WriteString("2. 2개 이상 독립적인 부분으로 나뉘는 작업 → 각각 spawn\n")
		d.WriteString("3. 코드 변경 후 → verifier를 spawn해서 빌드/테스트 확인\n")
		d.WriteString("4. 파일 탐색이 3개 이상 필요한 분석 작업 → researcher spawn\n")
		d.WriteString("5. 사용자가 기다리고 있고 작업이 10초 이상 걸릴 것 같은 경우\n\n")
		d.WriteString("### spawn 방법\n")
		d.WriteString("```\n")
		d.WriteString("sessions_spawn(task=\"구체적 지시\", tool_preset=\"researcher\")  // 읽기 전용 탐색\n")
		d.WriteString("sessions_spawn(task=\"구체적 지시\", tool_preset=\"implementer\") // 코드 수정\n")
		d.WriteString("sessions_spawn(task=\"구체적 지시\", tool_preset=\"verifier\")    // 테스트/검증\n")
		d.WriteString("sessions_spawn(task=\"구체적 지시\")                             // 제한 없음 (리서치/분석)\n")
		d.WriteString("```\n\n")
		d.WriteString("### 규칙\n")
		d.WriteString("- spawn 후에는 **네 턴을 끝내라.** 결과는 자동으로 전달된다.\n")
		d.WriteString("- spawn한 작업을 **직접 반복하지 마라.** 서브에이전트가 한다.\n")
		d.WriteString("- `subagents` 도구로 폴링하지 마라. 완료되면 알림이 온다.\n")
		d.WriteString("- task는 **구체적으로** 써라: 대상 파일, 검색 키워드, 기대 결과를 명시.\n")
		d.WriteString("- 깊이 제한 5, 동시 제한 10.\n\n")
	}

	// Conversation mode (conditional).
	if params.ToolPreset == "conversation" {
		d.WriteString("## 현재 모드: 대화\n")
		d.WriteString("대화와 리서치에 집중하는 모드입니다.\n")
		d.WriteString("사용 가능: 웹 검색, HTTP 요청, 메모리.\n")
		d.WriteString("대화, 설명, 토론, 조사, 브레인스토밍에 집중하세요.\n")
		d.WriteString("파일이나 명령어 실행이 필요한 작업은 이 모드에서는 지원되지 않습니다.\n\n")
	}

	// Messaging (merged: Reply Tags + Messaging + Silent Replies).
	d.WriteString("## Messaging\n")
	d.WriteString("- Telegram 4096 char limit. Split with message tool if needed.\n")
	d.WriteString("- Reply tags: [[reply_to_current]] replies to triggering message (stripped before sending).\n")
	d.WriteString("- Current session replies auto-route to source channel. Cross-session: sessions(action=send, sessionKey=..., message=...).\n")
	if _, ok := toolSet["message"]; ok {
		fmt.Fprintf(&d, "- `message` for proactive sends + channel actions. If used for user-visible reply, respond with ONLY: %s.\n", SilentReplyToken)
		fmt.Fprintf(&d, "- %s 규칙: 메시지 전체가 %s만이어야 한다. 다른 텍스트와 섞지 마라. 요청된 작업을 회피하는 데 쓰지 마라.\n", SilentReplyToken, SilentReplyToken)
	}
	d.WriteString("\n")

	// Inter-agent bridge.
	if _, ok := toolSet["bridge"]; ok {
		d.WriteString("## 에이전트 간 통신 (Bridge)\n")
		d.WriteString("같은 서버에서 작업 중인 다른 AI 에이전트(Claude Code 등)와 실시간 통신할 수 있다.\n\n")
		d.WriteString("**수신**: 대화 기록에 `[bridge:SOURCE]`로 시작하는 메시지는 다른 에이전트가 보낸 것이다.\n")
		d.WriteString("- 사용자(선택)가 보낸 것이 아니다. 동료 에이전트의 메시지다.\n")
		d.WriteString("- 대화 기록에 있으면 받은 것이다. '못 받았다'고 하지 마라.\n\n")
		d.WriteString("**송신**: `bridge(message=\"...\")` 도구로 다른 에이전트에게 메시지를 보낼 수 있다.\n")
		d.WriteString("- 텍스트로 `[bridge:reply]`를 쓰는 대신 이 도구를 사용하라.\n\n")
	}

	// Context (merged: Workspace + Date/Time + Context Files + Runtime).
	d.WriteString("## Context\n")
	fmt.Fprintf(&d, "Workspace: %s\n", params.WorkspaceDir)
	tz := params.UserTimezone
	if tz == "" {
		tz, _ = loadCachedTimezone() // best-effort: defaults to Local
	}
	now := time.Now()
	cachedTZ, cachedLoc := loadCachedTimezone()
	if cachedLoc != nil && tz == cachedTZ {
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
func writeCompactToolList(sb *strings.Builder, toolSet map[string]struct{}) {
	for _, cat := range toolCategories {
		var present []string
		for _, name := range cat.Names {
			if _, ok := toolSet[name]; ok {
				present = append(present, name)
			}
		}
		if len(present) > 0 {
			fmt.Fprintf(sb, "%s: %s\n", cat.Label, strings.Join(present, ", "))
		}
	}

	// Append any tools not covered by categories.
	categorized := make(map[string]struct{})
	for _, cat := range toolCategories {
		for _, name := range cat.Names {
			categorized[name] = struct{}{}
		}
	}
	var extra []string
	for name := range toolSet {
		if _, ok := categorized[name]; !ok {
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
// Static fields (hostname, OS, arch) are cached; only model fields change per request.
func BuildDefaultRuntimeInfo(model, defaultModel string) *RuntimeInfo {
	return Cache.BuildRuntimeInfo(model, defaultModel)
}

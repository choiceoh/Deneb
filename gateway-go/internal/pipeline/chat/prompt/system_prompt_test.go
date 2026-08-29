package prompt

import (
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/propus"
	"github.com/choiceoh/deneb/gateway-go/pkg/dentime"
)

func TestBuildSystemPromptRendersRequiredSections(t *testing.T) {
	params := SystemPromptParams{
		WorkspaceDir: "/home/user/project",
		ToolDefs: []ToolDef{
			{Name: "read"},
			{Name: "exec"},
			{Name: "wiki"},
		},
		UserTimezone: "Asia/Seoul",
		RuntimeInfo: &RuntimeInfo{
			Host:         "dgx-spark",
			OS:           "linux",
			Arch:         "arm64",
			Model:        "claude-sonnet-4-20250514",
			DefaultModel: "claude-sonnet-4-20250514",
		},
		Channel: "telegram",
	}

	prompt := BuildSystemPrompt(params)

	// Check required sections exist.
	sections := []string{
		"You are Nev — a personal assistant running inside Deneb (https://github.com/choiceoh/deneb).",
		"## Role",
		"## Communication",
		"## Attitude",
		"## Action Principles",
		"## Execution Bias",
		"## Trust and Respect",
		"## Safety",
		"## Tooling",
		"## Tool Usage",
		"## 위키 — 너의 외부 메모리",
		"## 사용자 모델 갱신",
		"## Messaging",
		"## Context",
		"/home/user/project",
		"Asia/Seoul",
		"host=dgx-spark",
		"channel=telegram",
	}

	for _, s := range sections {
		if !strings.Contains(prompt, s) {
			t.Errorf("system prompt missing section: %q", s)
		}
	}
}

func TestBuildSystemPromptRendersWorkPersona(t *testing.T) {
	tools := []ToolDef{{Name: "read"}, {Name: "wiki"}, {Name: "exec"}}

	// Business workspace: Nev persona + chief-of-staff role + wiki external
	// memory all present. The retired neutral-assistant identity must never
	// render — every
	// non-coding session gets the single chief-of-staff persona.
	work := BuildSystemPrompt(SystemPromptParams{ToolDefs: tools})
	for _, want := range []string{"You are Nev", "chief-of-staff", "## 위키 — 너의 외부 메모리", "## 작업 기억"} {
		if !strings.Contains(work, want) {
			t.Errorf("업무 prompt regression — missing %q", want)
		}
	}
	if strings.Contains(work, "Hindsight") {
		t.Error("업무 prompt should use wiki/diary work memory, not Hindsight")
	}
	if strings.Contains(work, "helpful, knowledgeable AI assistant") {
		t.Error("업무 prompt must not carry the retired 챗봇 neutral identity")
	}
	if !strings.Contains(work, "Call out inefficient or awkward choices") {
		t.Error("업무 prompt lost its proactive-critique attitude line")
	}
}

func TestEnglishControlPromptPreservesKoreanContracts(t *testing.T) {
	prompt := BuildSystemPrompt(SystemPromptParams{
		ToolDefs: []ToolDef{{Name: "gateway"}, {Name: "mail_archive"}, {Name: "groupware"}},
	})

	for _, want := range []string{
		"Always respond in Korean.",
		"'왜 대답이 없었어?'",
		"[SYSTEM: ... 전송이 확인되지 않았습니다 ...]",
		"'상태'·'재시작'·'업데이트'·'설정 변경'",
		"재고·출고·입고·발주",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("English control prompt lost Korean contract %q", want)
		}
	}
}

// TestBuildSystemPromptRendersPolarisSectionWhenToolPresent pins the tool-conditional recall coaching:
// coaching a model to call polaris when the preset withholds the tool produces
// failed tool-call loops, so the section renders only when the tool is present.
func TestBuildSystemPromptRendersPolarisSectionWhenToolPresent(t *testing.T) {
	with := BuildSystemPrompt(SystemPromptParams{ToolDefs: []ToolDef{{Name: "read"}, {Name: "polaris"}}})
	if !strings.Contains(with, "## Recall (polaris)") {
		t.Error("polaris-carrying prompt missing 회상 section")
	}
	without := BuildSystemPrompt(SystemPromptParams{ToolDefs: []ToolDef{{Name: "read"}}})
	if strings.Contains(without, "## Recall (polaris)") {
		t.Error("polaris-less prompt must not coach the polaris tool")
	}
}

func TestBuildSystemPromptRendersCompactToolList(t *testing.T) {
	params := SystemPromptParams{
		WorkspaceDir: "/tmp",
		ToolDefs: []ToolDef{
			{Name: "read"},
			{Name: "write"},
			{Name: "exec"},
		},
	}

	prompt := BuildSystemPrompt(params)

	// Should contain categorized tool list format.
	if !strings.Contains(prompt, "File: read, write") {
		t.Error("expected compact File category with read, write")
	}
	if !strings.Contains(prompt, "Exec: exec") {
		t.Error("expected compact Exec category")
	}
	// Should NOT contain verbose per-tool descriptions in the tool list.
	if strings.Contains(prompt, "- read: Read files") {
		t.Error("expected compact list, not verbose per-tool descriptions")
	}
}

func TestWriteCompactToolListSortsUncategorizedTools(t *testing.T) {
	var prompt strings.Builder
	writeCompactToolList(&prompt, toolNameSet{
		"office":           {},
		"goal":             {},
		"blackboard":       {},
		"mail_archive":     {},
		"heartbeat_update": {},
	})
	if got, want := prompt.String(), "Workflow: blackboard, goal\nOther: heartbeat_update, mail_archive, office\n"; got != want {
		t.Fatalf("uncategorized tool list = %q, want stable ordering %q", got, want)
	}
}

func TestBuildSystemPromptRendersSkillsAndThinPropusRouter(t *testing.T) {
	params := SystemPromptParams{
		WorkspaceDir: "/tmp",
		SkillsPrompt: "<available_skills>\n- test-skill\n</available_skills>",
	}

	prompt := buildSemiStaticPrompt(params, toolNameSet{"skills": {}})
	if !strings.Contains(prompt, "## Skills") {
		t.Error("missing skills section")
	}
	if !strings.Contains(prompt, "test-skill") {
		t.Error("missing skill content")
	}
	if !strings.Contains(prompt, "skills") {
		t.Error("missing skills tool hint for discoverable skills")
	}
	for _, stale := range []string{"목록의 설명을 스캔", "항목의 괄호 안 경로"} {
		if strings.Contains(prompt, stale) {
			t.Errorf("stale eager-catalog instruction %q remains in prompt", stale)
		}
	}
	for _, want := range []string{
		"### Propus",
		"Read and follow `evolution-proposal` SKILL.md",
		"`evolution-proposal` is the sole owner of detailed doctrine",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("missing thin Propus router content %q", want)
		}
	}

	doctrine := propus.PropusDoctrine()
	for _, detail := range []string{
		doctrine.Version,
		doctrine.SourceIDs()[0],
		doctrine.QualityGates[0],
		doctrine.ProductRules()[2],
	} {
		if strings.Contains(prompt, detail) {
			t.Errorf("ambient prompt must defer Propus detail %q to evolution-proposal", detail)
		}
	}

	start := strings.Index(prompt, "### Propus")
	if start < 0 {
		t.Fatal("missing Propus router")
	}
	if got := len(prompt[start:]); got > 1_000 {
		t.Errorf("ambient Propus router = %d bytes, want <= 1000; move procedure detail to evolution-proposal", got)
	}
}

func TestBuildSystemPromptRendersSkillsSectionWithoutSkills(t *testing.T) {
	// "WithoutSkills" here means without an AMBIENT catalog (SkillsPrompt is
	// empty), not without the skills tool: the fallback it asserts tells the
	// model to reach the tool, so the run has to be able to. Declaring it keeps
	// the original intent while the block now gates on tool availability.
	params := SystemPromptParams{
		WorkspaceDir: "/tmp",
		ToolDefs:     []ToolDef{{Name: "skills"}},
	}

	prompt := BuildSystemPrompt(params)
	// Even without always-skills, the skills section appears with skills tool hint.
	if !strings.Contains(prompt, "## Skills") {
		t.Error("skills section should always appear with skills tool hint")
	}
	if !strings.Contains(prompt, "skills") {
		t.Error("missing skills tool hint")
	}
}

func TestBuildSystemPromptNoMemoryWithoutTools(t *testing.T) {
	params := SystemPromptParams{
		WorkspaceDir: "/tmp",
		ToolDefs: []ToolDef{
			{Name: "read"},
		},
	}

	prompt := BuildSystemPrompt(params)
	if strings.Contains(prompt, "## Memory Recall") {
		t.Error("memory section should not appear without memory tools")
	}
}

func TestBuildRuntimeLineWithFullInfo(t *testing.T) {
	info := &RuntimeInfo{
		AgentID: "default",
		Host:    "dgx-spark",
		OS:      "linux",
		Arch:    "arm64",
		Model:   "claude-sonnet-4-20250514",
	}

	line := buildRuntimeLine(info, "telegram")

	if !strings.Contains(line, "agent=default") {
		t.Error("missing agent ID")
	}
	if !strings.Contains(line, "host=dgx-spark") {
		t.Error("missing host")
	}
	if !strings.Contains(line, "channel=telegram") {
		t.Error("missing channel")
	}
}

func TestBuildRuntimeLine_NilInfo(t *testing.T) {
	line := buildRuntimeLine(nil, "telegram")
	if !strings.HasPrefix(line, "Runtime:") {
		t.Error("expected line to start with Runtime:")
	}
	if !strings.Contains(line, "channel=telegram") {
		t.Error("expected channel even with nil info")
	}
	// Should not contain any agent/host/os fields
	if strings.Contains(line, "agent=") || strings.Contains(line, "host=") {
		t.Error("expected no agent/host fields with nil info")
	}
}

func TestBuildRuntimeLineWithoutChannel(t *testing.T) {
	info := &RuntimeInfo{
		Host: "my-host",
		OS:   "linux",
		Arch: "arm64",
	}
	line := buildRuntimeLine(info, "")
	if strings.Contains(line, "channel=") {
		t.Error("expected no channel field when empty")
	}
	if !strings.Contains(line, "host=my-host") {
		t.Error("missing host")
	}
	if !strings.Contains(line, "os=linux(arm64)") {
		t.Error("missing os(arch)")
	}
}

func TestBuildRuntimeLineWithDefaultModel(t *testing.T) {
	info := &RuntimeInfo{
		Model:        "claude-opus-4-6",
		DefaultModel: "claude-sonnet-4-6",
	}
	line := buildRuntimeLine(info, "")
	if !strings.Contains(line, "model=claude-opus-4-6") {
		t.Error("missing model")
	}
	if !strings.Contains(line, "default_model=claude-sonnet-4-6") {
		t.Error("missing default_model")
	}
}

func TestBuildSystemPromptRendersMessagingGuidanceWithMessageTool(t *testing.T) {
	params := SystemPromptParams{
		WorkspaceDir: "/tmp",
		ToolDefs: []ToolDef{
			{Name: "message"},
		},
	}

	prompt := BuildSystemPrompt(params)
	if !strings.Contains(prompt, SilentReplyToken) {
		t.Error("expected SilentReplyToken in messaging section when message tool is available")
	}
	if !strings.Contains(prompt, "외부 채널 전송이 실패하면 전달 상태는 실패/미확인이다.") {
		t.Error("expected explicit external-delivery failure guidance")
	}
}

func TestBuildSystemPromptOmitsMessagingGuidanceWithoutMessageTool(t *testing.T) {
	params := SystemPromptParams{
		WorkspaceDir: "/tmp",
		ToolDefs: []ToolDef{
			{Name: "read"},
		},
	}

	prompt := BuildSystemPrompt(params)
	// The message-specific guidance (proactive sends, NO_REPLY) should not appear without message tool
	if strings.Contains(prompt, "proactive sends") {
		t.Error("message-specific guidance should not appear without message tool")
	}
}

func TestBuildDefaultRuntimeInfoReturnsModelAndOS(t *testing.T) {
	info := BuildDefaultRuntimeInfo("claude-sonnet-4-6", "claude-sonnet-4-6")
	if info == nil {
		t.Fatal("expected non-nil RuntimeInfo")
	}
	if info.Model != "claude-sonnet-4-6" {
		t.Errorf("got %s, want model claude-sonnet-4-6", info.Model)
	}
	if info.DefaultModel != "claude-sonnet-4-6" {
		t.Errorf("got %s, want default_model claude-sonnet-4-6", info.DefaultModel)
	}
	if info.OS != "linux" {
		t.Errorf("got %q, want OS \"linux\"", info.OS)
	}
	if info.Arch == "" {
		t.Error("expected Arch to be set from runtime.GOARCH")
	}
}

func TestBuildSystemPromptWithoutPilotReferences(t *testing.T) {
	params := SystemPromptParams{
		WorkspaceDir: "/tmp",
		ToolDefs: []ToolDef{
			{Name: "read"},
		},
	}

	prompt := BuildSystemPrompt(params)
	if strings.Contains(prompt, "pilot") {
		t.Error("pilot references should not appear in system prompt")
	}
}

// TestResolveTimezoneReturnsConfiguredZone is a regression guard for the
// timezone mismatch where resolveTimezone() read only the TZ env var and the
// server-local zone abbreviation, ignoring DENEB_TIMEZONE and the config
// "timezone" key. On a UTC container (the common deployment) that made the
// system prompt show UTC while logs, cron, and the calendar briefing — all
// dentime-based — ran in the configured zone (typically KST). resolveTimezone
// must now agree with pkg/dentime.
func TestResolveTimezoneReturnsConfiguredZone(t *testing.T) {
	t.Setenv("DENEB_TIMEZONE", "Asia/Seoul")
	dentime.ResetCache()
	t.Cleanup(dentime.ResetCache)

	if got := resolveTimezone(); got != "Asia/Seoul" {
		t.Fatalf("resolveTimezone() = %q, want %q (must defer to dentime, not server-local UTC)", got, "Asia/Seoul")
	}
}

// TestBuildSystemPromptRendersDateInConfiguredZone verifies the rendered date line
// uses the configured zone, not the server-local zone. With an explicit
// UserTimezone the prompt must render "now" in that zone — proving the
// day-only date can flip a calendar day relative to UTC.
func TestBuildSystemPromptRendersDateInConfiguredZone(t *testing.T) {
	params := SystemPromptParams{
		WorkspaceDir: "/tmp",
		ToolDefs:     []ToolDef{{Name: "read"}},
		UserTimezone: "Asia/Seoul",
	}
	prompt := BuildSystemPrompt(params)

	if !strings.Contains(prompt, "(timezone: Asia/Seoul)") {
		t.Fatalf("system prompt missing configured timezone label; got:\n%s", prompt)
	}
	wantDate := time.Now().In(time.FixedZone("KST", 9*60*60)).Format("Monday, January 2, 2006")
	if !strings.Contains(prompt, wantDate) {
		t.Errorf("system prompt date not rendered in Asia/Seoul; want %q in prompt", wantDate)
	}
}

func TestBuildSystemPromptRendersDateFromInjectedTime(t *testing.T) {
	params := SystemPromptParams{
		WorkspaceDir: "/tmp",
		ToolDefs:     []ToolDef{{Name: "read"}},
		UserTimezone: "Asia/Seoul",
		Now:          time.Date(2040, time.December, 31, 16, 30, 0, 0, time.UTC),
	}
	prompt := BuildSystemPrompt(params)
	if want := "Tuesday, January 1, 2041"; !strings.Contains(prompt, want) {
		t.Fatalf("system prompt did not use injected semantic time %q; got:\n%s", want, prompt)
	}
}

func TestBuildSystemPromptBlocksReturnsSameContentAsString(t *testing.T) {
	params := SystemPromptParams{
		WorkspaceDir: "/tmp",
		ToolDefs: []ToolDef{
			{Name: "read"},
			{Name: "exec"},
		},
		UserTimezone: "UTC",
	}

	stringPrompt := BuildSystemPrompt(params)
	blocks := BuildSystemPromptBlocks(params)

	// Blocks should concatenate to the same content as string version.
	var combined strings.Builder
	for _, b := range blocks {
		combined.WriteString(b.Text)
	}

	if combined.String() != stringPrompt {
		t.Error("BuildSystemPromptBlocks content should match BuildSystemPrompt")
	}
}

// TestBuildSystemPromptBlocksPreservesCacheControlBoundary asserts the cache_control
// allocation: Static + Semi-static carry ephemeral markers; Dynamic does NOT.
// This invariant matters because Anthropic limits a request to 4 cache_control
// breakpoints. The 2 system markers leave room for 2 trailing message markers
// added by chat/buildTrailingCacheHook (Hermes Agent's "system_and_3" pattern).
// If the dynamic block ever regains a marker, trailing markers would push the
// request past the 4-breakpoint limit and the dynamic content (recall memory,
// timestamp, runtime info) would still cache-miss every turn.
func TestBuildSystemPromptBlocksPreservesCacheControlBoundary(t *testing.T) {
	params := SystemPromptParams{
		WorkspaceDir: "/tmp",
		ToolDefs:     []ToolDef{{Name: "read"}},
		SkillsPrompt: `<available_skills><skill><name>x</name></skill></available_skills>`,
	}
	blocks := BuildSystemPromptBlocks(params)
	if len(blocks) < 2 {
		t.Fatalf("expected at least 2 blocks (static + dynamic), got %d", len(blocks))
	}
	if blocks[0].CacheControl == nil || blocks[0].CacheControl.Type != "ephemeral" {
		t.Errorf("static block missing ephemeral cache_control: %+v", blocks[0].CacheControl)
	}
	last := blocks[len(blocks)-1]
	if last.CacheControl != nil {
		t.Errorf("dynamic block must NOT carry cache_control (would consume a breakpoint without reuse)")
	}
	if len(blocks) == 3 {
		if blocks[1].CacheControl == nil || blocks[1].CacheControl.Type != "ephemeral" {
			t.Errorf("semi-static (skills) block missing ephemeral cache_control: %+v", blocks[1].CacheControl)
		}
	}
}

// TestBuildSystemPromptBlocksEmitsCompactionNoteWhenFired asserts the P4
// invariant: when CompactionFired=true, the dynamic block carries a one-
// time reminder that summaries are present in history. The reminder
// references the SUMMARY_PREFIX marker so the model bridges the two
// signals (system note + per-message prefix).
func TestBuildSystemPromptBlocksEmitsCompactionNoteWhenFired(t *testing.T) {
	base := SystemPromptParams{
		WorkspaceDir: "/tmp",
		ToolDefs:     []ToolDef{{Name: "read"}},
	}

	noFlag := BuildSystemPromptBlocks(base)
	dynNoFlag := noFlag[len(noFlag)-1].Text
	if strings.Contains(dynNoFlag, "압축되었") {
		t.Errorf("compaction note must NOT appear when CompactionFired=false; dynamic=%q", dynNoFlag)
	}

	withFlag := base
	withFlag.CompactionFired = true
	flagged := BuildSystemPromptBlocks(withFlag)
	dynFlagged := flagged[len(flagged)-1].Text
	if !strings.Contains(dynFlagged, "압축되었") {
		t.Errorf("compaction note missing when CompactionFired=true; dynamic=%q", dynFlagged)
	}
	if !strings.Contains(dynFlagged, "[컨텍스트 요약 — 참고 전용]") {
		t.Errorf("compaction note must reference summary marker so the model bridges the two signals; dynamic=%q", dynFlagged)
	}
}

func TestBuildSystemPromptRendersWikiSaveGuidance(t *testing.T) {
	params := SystemPromptParams{
		WorkspaceDir: "/tmp",
		ToolDefs: []ToolDef{
			{Name: "wiki"},
		},
	}

	prompt := BuildSystemPrompt(params)

	invariants := []string{
		"wiki write/log에 쓰는 내용은 사용자에게 보이지 않는다",
		"위키 저장은 응답이 아니다",
		"응답 텍스트에 직접 써라",
		"\"위키에 정리해뒀어\" / \"저장했어\" 만으로 응답을 끝내지 마라",
	}
	for _, s := range invariants {
		if !strings.Contains(prompt, s) {
			t.Errorf("wiki guidance missing invariant: %q", s)
		}
	}
}

func TestBuildSystemPromptRendersConversationModeWithPreset(t *testing.T) {
	params := SystemPromptParams{
		WorkspaceDir: "/tmp",
		ToolDefs: []ToolDef{
			{Name: "web"},
			{Name: "wiki"},
		},
		ToolPreset: "conversation",
	}

	prompt := BuildSystemPrompt(params)
	if !strings.Contains(prompt, "현재 모드: 대화") {
		t.Error("conversation mode block should appear when ToolPreset is 'conversation'")
	}
	if !strings.Contains(prompt, "대화와 리서치에 집중하는 모드") {
		t.Error("conversation mode should describe focus on dialogue and research")
	}
}

func TestBuildSystemPromptOmitsConversationModeWithoutPreset(t *testing.T) {
	params := SystemPromptParams{
		WorkspaceDir: "/tmp",
		ToolDefs: []ToolDef{
			{Name: "read"},
			{Name: "write"},
			{Name: "exec"},
			{Name: "web"},
			{Name: "wiki"},
		},
	}

	prompt := BuildSystemPrompt(params)
	if strings.Contains(prompt, "현재 모드: 대화") {
		t.Error("conversation mode block should NOT appear in normal mode")
	}
}

func TestWriteCompactToolList_UncategorizedTools(t *testing.T) {
	toolSet := map[string]struct{}{
		"read":        {},
		"custom_tool": {},
	}

	var sb strings.Builder
	writeCompactToolList(&sb, toolSet)
	output := sb.String()

	if !strings.Contains(output, "File: read") {
		t.Error("expected categorized read in File group")
	}
	if !strings.Contains(output, "Other: custom_tool") {
		t.Error("expected uncategorized tool in Other group")
	}
}

func TestBuildSystemPromptRendersWebGuidanceWithWebTool(t *testing.T) {
	params := SystemPromptParams{
		WorkspaceDir: "/tmp",
		ToolDefs: []ToolDef{
			{Name: "web"},
		},
	}

	prompt := BuildSystemPrompt(params)
	if !strings.Contains(prompt, "## Web") {
		t.Error("expected ## Web section when web tool is registered")
	}
	if !strings.Contains(prompt, "web(query=...)") {
		t.Error("expected web search guidance")
	}
	if !strings.Contains(prompt, "fetch failure") || !strings.Contains(prompt, "403") {
		t.Error("expected fetch failure guidance")
	}
}

func TestBuildSystemPrompt_NoWebGuidanceWithoutTools(t *testing.T) {
	params := SystemPromptParams{
		WorkspaceDir: "/tmp",
		ToolDefs: []ToolDef{
			{Name: "read"},
		},
	}

	prompt := BuildSystemPrompt(params)
	if strings.Contains(prompt, "## Web\n") {
		t.Error("web guidance should not appear without web/http tools")
	}
}

func TestBuildSystemPromptGroupwareTriggers(t *testing.T) {
	params := SystemPromptParams{
		WorkspaceDir: "/tmp",
		ToolDefs: []ToolDef{
			{Name: "groupware"},
			{Name: "calendar"},
			{Name: "people"},
		},
	}
	prompt := BuildSystemPrompt(params)
	for _, want := range []string{
		"`groupware`가 1순위",
		`groupware(area="people")`,
		"Business: groupware",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("missing %q in prompt", want)
		}
	}
	if !strings.Contains(prompt, "사내 참석자는 `groupware") {
		t.Error("expected meeting-prep to mention groupware people")
	}
}

// Every line of the skills block routes through skills(action=read/list) or
// fetch_tools(query="skills"), and a restricted preset gates both — the
// allow-list decides what fetch_tools may activate. A run without the skills
// tool therefore cannot reach a single instruction there, so the block must not
// ship. The rest of this file already gates polaris/wiki/web/calendar/
// sessions_spawn/workfeed/bridge on toolSet; skills gated only on the briefcase
// flag.
func TestSemiStaticSkillsBlockGatesOnTheSkillsTool(t *testing.T) {
	params := SystemPromptParams{
		WorkspaceDir: "/tmp",
		SkillsPrompt: "<available_skills>\n- test-skill\n</available_skills>",
	}

	if got := buildSemiStaticPrompt(params, toolNameSet{"wiki": {}, "read": {}}); got != "" {
		t.Errorf("a run without the skills tool still got the skills block:\n%s", got)
	}
	// The no-ambient-catalog fallback also tells the model to fetch_tools the
	// skills tool, so it is just as unreachable.
	noCatalog := SystemPromptParams{WorkspaceDir: "/tmp"}
	if got := buildSemiStaticPrompt(noCatalog, toolNameSet{"wiki": {}}); got != "" {
		t.Errorf("the no-catalog fallback shipped without the skills tool:\n%s", got)
	}
	// A run that CAN reach the tool keeps everything.
	full := buildSemiStaticPrompt(params, toolNameSet{"skills": {}})
	if !strings.Contains(full, "test-skill") || !strings.Contains(full, "Propus") {
		t.Errorf("an unrestricted run lost the skills block:\n%s", full)
	}
	// Briefcase suppression still wins regardless of the tool set.
	briefcase := params
	briefcase.DisableSkills = true
	if got := buildSemiStaticPrompt(briefcase, toolNameSet{"skills": {}}); got != "" {
		t.Errorf("briefcase mode leaked the skills block:\n%s", got)
	}
}

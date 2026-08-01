// heartbeat_task.go — Periodic task that checks HEARTBEAT.md for autonomous work.
//
// Every 30 minutes during active hours (08:00–23:00 Asia/Seoul), reads
// ~/.deneb/HEARTBEAT.md and executes its instructions as a full agent turn.
// Users write tasks into HEARTBEAT.md and the agent picks them up autonomously.
// Outside active hours, or if the file is missing/empty, the task is a no-op —
// unless the proactive signal pass or the research lane (heartbeat_research.go,
// self-queued "[연구]" items from accumulated new data) warrants a turn.
//
// The heartbeat turn reasons in an ISOLATED session (HeartbeatWorkSessionKey,
// "submain:heartbeat"), NOT the user's live client:main. Running on client:main
// used to force a Polaris compaction of the user's conversation every tick (the
// 12K history budget below) — the top cause of the live session losing detail.
// Progress state lives in HEARTBEAT.md, so the isolated session needs no shared
// transcript; the user-facing report is delivered separately via the proactive
// relay (RelayNative → client:main + push), so isolation costs no visibility.
//
// Persistence is isolated from the chat transcript:
//   - EphemeralUser=true   → the trigger user-role message is NOT persisted
//     (recurring noise must not crowd out the 24-message recent window or
//     bias the LLM into modeling fake user requests).
//   - EphemeralAssistant=true → assistant/tool_result messages are NOT
//     persisted either. Heartbeat progress state must live in HEARTBEAT.md
//     (last report time/status/archive), not in the user's short-term chat
//     window. This prevents autonomous ticks from resetting or crowding out
//     the user's active conversation context.
//
// Inspired by OpenClaw's heartbeat system.
package heartbeat

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/monitoring"
	runtimesession "github.com/choiceoh/deneb/gateway-go/internal/domain/session"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/prompt"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
)

// Compile-time interface compliance.
var _ autonomous.PeriodicTask = (*heartbeatTask)(nil)

// heartbeatTask implements autonomous.PeriodicTask.
// Every 30 minutes, checks HEARTBEAT.md and executes tasks found there.
type heartbeatTask struct {
	chatHandler chatport.SyncRunner
	activity    *monitoring.ActivityTracker
	logger      *slog.Logger
	homeDir     string

	// collectSignals, when set, gathers a transport-agnostic snapshot of the
	// user's recent state (calendar conflicts, imminent events, etc.) each tick.
	// When DetectSignals finds escalation-worthy anomalies, a concise Korean
	// summary is prepended to the trigger so the agent prioritizes them — the
	// proactive "find the problem in the noise" layer the Claw-Anything paper
	// (docs/research/claw-anything-always-on-assistant.md, finding B) calls for.
	//
	// This is purely additive: signals enrich an existing heartbeat turn but
	// never suppress the user's HEARTBEAT.md checks. Nil → no augmentation
	// (default), so behavior is unchanged when no collector is wired.
	collectSignals func(ctx context.Context) autonomous.SignalInputs
	signalConfig   autonomous.SignalConfig

	// proposedSelfCoding, when set, reports the pending (status=proposed)
	// self-improvement coding candidates: count plus a change fingerprint.
	// Drives the self-coding review lane (heartbeat_selfcoding.go). Nil → lane
	// disabled (tests, genesis tracker absent).
	proposedSelfCoding func() (count int, fingerprint string)

	// dispatchBacklogSelfCoding, when set, reports how many code-scope
	// accepted candidates are waiting for coding-dispatch. The generator sweep
	// must not mine more while the consumer backlog is non-empty (proposed is
	// already covered by proposedSelfCoding above).
	dispatchBacklogSelfCoding func() int

	// promoteRecurrences, when set, deterministically converts fresh
	// target-recurrence signals ("the accepted evolve did not stick") into
	// proposed candidates each tick, BEFORE the lanes below look at the queue
	// — so a promotion is consumed by the review lane in the same tick. Nil →
	// disabled.
	promoteRecurrences func() (int, error)

	// promoteClusters, when set, deterministically converts the top recurring
	// failure clusters into proposed candidates each tick (no LLM call), so the
	// queue is fed even when the LLM sweep turn ignores its nudge. Nil → disabled.
	promoteClusters func() (int, error)

	// selfImproveSignals, when set, reports the capture-side funnel summary
	// plus the 7d target-recurrence count. Drives the sweep generator lane
	// (heartbeat_selfimprove_sweep.go). Nil → lane disabled.
	selfImproveSignals func() (genesis.SelfCorrectionFunnelSummary, int)

	// selfImproveEvidence, when set, returns the fleet-wide failure evidence
	// bundle (deterministic signature clusters, support-ordered) that the sweep
	// nudge renders so the turn starts from recurring cross-case mechanisms
	// instead of bare counters. Called only when the sweep actually fires
	// (≤ once per interval). Nil → the nudge falls back to counters only.
	selfImproveEvidence func(limit int) []genesis.FailureClusterSummary

	// idleSkillReview, when set, fires one fenced Propus review when no
	// review has completed for a long stretch (user away, deploy churn
	// cancelling forks) — the idle backstop that keeps the review loop fed
	// on quiet days. The closure owns staleness/retry pacing and returns
	// whether a review actually ran plus a short detail for the log. Runs
	// with the deterministic lanes, before the turn-gating check, so it
	// fires even on an otherwise empty tick. Nil → lane disabled. See
	// heartbeat_idle_review.go.
	idleSkillReview func(ctx context.Context) (fired bool, detail string)

	// model is the model role the heartbeat turn runs on ("submain" when
	// agents.submainModel is configured, else "" → main). Moves autonomous
	// volume off the interactive main subscription.
	model string

	// deliver, when set, hands the finished report to the proactive relay
	// (RelayNative → client:main + push). The turn now reasons in an isolated
	// session, so this explicit relay is how the report reaches the user;
	// RelayNative suppresses NO_REPLY/empty output. Nil in tests → no delivery.
	deliver func(text string) (bool, error)

	// nowFn overrides the task clock in tests so the active-hours gate is
	// deterministic (a real run leaves it nil → time.Now).
	nowFn func() time.Time
}

// Task runs the periodic HEARTBEAT.md and proactive-signal workflow.
type Task = heartbeatTask

// TaskConfig contains the execution and signal boundaries for Task.
type TaskConfig struct {
	ChatHandler               chatport.SyncRunner
	Activity                  *monitoring.ActivityTracker
	Logger                    *slog.Logger
	HomeDir                   string
	CollectSignals            func(context.Context) autonomous.SignalInputs
	SignalConfig              autonomous.SignalConfig
	ProposedSelfCoding        func() (count int, fingerprint string)
	DispatchBacklogSelfCoding func() int
	PromoteRecurrences        func() (int, error)
	PromoteClusters           func() (int, error)
	SelfImproveSignals        func() (genesis.SelfCorrectionFunnelSummary, int)
	SelfImproveEvidence       func(limit int) []genesis.FailureClusterSummary
	IdleSkillReview           func(context.Context) (bool, string)
	Model                     string
	Deliver                   func(text string) (bool, error)
	Now                       func() time.Time
}

// NewTask constructs the periodic heartbeat worker.
func NewTask(cfg TaskConfig) *Task {
	return &heartbeatTask{
		chatHandler:               cfg.ChatHandler,
		activity:                  cfg.Activity,
		logger:                    cfg.Logger,
		homeDir:                   cfg.HomeDir,
		collectSignals:            cfg.CollectSignals,
		signalConfig:              cfg.SignalConfig,
		proposedSelfCoding:        cfg.ProposedSelfCoding,
		dispatchBacklogSelfCoding: cfg.DispatchBacklogSelfCoding,
		promoteRecurrences:        cfg.PromoteRecurrences,
		promoteClusters:           cfg.PromoteClusters,
		selfImproveSignals:        cfg.SelfImproveSignals,
		selfImproveEvidence:       cfg.SelfImproveEvidence,
		idleSkillReview:           cfg.IdleSkillReview,
		model:                     cfg.Model,
		deliver:                   cfg.Deliver,
		nowFn:                     cfg.Now,
	}
}

// now returns the task clock (nowFn in tests, time.Now in production).
func (t *heartbeatTask) now() time.Time {
	if t.nowFn != nil {
		return t.nowFn()
	}
	return time.Now()
}

// Name returns the component's stable scheduler name.
func (t *heartbeatTask) Name() string { return "heartbeat" }

// Interval returns the component's scheduling cadence.
func (t *heartbeatTask) Interval() time.Duration { return 30 * time.Minute }

// Active-hours window (Asia/Seoul). Outside this window, Run() is a no-op.
// Matches agents.defaults.heartbeat.activeHours in deneb.json.
const (
	heartbeatActiveStartHour = 8
	heartbeatActiveEndHour   = 23
	// Safety bound on assembled history. The heartbeat now runs in an isolated,
	// ephemeral session (HeartbeatWorkSessionKey) that persists nothing, so in
	// practice there is no history to trim — this cap only guards an unexpectedly
	// non-empty assembly. It no longer forces a compaction of the user's
	// client:main session (the prior behavior that lost live-chat detail).
	heartbeatHistoryBudget = 12_000
	// Hard cap for an autonomous heartbeat turn's model-emitted tool calls.
	// The self-coding review contract says "max 2 candidates", but production
	// 2026-07-24 showed a heartbeat sweep ignoring that soft limit and issuing
	// 100+ skill_lifecycle review calls against terminal candidates. Keep this
	// above observed healthy self-coding runs (~23 calls) while bounding a
	// runaway loop before it floods runtime-health with tool-complete errors.
	heartbeatMaxToolCallAttempts = 24
)

// heartbeatTriggerTemplate is injected as a user-role message into the active
// session. It carries HEARTBEAT.md verbatim and reminds the agent of the
// reply-suppression contract. The agent is expected to consult prior session
// context (commitments, user replies) when deciding what to do.
//
// The leading prompt.HeartbeatTriggerPrefix matches a system-prompt rule that
// teaches the LLM to treat such messages as self-triggers, not real user
// input. Keep that constant the single source of truth.
const heartbeatTriggerTemplate = prompt.HeartbeatTriggerPrefix + ` 30분 주기 자동 점검입니다. 사용자가 직접 보낸 메시지가 아닙니다.

규칙:
- 아래 HEARTBEAT.md 지시를 따르세요. 이 점검은 독립 세션에서 돌아 대화 맥락을 공유하지 않으니, 진행 상태·직전 보고 여부·이전 약속은 HEARTBEAT.md의 마지막 보고 시각·상태 줄을 유일한 기준으로 판단하세요.
- 이미 사용자가 답해서 처리된 항목은 다시 묻지 말고 곧장 실행하세요.
- 진행에 꼭 필요한데 사용자만 아는 핵심 정보가 비어 막혔다면, 추측으로 메우지 말고 그 항목에서 한 번 질문하세요. 단 아직 답을 못 받은 같은 질문을 다음 점검에서 반복하지는 마세요(NO_REPLY 유지) — 답이 오면 그때 진행합니다.
- 직전 하트비트에서 이미 같은 보고를 했고 새 진전이 없으면 본문에 정확히 ` + "`NO_REPLY`" + ` 한 단어만 출력하세요(다른 텍스트 금지). 사용자가 같은 응답을 두 번 받지 않도록 매우 엄격히 지키세요.
- 알릴지 말지는 비대칭으로 판단하세요. 결재·승인 대기, 외부 약속·확답, 임박한 마감, 리스크 임박처럼 임원이 직접 판단해야 하고 놓치면 비용이 큰 건은 적극 알리세요. 반대로 단순 정보(FYI)·위임 가능한 실무·이미 보고한 진행 경과는 지금 끼어들지 말고 NO_REPLY로 두세요(다음 모닝레터가 묶어 다룹니다). 애매하면 침묵 쪽 — 능동 알림은 과개입이 미개입보다 비쌉니다.
- 사용자가 "그만"·"중단"·"하지 마"·"꺼" 같은 중단 의사를 표현했다면, 해당 항목을 HEARTBEAT.md에서 제거하고 NO_REPLY를 출력하세요.
- 사용자에게 실제로 보고할 때(NO_REPLY가 아닐 때), 내용이 구조적(현황·수치·목록·결정 요청)이면 산문 나열 대신 deneb-ui 카드로 보고하세요 — 특히 사용자의 결정이 필요한 보고는 승인/대안 버튼을 단 인터랙티브 카드가 기본입니다.
- 직전 보고 여부와 진행 상태는 대화 transcript가 아니라 HEARTBEAT.md의 마지막 보고 시각·상태 줄을 기준으로 판단하세요. 하트비트 턴의 응답은 단기 대화 컨텍스트에 저장되지 않을 수 있습니다.

작업 종료 시 HEARTBEAT.md 갱신(필수, heartbeat_update 도구만 사용):
- 일반 fs.write/edit는 workspace 밖이라 통하지 않습니다. 반드시 heartbeat_update 도구로 ~/.deneb/HEARTBEAT.md를 통째로 새 내용으로 덮어쓰세요.
- 완료된 항목은 새 content에서 빼서 호출하세요. 사용자가 중단을 요청한 항목도 즉시 빼세요.
- 진행 중인 항목은 마지막 보고 시각·상태를 같은 줄에 갱신하세요
  (예: "[진행중 18:21 — pull 95%%, 다음 점검에서 결과 확인]").
- ★사용자 행동이 필요 없는 순수 상태 메모(스윕 점검 기록·"상태 유지"·완료 확인 등)는 본문이 아니라 "## status" 섹션 아래에 두세요. "## status"와 "## archive" 아래 내용은 다음 점검을 깨우지 않습니다 — 실행할 일이 남은 항목만 본문에 남기세요. 본문에 상태 메모를 두면 매 30분 풀 턴이 그걸 다시 읽고 NO_REPLY만 내는 공회전이 됩니다.
- 동일 항목이 3회 연속 진전 없이 반복되면 파일 하단의 "## archive" 섹션으로 이동하고 본문에서는 제거하세요.
- 모든 항목이 종료되면 content="" 로 호출해 파일을 비우세요. 다음 점검은 자동 skip 됩니다.
- heartbeat_update는 직전 내용을 HEARTBEAT.md.prev로 자동 백업하므로 잘못 지웠을 때 사용자가 복구할 수 있습니다.

---
HEARTBEAT.md:
%s`

// Run executes one scheduled task cycle.
func (t *heartbeatTask) Run(ctx context.Context) error {
	if t.chatHandler == nil || !t.chatHandler.ChatReady() {
		return nil
	}

	if !withinActiveHours(t.now()) {
		t.logger.Debug("heartbeat: skipped, outside active hours")
		return nil
	}

	// Skip if the user is actively using the system — before any signal work, so
	// we don't fetch the calendar just to discard the result. Avoids racing a
	// turn in flight or interrupting the user mid-conversation.
	if t.activity != nil {
		idleMs := time.Now().UnixMilli() - t.activity.LastActivityAt()
		idle := time.Duration(idleMs) * time.Millisecond
		if idle < 1*time.Minute {
			t.logger.Debug("heartbeat: skipped, user active", "idle", idle.Round(time.Second))
			return nil
		}
	}

	content := t.readHeartbeat()
	// Scaffolding-only files (just section headers / archived items) count as
	// empty: production 2026-07-05 showed a file holding only "## Active Tasks"
	// kept every 30-min tick paying a full ~29K-token cloud turn — roughly half
	// the day's model spend — to conclude there was nothing to do.
	if !heartbeatHasTasks(content) {
		content = ""
	}

	// Proactive signal pass: cheap, runs before the LLM turn. Detected anomalies
	// (calendar conflicts, imminent meetings, …) both enrich a HEARTBEAT.md run
	// AND can initiate one on their own when HEARTBEAT.md is empty — the
	// signal-driven proactivity the Claw-Anything paper calls for (finding B).
	signalSummary := t.detectSignalSummary(ctx)

	// Research lane: when enough new data accumulated (mail analyses, wiki
	// writes), ask this turn to formulate its own "[연구]" items — see
	// heartbeat_research.go. Deterministic scan, throttled to ~once a day.
	researchNudge := t.detectResearchNudge(time.Now())

	// Deterministic capture first: fresh target recurrences become proposed
	// candidates NOW, so the review lane below sees them in this same tick
	// instead of one interval later.
	if t.promoteRecurrences != nil {
		if promoted, err := t.promoteRecurrences(); err != nil {
			t.logger.Warn("heartbeat: target-recurrence promotion failed", "error", err)
		} else if promoted > 0 {
			t.logger.Info("heartbeat: target-recurrence candidates promoted", "count", promoted)
		}
	}
	if t.promoteClusters != nil {
		if promoted, err := t.promoteClusters(); err != nil {
			t.logger.Warn("heartbeat: failure-cluster promotion failed", "error", err)
		} else if promoted > 0 {
			t.logger.Info("heartbeat: failure-cluster candidates promoted", "count", promoted)
		}
	}

	// Idle review backstop: with no real user turns for a long stretch the
	// nudger never fires (cron/system sessions are excluded by design), so
	// Propus starves — re-review the most recent real session directly. Runs
	// here with the deterministic lanes so it fires even when the tick has
	// nothing else to do.
	if t.idleSkillReview != nil {
		if fired, detail := t.idleSkillReview(ctx); fired {
			t.logger.Info("heartbeat: idle skill review fired", "detail", detail)
		}
	}

	// Self-coding review lane: pending self-improvement candidates get
	// consumed by this turn instead of waiting on the operator to notice the
	// 자가코딩 개선 screen — see heartbeat_selfcoding.go.
	selfCodingNudge := t.detectSelfCodingNudge(time.Now())

	// Self-improvement sweep lane: when the queue is EMPTY but rejection or
	// recurrence signals accumulated, this turn mines them and proposes
	// candidates itself — see heartbeat_selfimprove_sweep.go. Mutually
	// exclusive with the review lane by construction (fires only at count 0).
	sweepNudge := t.detectSelfImproveSweepNudge(time.Now())

	// Nothing to do: no user checks, no signals, no lane nudges.
	if !heartbeatShouldRun(content, signalSummary, selfCodingNudge, sweepNudge, researchNudge) {
		t.logger.Debug("heartbeat: skipped, no actionable tasks or signals")
		return nil
	}
	if signalSummary != "" {
		t.logger.Info("heartbeat: proactive signals detected", "hasHeartbeatMd", content != "")
	}

	// The heartbeat reasons in its OWN isolated session (never client:main), so
	// the autonomous tick doesn't assemble or compact the user's live
	// conversation — the prior client:main piggyback forced a Polaris compaction
	// every tick. The user-facing report is delivered separately via t.deliver
	// (the proactive relay), not by sharing this session's transcript.
	sessionKey := runtimesession.HeartbeatWorkSessionKey

	triggerMsg := fmt.Sprintf(heartbeatTriggerTemplate, composeHeartbeatBody(signalSummary, content, selfCodingNudge, sweepNudge, researchNudge))

	runCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	req := heartbeatSyncRequest()
	req.SessionKey = sessionKey
	req.Message = triggerMsg
	req.Model = t.model
	result, err := t.chatHandler.RunSync(runCtx, req)

	// Fixture harvest (P0 of instruction-surface evolve): persist this firing's
	// variable inputs + outcome so a future shadow-replay gate has a real
	// corpus. Best-effort by design — recorded for failed turns too (the error
	// IS ground truth), and never allowed to affect the turn result.
	fixture := heartbeatFixture{
		FiredAt:         time.Now().UnixMilli(),
		SessionKey:      sessionKey,
		SignalSummary:   signalSummary,
		SelfCodingNudge: selfCodingNudge,
		SweepNudge:      sweepNudge,
		ResearchNudge:   researchNudge,
		HeartbeatMD:     content,
	}
	if err != nil {
		fixture.OutcomeErr = err.Error()
	} else {
		fixture.OutcomeText = result.Text
	}
	t.recordHeartbeatFixture(fixture)
	// Post-apply anomaly watch (instruction-surface P2): consecutive failed
	// heartbeat turns after an auto-applied HEARTBEAT.md restore the backup.
	// No-op unless an auto-apply marker exists.
	if t.homeDir != "" {
		noteHeartbeatTurnOutcome(filepath.Join(t.homeDir, ".deneb", "HEARTBEAT.md"), err == nil, t.logger)
	}

	if err != nil {
		return fmt.Errorf("heartbeat: agent turn failed: %w", err)
	}

	// Deliver the report to the user's channel via the proactive relay. The turn
	// ran in an isolated session, so this explicit relay (RelayNative →
	// client:main + push) is how the user sees it; RelayNative suppresses
	// NO_REPLY/empty output, so a nothing-to-report tick delivers nothing.
	delivered := false
	if t.deliver != nil {
		var derr error
		if delivered, derr = t.deliver(result.BestText); derr != nil {
			t.logger.Error("heartbeat: report delivery failed", "error", derr)
		}
	}

	t.logger.Info(
		"heartbeat completed",
		"output_len", len(result.BestText),
		"delivered", delivered,
		"session", sessionKey,
	)
	return nil
}

// detectSignalSummary runs the proactive signal pass and returns a concise Korean
// summary when escalation-worthy anomalies are found, or "" (no collector wired,
// or nothing noteworthy).
func (t *heartbeatTask) detectSignalSummary(ctx context.Context) string {
	if t.collectSignals == nil {
		return ""
	}
	report := autonomous.DetectSignals(t.collectSignals(ctx), t.signalConfig)
	if !report.ShouldEscalate() {
		return ""
	}
	return report.Summary(t.signalConfig.MaxReasonsPerKind)
}

// heartbeatShouldRun reports whether a heartbeat turn is warranted: the user
// has HEARTBEAT.md checks, there are escalation-worthy signals to surface, or
// a lane (self-coding review, new-data research) fired a nudge. Pure for unit
// testing.
func heartbeatShouldRun(content, signalSummary, selfCodingNudge, sweepNudge, researchNudge string) bool {
	return strings.TrimSpace(content) != "" || strings.TrimSpace(signalSummary) != "" ||
		strings.TrimSpace(selfCodingNudge) != "" || strings.TrimSpace(sweepNudge) != "" ||
		strings.TrimSpace(researchNudge) != ""
}

// heartbeatHasTasks reports whether HEARTBEAT.md carries anything the agent
// should act on. Blank lines, markdown headings ("## Active Tasks"), full-line
// HTML comments, and everything under an "## archive" section are scaffolding
// — the file template survives an emptied task list, and archived items are by
// definition parked. Only a remaining content line makes the tick worth a turn.
//
// Heading rules (post-#3100 review): "#urgent LC 확인" is a hashtag-tagged task,
// not a heading (a '#' run must be followed by whitespace); and a nested
// heading inside the archive section ("### 2026-07") does not leave archive —
// only a same-or-higher-level sibling section does.
func heartbeatHasTasks(content string) bool {
	// >0 → inside an ignored ("## archive" / "## status") section; value = that
	// heading's level. "status" carries lane bookkeeping (e.g. the
	// self-improvement sweep's last-check notes): production 2026-07-18..08-01
	// showed such notes parked at top level made 92% of firings "has tasks",
	// each paying a full submain turn (~29K tokens) to re-read its own
	// bookkeeping and conclude NO_REPLY (91% of 105 firings). Same disease as
	// the 2026-07-05 scaffolding fix, new carrier.
	ignoredLevel := 0
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if level, title, ok := markdownHeading(trimmed); ok {
			switch {
			case strings.EqualFold(title, "archive") || strings.EqualFold(title, "status"):
				ignoredLevel = level
			case ignoredLevel > 0 && level > ignoredLevel:
				// Subheading nested under an ignored section — stays ignored.
			default:
				ignoredLevel = 0
			}
			continue
		}
		if ignoredLevel > 0 || strings.HasPrefix(trimmed, "<!--") {
			continue
		}
		return true
	}
	return false
}

// markdownHeading parses a "## Title"-style line from already-trimmed input.
// The '#' run must be 1-6 long and followed by whitespace (or end the line);
// anything else — like a "#hashtag task" — is content, not a heading.
func markdownHeading(trimmed string) (level int, title string, ok bool) {
	i := 0
	for i < len(trimmed) && trimmed[i] == '#' {
		i++
	}
	if i == 0 || i > 6 {
		return 0, "", false
	}
	if i < len(trimmed) && trimmed[i] != ' ' && trimmed[i] != '\t' {
		return 0, "", false
	}
	return i, strings.TrimSpace(trimmed[i:]), true
}

// composeHeartbeatBody builds the trigger body from the (optional) signal
// summary, (optional) HEARTBEAT.md content, and (optional) lane nudges.
// Signals lead so the agent prioritizes them; lanes come after the user's own
// checks — self-coding review (actionable now), then the sweep generator
// (mutually exclusive with review), then research (formulating new work).
// When there is no HEARTBEAT.md, a short note tells the agent the injected
// blocks are the only agenda (and to stay non-intrusive). Pure for unit
// testing.
func composeHeartbeatBody(signalSummary, content, selfCodingNudge, sweepNudge, researchNudge string) string {
	signalSummary = strings.TrimSpace(signalSummary)
	content = strings.TrimSpace(content)
	selfCodingNudge = strings.TrimSpace(selfCodingNudge)
	sweepNudge = strings.TrimSpace(sweepNudge)
	researchNudge = strings.TrimSpace(researchNudge)
	sections := make([]string, 0, 5)
	if signalSummary != "" {
		sections = append(sections, signalSummary)
	}
	if content != "" {
		sections = append(sections, content)
	}
	if selfCodingNudge != "" {
		sections = append(sections, selfCodingNudge)
	}
	if sweepNudge != "" {
		sections = append(sections, sweepNudge)
	}
	if researchNudge != "" {
		sections = append(sections, researchNudge)
	}
	body := strings.Join(sections, "\n\n---\n")
	if content == "" && body != "" {
		body += "\n\n(현재 HEARTBEAT.md에 등록된 작업은 없습니다. 위 내용만 검토해, 정말 알릴 가치가 있을 때만 사용자에게 간결히 알리세요.)"
	}
	return body
}

func heartbeatSyncRequest() chatport.SyncRequest {
	maxToolCallAttempts := heartbeatMaxToolCallAttempts
	return chatport.SyncRequest{
		MaxHistoryTokens:    heartbeatHistoryBudget,
		MaxToolCallAttempts: &maxToolCallAttempts,
		EphemeralUser:       true,
		EphemeralAssistant:  true,
		// The report is delivered by the proactive relay after the run (see Run),
		// so tell the agent NOT to use the in-loop message tool — that path is a
		// benign no-op under auto-delivery and would otherwise error.
		AutoDeliveredOutput: true,
	}
}

// withinActiveHours reports whether the given instant falls inside the
// heartbeat active-hours window, evaluated in Asia/Seoul. Falls back to UTC
// if the timezone database is unavailable.
func withinActiveHours(now time.Time) bool {
	loc, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		loc = time.UTC
	}
	hour := now.In(loc).Hour()
	return hour >= heartbeatActiveStartHour && hour < heartbeatActiveEndHour
}

// readHeartbeat reads ~/.deneb/HEARTBEAT.md if it exists.
// Returns empty string if not found or empty.
func (t *heartbeatTask) readHeartbeat() string {
	if t.homeDir == "" {
		return ""
	}

	path := filepath.Join(t.homeDir, ".deneb", "HEARTBEAT.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return "" // Not found or not readable — silent skip.
	}

	content := strings.TrimSpace(string(data))
	if content == "" {
		return ""
	}
	return content
}

// heartbeat_task.go — Periodic task that checks HEARTBEAT.md for autonomous work.
//
// Every 30 minutes during active hours (08:00–23:00 Asia/Seoul), reads
// ~/.deneb/HEARTBEAT.md and executes its instructions as a full agent turn.
// Users write tasks into HEARTBEAT.md and the agent picks them up autonomously.
// Outside active hours, or if the file is missing/empty, the task is a no-op.
//
// The heartbeat turn is dispatched into the user's most recently active native
// client session (tracked via ActivityTracker.LastSessionKey), falling back to
// client:main when no native session has been seen yet. Sharing the session
// means the agent sees prior commitments and user replies in the same
// transcript, instead of running in an isolated stateless channel.
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
package server

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/monitoring"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/prompt"
)

// Compile-time interface compliance.
var _ autonomous.PeriodicTask = (*heartbeatTask)(nil)

// heartbeatTask implements autonomous.PeriodicTask.
// Every 30 minutes, checks HEARTBEAT.md and executes tasks found there.
type heartbeatTask struct {
	chatHandler *chat.Handler
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
}

func (t *heartbeatTask) Name() string            { return "heartbeat" }
func (t *heartbeatTask) Interval() time.Duration { return 30 * time.Minute }

// Active-hours window (Asia/Seoul). Outside this window, Run() is a no-op.
// Matches agents.defaults.heartbeat.activeHours in deneb.json.
const (
	heartbeatActiveStartHour = 8
	heartbeatActiveEndHour   = 23
	// Heartbeat piggybacks on a live client session for recent commitments, but
	// production logs showed routine ticks assembling 60K+ history tokens from
	// client:main. Keep enough recent transcript to resolve follow-ups while
	// forcing Polaris to summarize the long tail before the autonomous check.
	heartbeatHistoryBudget = 12_000
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
- 아래 HEARTBEAT.md 지시를 따르되, 같은 세션의 직전 대화(사용자 응답·이전 약속)를 반드시 반영하세요.
- 이미 사용자가 답해서 처리된 항목은 다시 묻지 말고 곧장 실행하세요.
- 진행에 꼭 필요한데 사용자만 아는 핵심 정보가 비어 막혔다면, 추측으로 메우지 말고 그 항목에서 한 번 질문하세요. 단 아직 답을 못 받은 같은 질문을 다음 점검에서 반복하지는 마세요(NO_REPLY 유지) — 답이 오면 그때 진행합니다.
- 직전 하트비트에서 이미 같은 보고를 했고 새 진전이 없으면 본문에 정확히 ` + "`NO_REPLY`" + ` 한 단어만 출력하세요(다른 텍스트 금지). 사용자가 같은 응답을 두 번 받지 않도록 매우 엄격히 지키세요.
- 알릴지 말지는 비대칭으로 판단하세요. 결재·승인 대기, 외부 약속·확답, 임박한 마감, 리스크 임박처럼 임원이 직접 판단해야 하고 놓치면 비용이 큰 건은 적극 알리세요. 반대로 단순 정보(FYI)·위임 가능한 실무·이미 보고한 진행 경과는 지금 끼어들지 말고 NO_REPLY로 두세요(다음 모닝레터가 묶어 다룹니다). 애매하면 침묵 쪽 — 능동 알림은 과개입이 미개입보다 비쌉니다.
- 사용자가 "그만"·"중단"·"하지 마"·"꺼" 같은 중단 의사를 표현했다면, 해당 항목을 HEARTBEAT.md에서 제거하고 NO_REPLY를 출력하세요.
- 직전 보고 여부와 진행 상태는 대화 transcript가 아니라 HEARTBEAT.md의 마지막 보고 시각·상태 줄을 기준으로 판단하세요. 하트비트 턴의 응답은 단기 대화 컨텍스트에 저장되지 않을 수 있습니다.

작업 종료 시 HEARTBEAT.md 갱신(필수, heartbeat_update 도구만 사용):
- 일반 fs.write/edit는 workspace 밖이라 통하지 않습니다. 반드시 heartbeat_update 도구로 ~/.deneb/HEARTBEAT.md를 통째로 새 내용으로 덮어쓰세요.
- 완료된 항목은 새 content에서 빼서 호출하세요. 사용자가 중단을 요청한 항목도 즉시 빼세요.
- 진행 중인 항목은 마지막 보고 시각·상태를 같은 줄에 갱신하세요
  (예: "[진행중 18:21 — pull 95%%, 다음 점검에서 결과 확인]").
- 동일 항목이 3회 연속 진전 없이 반복되면 파일 하단의 "## archive" 섹션으로 이동하고 본문에서는 제거하세요.
- 모든 항목이 종료되면 content="" 로 호출해 파일을 비우세요. 다음 점검은 자동 skip 됩니다.
- heartbeat_update는 직전 내용을 HEARTBEAT.md.prev로 자동 백업하므로 잘못 지웠을 때 사용자가 복구할 수 있습니다.

---
HEARTBEAT.md:
%s`

func (t *heartbeatTask) Run(ctx context.Context) error {
	if t.chatHandler == nil {
		return nil
	}

	if !withinActiveHours(time.Now()) {
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

	// Proactive signal pass: cheap, runs before the LLM turn. Detected anomalies
	// (calendar conflicts, imminent meetings, …) both enrich a HEARTBEAT.md run
	// AND can initiate one on their own when HEARTBEAT.md is empty — the
	// signal-driven proactivity the Claw-Anything paper calls for (finding B).
	signalSummary := t.detectSignalSummary(ctx)

	// Nothing to do: no user-configured checks and no escalation-worthy signals.
	if !heartbeatShouldRun(content, signalSummary) {
		return nil
	}
	if signalSummary != "" {
		t.logger.Info("heartbeat: proactive signals detected", "hasHeartbeatMd", content != "")
	}

	// Resolve the target session: latest active native session, or the native
	// work home if the app has not recorded a session yet. Heartbeat piggybacks
	// on the user's native transcript so prior commitments and replies are
	// visible to the agent.
	lastSessionKey := ""
	if t.activity != nil {
		lastSessionKey = t.activity.LastSessionKey()
	}
	sessionKey := heartbeatTargetSessionKey(lastSessionKey)

	triggerMsg := fmt.Sprintf(heartbeatTriggerTemplate, composeHeartbeatBody(signalSummary, content))

	runCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	opts := heartbeatSyncOptions()
	result, err := t.chatHandler.SendSync(runCtx, sessionKey, triggerMsg, "", opts)
	if err != nil {
		return fmt.Errorf("heartbeat: agent turn failed: %w", err)
	}

	t.logger.Info(
		"heartbeat completed",
		"output_len", len(result.Text),
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

// heartbeatShouldRun reports whether a heartbeat turn is warranted: either the
// user has HEARTBEAT.md checks, or there are escalation-worthy signals to surface.
// Pure for unit testing.
func heartbeatShouldRun(content, signalSummary string) bool {
	return strings.TrimSpace(content) != "" || strings.TrimSpace(signalSummary) != ""
}

// composeHeartbeatBody builds the trigger body from the (optional) signal summary
// and (optional) HEARTBEAT.md content. Signals lead so the agent prioritizes them;
// when there is no HEARTBEAT.md, a short note tells the agent the signals are the
// only agenda (and to stay non-intrusive). Pure for unit testing.
func composeHeartbeatBody(signalSummary, content string) string {
	signalSummary = strings.TrimSpace(signalSummary)
	content = strings.TrimSpace(content)
	switch {
	case signalSummary != "" && content != "":
		return signalSummary + "\n\n---\n" + content
	case signalSummary != "":
		return signalSummary + "\n\n(현재 HEARTBEAT.md에 등록된 작업은 없습니다. 위 감지 신호만 검토해, 정말 알릴 가치가 있을 때만 사용자에게 간결히 알리세요.)"
	default:
		return content
	}
}

func heartbeatSyncOptions() *chat.SyncOptions {
	return &chat.SyncOptions{
		MaxHistoryTokens:   heartbeatHistoryBudget,
		EphemeralUser:      true,
		EphemeralAssistant: true,
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

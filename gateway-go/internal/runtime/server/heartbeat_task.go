// heartbeat_task.go — Periodic task that checks HEARTBEAT.md for autonomous work.
//
// Every 30 minutes during active hours (08:00–23:00 Asia/Seoul), reads
// ~/.deneb/HEARTBEAT.md and executes its instructions as a full agent turn.
// Users write tasks into HEARTBEAT.md and the agent picks them up autonomously.
// Outside active hours, or if the file is missing/empty, the task is a no-op.
//
// The heartbeat turn is dispatched into the user's most recently active
// telegram session (tracked via ActivityTracker.LastSessionKey). Sharing the
// session means the agent sees prior commitments and user replies in the same
// transcript, instead of running in an isolated stateless channel. When no
// telegram session has been seen yet, the task is a no-op.
//
// Persistence is asymmetric:
//   - EphemeralUser=true   → the trigger user-role message is NOT persisted
//     (recurring noise must not crowd out the 24-message recent window or
//     bias the LLM into modeling fake user requests).
//   - EphemeralAssistant=false → the assistant's reply IS persisted, so the
//     next iteration can see "did I already report this 30 minutes ago?" and
//     avoid duplicate broadcasts. Combined with the trigger template's
//     instruction to update HEARTBEAT.md when work concludes, this gives the
//     agent both context (prior reports + user replies) and an action
//     (file edit) for breaking the repeat-loop.
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
}

func (t *heartbeatTask) Name() string            { return "heartbeat" }
func (t *heartbeatTask) Interval() time.Duration { return 30 * time.Minute }

// Active-hours window (Asia/Seoul). Outside this window, Run() is a no-op.
// Matches agents.defaults.heartbeat.activeHours in deneb.json.
const (
	heartbeatActiveStartHour = 8
	heartbeatActiveEndHour   = 23
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
- 직전 하트비트에서 이미 같은 보고를 했고 새 진전이 없으면 본문에 정확히 ` + "`NO_REPLY`" + ` 한 단어만 출력하세요(다른 텍스트 금지). 사용자가 같은 응답을 두 번 받지 않도록 매우 엄격히 지키세요.
- 사용자가 "그만"·"중단"·"하지 마"·"꺼" 같은 중단 의사를 표현했다면, 해당 항목을 HEARTBEAT.md에서 제거하고 NO_REPLY를 출력하세요.

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

	content := t.readHeartbeat()
	if content == "" {
		// No HEARTBEAT.md or empty — skip silently.
		return nil
	}

	// Resolve the target session: latest active telegram session. Heartbeat
	// piggybacks on the user's session so prior commitments and replies are
	// visible to the agent. Without an active session, skip entirely instead
	// of falling back to an isolated channel.
	if t.activity == nil {
		t.logger.Debug("heartbeat: skipped, no activity tracker")
		return nil
	}
	sessionKey := t.activity.LastSessionKey()
	if !strings.HasPrefix(sessionKey, "telegram:") {
		t.logger.Debug("heartbeat: skipped, no active telegram session", "sessionKey", sessionKey)
		return nil
	}

	// Skip if user is actively using the system. Avoids racing a turn that
	// is in flight or interrupting the user mid-conversation.
	idleMs := time.Now().UnixMilli() - t.activity.LastActivityAt()
	idle := time.Duration(idleMs) * time.Millisecond
	if idle < 1*time.Minute {
		t.logger.Debug("heartbeat: skipped, user active", "idle", idle.Round(time.Second))
		return nil
	}

	triggerMsg := fmt.Sprintf(heartbeatTriggerTemplate, content)

	runCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	// Asymmetric persistence: drop the recurring trigger so it can't bias
	// the model into modeling fake user requests, but KEEP the assistant's
	// reply. Persisting the response lets the next heartbeat see prior
	// reports + the user's responses to them, which is how the agent
	// decides whether to NO_REPLY versus repeat itself.
	opts := &chat.SyncOptions{
		EphemeralUser:      true,
		EphemeralAssistant: false,
	}
	result, err := t.chatHandler.SendSync(runCtx, sessionKey, triggerMsg, "", opts)
	if err != nil {
		return fmt.Errorf("heartbeat: agent turn failed: %w", err)
	}

	t.logger.Info("heartbeat completed",
		"output_len", len(result.Text),
		"session", sessionKey,
	)
	return nil
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

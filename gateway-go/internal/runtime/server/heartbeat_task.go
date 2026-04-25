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

// heartbeatTriggerPrefix marks heartbeat-injected user-role messages so the
// agent can distinguish them from real user input. The system prompt instructs
// the agent to treat anything starting with this prefix as an autonomous
// self-trigger rather than a user request.
const heartbeatTriggerPrefix = "[시스템 하트비트]"

// heartbeatTriggerTemplate is injected as a user-role message into the active
// session. It carries HEARTBEAT.md verbatim and reminds the agent of the
// reply-suppression contract. The agent is expected to consult prior session
// context (commitments, user replies) when deciding what to do.
const heartbeatTriggerTemplate = heartbeatTriggerPrefix + ` 30분 주기 자동 점검입니다. 사용자가 직접 보낸 메시지가 아닙니다.

규칙:
- 아래 HEARTBEAT.md 지시를 따르되, 같은 세션의 직전 대화(사용자 응답·이전 약속)를 반드시 반영하세요.
- 이미 사용자가 답해서 처리된 항목은 다시 묻지 말고 곧장 실행하세요.
- 점검 결과 새로 알릴 것이 없으면 본문에 정확히 ` + "`NO_REPLY`" + ` 한 단어만 출력하세요(다른 텍스트 금지).

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

	prompt := fmt.Sprintf(heartbeatTriggerTemplate, content)

	runCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	result, err := t.chatHandler.SendSync(runCtx, sessionKey, prompt, "", nil)
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

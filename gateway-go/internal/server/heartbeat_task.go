// heartbeat_task.go — Periodic task that checks HEARTBEAT.md for autonomous work.
//
// Every 5 minutes, reads ~/.deneb/HEARTBEAT.md and executes its instructions
// as a full agent turn. Users write tasks into HEARTBEAT.md and the agent
// picks them up autonomously. If no file exists or it's empty, the task is a no-op.
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

	"github.com/choiceoh/deneb/gateway-go/internal/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/monitoring"
)

// heartbeatTask implements autonomous.PeriodicTask.
// Every 5 minutes, checks HEARTBEAT.md and executes tasks found there.
type heartbeatTask struct {
	chatHandler *chat.Handler
	activity    *monitoring.ActivityTracker
	logger      *slog.Logger
	homeDir     string
}

func (t *heartbeatTask) Name() string            { return "heartbeat" }
func (t *heartbeatTask) Interval() time.Duration { return 3 * time.Minute }

const heartbeatSessionKey = "system:heartbeat"

// heartbeatSystemPrompt wraps HEARTBEAT.md content for the agent.
const heartbeatSystemPrompt = `[시스템 하트비트 — 3분 주기 자율 작업 확인]

아래는 HEARTBEAT.md의 내용입니다. 이 파일에 정의된 작업을 수행하세요.
파일 내용을 엄격히 따르세요. 이전 대화에서 추론하거나 이전 작업을 반복하지 마세요.
주의가 필요한 것이 없으면 아무것도 하지 마세요.

---
%s`

func (t *heartbeatTask) Run(ctx context.Context) error {
	if t.chatHandler == nil {
		return nil
	}

	content := t.readHeartbeat()
	if content == "" {
		// No HEARTBEAT.md or empty — skip silently.
		return nil
	}

	// Skip if user is actively using the system.
	if t.activity != nil {
		idleMs := time.Now().UnixMilli() - t.activity.LastActivityAt()
		idle := time.Duration(idleMs) * time.Millisecond
		if idle < 2*time.Minute {
			t.logger.Debug("heartbeat: skipped, user active", "idle", idle.Round(time.Second))
			return nil
		}
	}

	prompt := fmt.Sprintf(heartbeatSystemPrompt, content)

	runCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	result, err := t.chatHandler.SendSync(runCtx, heartbeatSessionKey, prompt, "", nil)
	if err != nil {
		return fmt.Errorf("heartbeat: agent turn failed: %w", err)
	}

	t.logger.Info("heartbeat completed",
		"output_len", len(result.Text),
		"session", heartbeatSessionKey,
	)
	return nil
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

// diary_heartbeat_task.go — 2-hour periodic task that triggers the main LLM
// to write a detailed diary entry about recent events. Unlike SQL facts which
// are terse key-value pairs, diary entries capture rich narrative context:
// conversations, decisions, tool usage, errors, and resolutions.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/monitoring"
)

// minIdleDuration is the minimum user idle time required before a diary
// heartbeat is allowed to run. If the user sent input less than this
// duration ago, the heartbeat is skipped to avoid interrupting active work.
const minIdleDuration = 5 * time.Minute

// diaryHeartbeatTask implements autonomous.PeriodicTask.
// Every 2 hours it sends a system prompt to the main LLM asking it to
// review recent activity and write a detailed diary entry using the
// memory log action. The task only runs when the user has been idle for
// at least 5 minutes, so it never interrupts active sessions.
type diaryHeartbeatTask struct {
	chatHandler *chat.Handler
	activity    *monitoring.ActivityTracker
	logger      *slog.Logger
}

func (t *diaryHeartbeatTask) Name() string            { return "diary-heartbeat" }
func (t *diaryHeartbeatTask) Interval() time.Duration { return 2 * time.Hour }

// diaryHeartbeatPrompt instructs the LLM to write a detailed diary entry.
const diaryHeartbeatPrompt = `[시스템 다이어리 하트비트 — 2시간 주기]

지난 2시간 동안 일어난 일을 다이어리에 상세히 기록하세요.
SQL 팩트처럼 짧게 쓰지 말고, 서술형으로 풍부하게 작성하세요.

기록할 내용:
- 사용자와 나눈 대화 요약 (주제, 요청, 결론)
- 실행한 도구와 그 결과 (코드 변경, 빌드, 테스트 등)
- 내린 결정과 그 이유
- 발생한 오류/문제와 해결 과정
- 사용자 반응과 피드백
- 진행 중인 작업 상태

아무 활동이 없었으면 memory(action=log, title="하트비트", query="활동 없음 — 대기 상태") 로 간단히 기록.
활동이 있었으면 memory(action=log, title="[주제]", query="...상세 서술...") 로 기록.
여러 주제가 있으면 각각 별도의 log 호출로 분리하세요.`

func (t *diaryHeartbeatTask) Run(ctx context.Context) error {
	if t.chatHandler == nil {
		return fmt.Errorf("diary-heartbeat: chat handler not available")
	}

	// Skip if user is actively using the system (idle < 5 min).
	if t.activity != nil {
		idleMs := time.Now().UnixMilli() - t.activity.LastActivityAt()
		idle := time.Duration(idleMs) * time.Millisecond
		if idle < minIdleDuration {
			t.logger.Info("diary-heartbeat: skipped, user active",
				"idle", idle.Round(time.Second))
			return nil
		}
	}

	sessionKey := "system:diary-heartbeat"

	// Run a synchronous agent turn with the diary prompt.
	runCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	result, err := t.chatHandler.SendSync(runCtx, sessionKey, diaryHeartbeatPrompt, "", nil)
	if err != nil {
		return fmt.Errorf("diary-heartbeat: agent turn failed: %w", err)
	}

	t.logger.Info("diary-heartbeat completed",
		"output_len", len(result.Text),
		"session", sessionKey,
	)
	return nil
}

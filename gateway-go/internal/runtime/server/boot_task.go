// boot_task.go — Startup agent turn (inspired by OpenClaw's BOOT.md).
//
// On first run (30s after gateway start), reads ~/.deneb/BOOT.md and executes
// it as a full agent turn. This allows the agent to perform initialization
// tasks: check for updates, summarize overnight activity, run diagnostics, etc.
//
// Subsequent runs (every 24h) act as daily "morning check" — the agent reviews
// what happened since its last check and takes proactive action.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/monitoring"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolpreset"
)

// bootTask implements autonomous.PeriodicTask.
// Runs a full agent turn using BOOT.md content on startup and daily thereafter.
type bootTask struct {
	chatHandler *chat.Handler
	activity    *monitoring.ActivityTracker
	logger      *slog.Logger
	homeDir     string
	firstRun    atomic.Bool // true after the initial boot run
}

func (t *bootTask) Name() string            { return "boot" }
func (t *bootTask) Interval() time.Duration { return 24 * time.Hour }

// defaultBootPrompt is used when no BOOT.md file exists.
const defaultBootPrompt = `[시스템 부트 — 게이트웨이 시작됨]

게이트웨이가 방금 시작되었습니다. 다음 작업을 수행하세요:

1. 시스템 상태 확인 (health 도구 사용)
2. 최근 메모리 확인 — 마지막 세션 이후 기억해야 할 것이 있는지
3. 중요한 변화가 있으면 사용자에게 간략히 알림 (memory log로 기록)

아무 이상 없으면 memory(action=log, title="부트 완료", query="정상 시작, 이상 없음") 으로 기록.`

func (t *bootTask) Run(ctx context.Context) error {
	if t.chatHandler == nil {
		return fmt.Errorf("boot: chat handler not available")
	}

	// On daily runs (not first boot), skip if user is active.
	if t.firstRun.Load() && t.activity != nil {
		idleMs := time.Now().UnixMilli() - t.activity.LastActivityAt()
		idle := time.Duration(idleMs) * time.Millisecond
		if idle < 5*time.Minute {
			t.logger.Info("boot: skipped daily check, user active", "idle", idle.Round(time.Second))
			return nil
		}
	}

	// Read BOOT.md if it exists.
	prompt := t.resolveBootPrompt()

	isFirstBoot := !t.firstRun.Load()
	t.firstRun.Store(true)

	runCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	result, err := t.chatHandler.SendSync(runCtx, "boot", prompt, "", &chat.SyncOptions{
		ToolPreset:       string(toolpreset.PresetBoot),
		MaxHistoryTokens: 30_000,
	})
	if err != nil {
		return fmt.Errorf("boot: agent turn failed: %w", err)
	}

	kind := "daily-check"
	if isFirstBoot {
		kind = "first-boot"
	}
	t.logger.Info("boot task completed",
		"kind", kind,
		"output_len", len(result.Text),
	)
	return nil
}

// resolveBootPrompt reads ~/.deneb/BOOT.md if it exists, otherwise returns
// the default boot prompt.
func (t *bootTask) resolveBootPrompt() string {
	if t.homeDir == "" {
		return defaultBootPrompt
	}

	bootPath := filepath.Join(t.homeDir, ".deneb", "BOOT.md")
	data, err := os.ReadFile(bootPath)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultBootPrompt
		}
		t.logger.Warn("boot: failed to read BOOT.md, using default", "error", err)
		return defaultBootPrompt
	}

	content := string(data)
	if content == "" {
		return defaultBootPrompt
	}

	// Prepend system context so the LLM knows this is a boot turn.
	return fmt.Sprintf("[시스템 부트 — 게이트웨이 시작됨]\n\n아래는 BOOT.md의 내용입니다. 지시에 따라 수행하세요:\n\n%s", content)
}

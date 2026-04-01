package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
)

// ProgressTracker edits a single Telegram message in-place to show
// real-time tool execution status during agent runs.
type ProgressTracker struct {
	client    *Client
	chatID    int64
	messageID int64 // 0 until first message sent
	steps     []ProgressStep
	mu        sync.Mutex
}

// ProgressStep records a single tool invocation and its current status.
type ProgressStep struct {
	Tool   string
	Status string // "running", "done", "error"
}

// toolNameKorean maps tool names to Korean labels for vibe coder display.
var toolNameKorean = map[string]string{
	"exec":      "명령어 실행",
	"write":     "파일 작성",
	"edit":      "파일 수정",
	"read":      "파일 읽기",
	"grep":      "코드 검색",
	"glob":      "파일 검색",
	"ls":        "디렉토리 확인",
	"send_file": "파일 전송",
}

// NewProgressTracker creates a tracker bound to a specific Telegram chat.
func NewProgressTracker(client *Client, chatID int64) *ProgressTracker {
	return &ProgressTracker{
		client: client,
		chatID: chatID,
	}
}

// OnToolStart records a new tool execution and sends or edits the progress message.
func (pt *ProgressTracker) OnToolStart(ctx context.Context, name string) {
	pt.mu.Lock()
	pt.steps = append(pt.steps, ProgressStep{Tool: name, Status: "running"})
	pt.mu.Unlock()

	pt.updateMessage(ctx)
}

// OnToolComplete marks a tool step as done or errored and updates the message.
func (pt *ProgressTracker) OnToolComplete(ctx context.Context, name string, isError bool) {
	pt.mu.Lock()
	// Find the last running step with this tool name (in case of duplicate calls).
	for i := len(pt.steps) - 1; i >= 0; i-- {
		if pt.steps[i].Tool == name && pt.steps[i].Status == "running" {
			if isError {
				pt.steps[i].Status = "error"
			} else {
				pt.steps[i].Status = "done"
			}
			break
		}
	}
	pt.mu.Unlock()

	pt.updateMessage(ctx)
}

// Finalize marks all remaining running steps as done and performs a final update.
func (pt *ProgressTracker) Finalize(ctx context.Context) {
	pt.mu.Lock()
	anyChanged := false
	for i := range pt.steps {
		if pt.steps[i].Status == "running" {
			pt.steps[i].Status = "done"
			anyChanged = true
		}
	}
	pt.mu.Unlock()

	if anyChanged {
		pt.updateMessage(ctx)
	}
}

// updateMessage sends a new progress message or edits the existing one.
func (pt *ProgressTracker) updateMessage(ctx context.Context) {
	text := pt.renderText()

	pt.mu.Lock()
	msgID := pt.messageID
	pt.mu.Unlock()

	if msgID == 0 {
		// First message: send a new one.
		results, err := SendText(ctx, pt.client, pt.chatID, text, SendOptions{
			DisableLinkPreview: true,
			DisableNotification: true,
		})
		if err != nil || len(results) == 0 {
			return
		}
		pt.mu.Lock()
		pt.messageID = results[0].MessageID
		pt.mu.Unlock()
		return
	}

	// Subsequent updates: edit the existing message.
	if _, err := EditMessageText(ctx, pt.client, pt.chatID, msgID, text, "", nil); err != nil {
		slog.Warn("failed to edit progress message", "msgId", msgID, "error", err)
	}
}

// renderText builds the plain-text progress display from current steps.
func (pt *ProgressTracker) renderText() string {
	pt.mu.Lock()
	steps := make([]ProgressStep, len(pt.steps))
	copy(steps, pt.steps)
	pt.mu.Unlock()

	var b strings.Builder
	b.WriteString("🔧 도구 실행 중...\n")

	for _, s := range steps {
		var icon string
		switch s.Status {
		case "running":
			icon = "⏳"
		case "done":
			icon = "✅"
		case "error":
			icon = "❌"
		}

		label := toolNameKorean[s.Tool]
		if label == "" {
			label = s.Tool
		}

		fmt.Fprintf(&b, "\n%s %s", icon, label)
	}

	return b.String()
}

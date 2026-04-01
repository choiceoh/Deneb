package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

const (
	// statusLineInterval is the number of completed tools between status summary lines.
	statusLineInterval = 4

	// statusSummaryTimeout is the maximum time to wait for a summary LLM call.
	statusSummaryTimeout = 10 * time.Second

	// maxReasonLen caps each thinking-text snippet sent to the summarizer.
	maxReasonLen = 300

	// maxSummaryLen caps the rendered summary line in runes.
	maxSummaryLen = 30
)

// SummarizeFn calls a local LLM to summarize recent agent activity into a
// short Korean phrase. The input is a slice of recent thinking-text snippets.
type SummarizeFn func(ctx context.Context, reasons []string) (string, error)

// ProgressTracker edits a single Telegram message in-place to show
// real-time tool execution status during agent runs.
type ProgressTracker struct {
	client    *Client
	chatID    int64
	messageID int64 // 0 until first message sent
	steps     []ProgressStep
	mu        sync.Mutex

	// Status summary support: periodically summarize what the agent is doing
	// via a local LLM call and insert the result into the progress message.
	completedCount int
	reasons        []string         // accumulated thinking texts from OnToolStart
	statusInserts  map[int]string   // stepIndex -> summary phrase to render after that step
	summarizeFn    SummarizeFn      // injected; nil = no summaries
	pendingSummary atomic.Bool      // prevents overlapping LLM calls
}

// ProgressStep records a single tool invocation and its current status.
type ProgressStep struct {
	Tool   string
	Status string // "running", "done", "error"
}

// toolNameKorean maps tool names to Korean labels for vibe coder display.
var toolNameKorean = map[string]string{
	"exec":            "명령어 실행",
	"write":           "파일 작성",
	"edit":            "파일 수정",
	"multi_edit":      "일괄 수정",
	"read":            "파일 읽기",
	"batch_read":      "일괄 읽기",
	"grep":            "코드 검색",
	"find":            "파일 검색",
	"search_and_read": "코드 검색+읽기",
	"tree":            "디렉토리 구조",
	"analyze":         "코드 분석",
	"inspect":         "코드 상세 분석",
	"diff":            "변경사항 비교",
	"test":            "테스트 실행",
	"git":             "Git 작업",
	"ls":              "디렉토리 확인",
	"send_file":       "파일 전송",
	"web":             "웹 검색",
	"memory":          "메모리 검색",
	"pilot":           "AI 분석",
	"image":           "이미지 분석",
	"gmail":           "이메일",
	"message":         "메시지 전송",
	"continue_run":    "자동 연속 실행",
}

// NewProgressTracker creates a tracker bound to a specific Telegram chat.
// summarizeFn is optional; when non-nil, the tracker periodically calls it to
// generate Korean status summaries from accumulated thinking text.
func NewProgressTracker(client *Client, chatID int64, summarizeFn SummarizeFn) *ProgressTracker {
	return &ProgressTracker{
		client:      client,
		chatID:      chatID,
		summarizeFn: summarizeFn,
	}
}

// OnToolStart records a new tool execution and sends or edits the progress message.
// reason is the LLM's thinking text explaining why this tool is being called;
// it may be empty for models that don't produce thinking blocks.
func (pt *ProgressTracker) OnToolStart(ctx context.Context, name, reason string) {
	pt.mu.Lock()
	pt.steps = append(pt.steps, ProgressStep{Tool: name, Status: "running"})
	if reason != "" {
		// Truncate long thinking text to avoid sending huge prompts to summarizer.
		if len([]rune(reason)) > maxReasonLen {
			reason = string([]rune(reason)[:maxReasonLen])
		}
		pt.reasons = append(pt.reasons, reason)
	}
	pt.mu.Unlock()

	pt.updateMessage(ctx)
}

// OnToolComplete marks a tool step as done or errored and updates the message.
// Every statusLineInterval completions, it asynchronously calls the summarizer
// to generate a Korean status line from accumulated thinking text.
func (pt *ProgressTracker) OnToolComplete(ctx context.Context, name string, isError bool) {
	pt.mu.Lock()
	var completedIdx int
	for i := len(pt.steps) - 1; i >= 0; i-- {
		if pt.steps[i].Tool == name && pt.steps[i].Status == "running" {
			if isError {
				pt.steps[i].Status = "error"
			} else {
				pt.steps[i].Status = "done"
			}
			completedIdx = i
			break
		}
	}

	pt.completedCount++
	shouldSummarize := pt.summarizeFn != nil &&
		pt.completedCount >= statusLineInterval &&
		pt.completedCount%statusLineInterval == 0 &&
		len(pt.reasons) > 0

	var reasonsCopy []string
	insertIdx := completedIdx
	if shouldSummarize {
		// Copy and reset accumulated reasons for this batch.
		reasonsCopy = make([]string, len(pt.reasons))
		copy(reasonsCopy, pt.reasons)
		pt.reasons = pt.reasons[:0]
	}
	pt.mu.Unlock()

	pt.updateMessage(ctx)

	if shouldSummarize && pt.pendingSummary.CompareAndSwap(false, true) {
		go pt.runSummary(insertIdx, reasonsCopy)
	}
}

// runSummary calls the summarizer in a background goroutine and inserts the
// result into statusInserts for the next updateMessage call.
func (pt *ProgressTracker) runSummary(insertIdx int, reasons []string) {
	defer pt.pendingSummary.Store(false)

	sCtx, cancel := context.WithTimeout(context.Background(), statusSummaryTimeout)
	defer cancel()

	summary, err := pt.summarizeFn(sCtx, reasons)
	if err != nil {
		slog.Debug("progress summary failed", "error", err)
		return
	}
	summary = sanitizeSummary(summary)
	if summary == "" {
		return
	}

	pt.mu.Lock()
	if pt.statusInserts == nil {
		pt.statusInserts = make(map[int]string)
	}
	pt.statusInserts[insertIdx] = summary
	pt.mu.Unlock()

	pt.updateMessage(sCtx)
}

// sanitizeSummary cleans up LLM output to a short, single-line Korean phrase.
func sanitizeSummary(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	// Strip reasoning model artifacts (<think>...</think>, "Thinking Process:", etc.).
	raw = jsonutil.StripThinkingTags(raw)
	raw = jsonutil.StripThinkingPreamble(raw)
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	// Take only the first line.
	if idx := strings.IndexByte(raw, '\n'); idx >= 0 {
		raw = raw[:idx]
	}
	// Strip surrounding quotes.
	raw = strings.Trim(raw, "\"'`\u201C\u201D\u2018\u2019")
	// Strip leading emoji/bullet.
	raw = strings.TrimLeft(raw, "💭🔧⏳✅❌-•·* ")
	raw = strings.TrimSpace(raw)

	// Truncate to maxSummaryLen runes.
	runes := []rune(raw)
	if len(runes) > maxSummaryLen {
		raw = string(runes[:maxSummaryLen])
	}
	return raw
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
	if pt.client == nil {
		return
	}

	text := pt.renderText()

	pt.mu.Lock()
	msgID := pt.messageID
	pt.mu.Unlock()

	if msgID == 0 {
		// First message: send a new one.
		results, err := SendText(ctx, pt.client, pt.chatID, text, SendOptions{
			DisableLinkPreview:  true,
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

// renderText builds the plain-text progress display from current steps,
// interleaving status summary lines at the appropriate positions.
func (pt *ProgressTracker) renderText() string {
	pt.mu.Lock()
	steps := make([]ProgressStep, len(pt.steps))
	copy(steps, pt.steps)
	var inserts map[int]string
	if len(pt.statusInserts) > 0 {
		inserts = make(map[int]string, len(pt.statusInserts))
		for k, v := range pt.statusInserts {
			inserts[k] = v
		}
	}
	pt.mu.Unlock()

	var b strings.Builder
	b.WriteString("🔧 도구 실행 중...\n")

	for i, s := range steps {
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

		if inserts != nil {
			if phrase, ok := inserts[i]; ok {
				fmt.Fprintf(&b, "\n💭 %s", phrase)
			}
		}
	}

	return b.String()
}

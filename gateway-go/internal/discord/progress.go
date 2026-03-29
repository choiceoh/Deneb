// Package discord — ProgressTracker edits a single Discord message in-place
// to show real-time agent execution progress (tool start/complete steps).
//
// Throttles edits to ≤1 per 2 seconds to stay within Discord rate limits.
package discord

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

const (
	// progressEditThrottle is the minimum interval between message edits.
	progressEditThrottle = 2 * time.Second
)

// toolNameKorean maps raw English tool names to Korean labels for vibe coders.
// The progress tracker shows these instead of cryptic tool names.
var toolNameKorean = map[string]string{
	"exec":        "명령어 실행",
	"read":        "파일 읽기",
	"write":       "파일 작성",
	"edit":        "파일 수정",
	"grep":        "코드 검색",
	"find":        "파일 찾기",
	"ls":          "폴더 탐색",
	"web":         "웹 검색",
	"web_search":  "웹 검색",
	"web_fetch":   "웹 페이지 가져오기",
	"send_file":   "파일 전송",
	"http":        "API 호출",
	"kv":          "데이터 저장",
	"clipboard":   "클립보드",
	"process":     "프로세스 관리",
	"search_code": "코드 검색",
	"glob":        "파일 패턴 검색",
	"multi_edit":  "다중 파일 수정",
	"tree":        "프로젝트 구조",
	"diff":        "변경 비교",
	"analyze":     "코드 분석",
	"test":        "테스트 실행",
	"git":         "Git 작업",
	"pilot":       "Pilot 분석",
	"subagents":   "서브에이전트",
	"vega":        "프로젝트 검색",
	"polaris":     "시스템 문서",
	"image":       "이미지 분석",
	"gmail":       "Gmail",
	"cron":        "예약 작업",
	"memory_search": "메모리 검색",
	"health_check":  "상태 점검",
}

// KoreanToolName returns a Korean label for a tool name.
// Falls back to the original name if no translation exists.
func KoreanToolName(name string) string {
	if kr, ok := toolNameKorean[name]; ok {
		return kr
	}
	return name
}

// parallelGroupWindow is the maximum time between StartStep calls that are
// considered part of the same parallel batch. The executor fires all tool
// goroutines from the same loop iteration — they call OnToolStart within
// microseconds of each other, so 50ms is generous.
const parallelGroupWindow = 50 * time.Millisecond

// ProgressTracker manages a single Discord message that is edited to reflect
// agent execution progress. Each tool execution becomes a step with a status.
type ProgressTracker struct {
	client     *Client
	channelID  string
	messageID  string // the progress message being edited
	summarizer *Summarizer // optional; summarizes thinking blocks for step reasons

	mu          sync.Mutex
	steps       []ProgressStep
	lastEdit    time.Time
	dirty       bool // true if steps changed since last edit
	finalized   bool
	nextGroup   int       // incremented for each new parallel batch
	groupWindow time.Time // timestamp of first StartStep in current batch
	startTime   time.Time // when the tracker was created (for total elapsed)
}

// NewProgressTracker sends an initial progress message and returns a tracker.
// Returns nil if the message cannot be sent.
func NewProgressTracker(ctx context.Context, client *Client, channelID string) *ProgressTracker {
	msg, err := client.SendMessage(ctx, channelID, &SendMessageRequest{
		Embeds: []Embed{{
			Title:       "⏳ 처리 중...",
			Description: "에이전트가 작업을 시작합니다.",
			Color:       ColorProgress,
		}},
		AllowedMentions: &AllowedMentions{Parse: []string{}},
	})
	if err != nil {
		slog.Warn("progress tracker: failed to send initial embed", "channel", channelID, "error", err)
		return nil
	}
	if msg == nil {
		slog.Warn("progress tracker: initial embed returned nil message", "channel", channelID)
		return nil
	}

	return &ProgressTracker{
		client:    client,
		channelID: channelID,
		messageID: msg.ID,
		startTime: time.Now(),
	}
}

// SetSummarizer attaches a summarizer for async thinking summaries.
func (pt *ProgressTracker) SetSummarizer(rs *Summarizer) {
	if pt == nil {
		return
	}
	pt.summarizer = rs
}

// AddStep adds a new pending step. Does not trigger an edit.
// Tool names are automatically translated to Korean for vibe coders.
func (pt *ProgressTracker) AddStep(name string) {
	if pt == nil {
		return
	}
	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.steps = append(pt.steps, ProgressStep{Name: KoreanToolName(name), Status: StepPending})
	pt.dirty = true
}

// StartStep marks a step as running. Triggers a throttled edit.
// Tool names are automatically translated to Korean for vibe coders.
// rawThinking is the raw thinking block text from the LLM (may be empty).
// If a Summarizer is attached, it asynchronously generates a brief
// summary and updates the step once ready.
// Tools that start within parallelGroupWindow of each other are assigned the
// same Group ID so the progress embed can visually group them.
func (pt *ProgressTracker) StartStep(ctx context.Context, name, rawThinking string) {
	if pt == nil {
		return
	}
	kr := KoreanToolName(name)
	pt.mu.Lock()

	// Assign parallel group based on timing.
	now := time.Now()
	if pt.groupWindow.IsZero() || now.Sub(pt.groupWindow) > parallelGroupWindow {
		pt.nextGroup++
		pt.groupWindow = now
	}
	group := pt.nextGroup

	var stepIdx int
	found := false
	for i := range pt.steps {
		if pt.steps[i].Name == kr && pt.steps[i].Status == StepPending {
			pt.steps[i].Status = StepRunning
			pt.steps[i].Group = group
			pt.steps[i].StartedAt = now
			stepIdx = i
			found = true
			break
		}
	}
	if !found {
		stepIdx = len(pt.steps)
		pt.steps = append(pt.steps, ProgressStep{Name: kr, Status: StepRunning, Group: group, StartedAt: now})
	}
	needsSummary := rawThinking != "" && pt.summarizer != nil
	pt.dirty = true
	pt.mu.Unlock()

	pt.tryEdit(ctx)

	// Fire async LLM summarization if we have raw thinking and a summarizer.
	if needsSummary {
		go pt.summarizeAndUpdate(ctx, stepIdx, rawThinking)
	}
}

// summarizeAndUpdate calls the lightweight LLM to summarize thinking text,
// then updates the step's reason and triggers an embed edit. If the tracker
// has already been finalized, it sends a direct edit to update the final
// embed with the late-arriving summary.
func (pt *ProgressTracker) summarizeAndUpdate(ctx context.Context, stepIdx int, rawThinking string) {
	summary := pt.summarizer.ReasoningSummary(ctx, rawThinking)
	if summary == "" {
		return
	}

	pt.mu.Lock()
	if stepIdx >= len(pt.steps) || pt.steps[stepIdx].Reason != "" {
		pt.mu.Unlock()
		return
	}
	pt.steps[stepIdx].Reason = summary
	wasFinalized := pt.finalized
	if !wasFinalized {
		pt.dirty = true
	}
	steps := make([]ProgressStep, len(pt.steps))
	copy(steps, pt.steps)
	pt.mu.Unlock()

	if wasFinalized {
		// Embed already finalized — send a direct edit with the updated summary.
		embed := FormatProgressEmbed(steps, pt.startTime)
		pt.client.EditMessage(ctx, pt.channelID, pt.messageID, &EditMessageRequest{
			Embeds: []Embed{embed},
		})
	} else {
		pt.tryEdit(ctx)
	}
}

// CompleteStep marks a step as done. Triggers a throttled edit.
// Tool names are automatically translated to Korean for vibe coders.
func (pt *ProgressTracker) CompleteStep(ctx context.Context, name string, isError bool) {
	pt.CompleteStepWithResult(ctx, name, isError, "")
}

// CompleteStepWithResult marks a step as done with an optional error result preview.
// If errorResult is non-empty and the step has no existing reason, it's used as the
// reason text so vibe coders can see a brief error preview in the progress embed.
func (pt *ProgressTracker) CompleteStepWithResult(ctx context.Context, name string, isError bool, errorResult string) {
	if pt == nil {
		return
	}
	kr := KoreanToolName(name)
	pt.mu.Lock()

	status := StepDone
	if isError {
		status = StepError
	}

	for i := range pt.steps {
		if pt.steps[i].Name == kr && (pt.steps[i].Status == StepRunning || pt.steps[i].Status == StepPending) {
			pt.steps[i].Status = status
			if !pt.steps[i].StartedAt.IsZero() {
				pt.steps[i].Duration = time.Since(pt.steps[i].StartedAt)
			}
			// Show error preview if no reason exists yet.
			if isError && errorResult != "" && pt.steps[i].Reason == "" {
				pt.steps[i].Reason = errorResult
			}
			break
		}
	}
	pt.dirty = true
	pt.mu.Unlock()

	pt.tryEdit(ctx)
}

// Finalize sends the final edit marking the progress as complete.
func (pt *ProgressTracker) Finalize(ctx context.Context) {
	if pt == nil {
		return
	}
	pt.mu.Lock()
	if pt.finalized {
		pt.mu.Unlock()
		return
	}
	pt.finalized = true

	// Mark any remaining running steps as done.
	for i := range pt.steps {
		if pt.steps[i].Status == StepRunning || pt.steps[i].Status == StepPending {
			pt.steps[i].Status = StepDone
		}
	}
	steps := make([]ProgressStep, len(pt.steps))
	copy(steps, pt.steps)
	pt.mu.Unlock()

	embed := FormatProgressEmbed(steps, pt.startTime)
	pt.client.EditMessage(ctx, pt.channelID, pt.messageID, &EditMessageRequest{
		Embeds: []Embed{embed},
	})
}

// MessageID returns the tracked message ID.
func (pt *ProgressTracker) MessageID() string {
	if pt == nil {
		return ""
	}
	return pt.messageID
}

// tryEdit edits the progress message if the throttle period has elapsed.
func (pt *ProgressTracker) tryEdit(ctx context.Context) {
	pt.mu.Lock()
	if !pt.dirty || pt.finalized {
		pt.mu.Unlock()
		return
	}
	if time.Since(pt.lastEdit) < progressEditThrottle {
		pt.mu.Unlock()
		return
	}

	steps := make([]ProgressStep, len(pt.steps))
	copy(steps, pt.steps)
	pt.lastEdit = time.Now()
	pt.dirty = false
	pt.mu.Unlock()

	embed := FormatProgressEmbed(steps, pt.startTime)
	pt.client.EditMessage(ctx, pt.channelID, pt.messageID, &EditMessageRequest{
		Embeds: []Embed{embed},
	})
}

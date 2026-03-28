package chat

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// TaskProgress tracks the live progress of a running agent task.
// Updated by the task goroutine, read by concurrent response runs.
// All methods are thread-safe.
type TaskProgress struct {
	mu          sync.RWMutex
	sessionKey  string
	runID       string
	userMessage string // original user message that started the task
	startedAt   time.Time
	turnCount   int
	toolLog     []ToolLogEntry
	lastText    strings.Builder // last LLM text output (rolling buffer)
	currentTool string          // currently executing tool ("" if none)
}

// ToolLogEntry records a single tool invocation in the task progress.
type ToolLogEntry struct {
	Name    string
	Input   string // truncated
	Output  string // truncated
	Started time.Time
	Done    bool
}

// NewTaskProgress creates a new progress tracker for a task run.
func NewTaskProgress(sessionKey, runID, userMessage string) *TaskProgress {
	return &TaskProgress{
		sessionKey:  sessionKey,
		runID:       runID,
		userMessage: userMessage,
		startedAt:   time.Now(),
	}
}

// IsRunning returns true if this tracker represents an active task.
func (tp *TaskProgress) IsRunning() bool {
	return tp != nil
}

// SetCurrentTool records the tool currently being executed.
func (tp *TaskProgress) SetCurrentTool(name string) {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	tp.currentTool = name
}

// AppendToolLog adds a new tool invocation to the log.
func (tp *TaskProgress) AppendToolLog(name, input string) {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	tp.toolLog = append(tp.toolLog, ToolLogEntry{
		Name:    name,
		Input:   truncateStr(input, 200),
		Started: time.Now(),
	})
	tp.currentTool = name
}

// FinishTool marks the most recent tool with the given name as done.
func (tp *TaskProgress) FinishTool(name, output string) {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	// Walk backwards to find the latest matching entry.
	for i := len(tp.toolLog) - 1; i >= 0; i-- {
		if tp.toolLog[i].Name == name && !tp.toolLog[i].Done {
			tp.toolLog[i].Output = truncateStr(output, 200)
			tp.toolLog[i].Done = true
			break
		}
	}
	if tp.currentTool == name {
		tp.currentTool = ""
	}
}

// AppendText appends streaming text output from the LLM.
// Keeps only the last ~800 characters.
func (tp *TaskProgress) AppendText(text string) {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	tp.lastText.WriteString(text)
	if tp.lastText.Len() > 1200 {
		// Trim to last 800 chars.
		s := tp.lastText.String()
		tp.lastText.Reset()
		tp.lastText.WriteString(s[len(s)-800:])
	}
}

// IncrementTurn advances the turn counter.
func (tp *TaskProgress) IncrementTurn() {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	tp.turnCount++
}

// FormatContextBlock returns a human-readable summary of the task progress
// for injection into the response run's system prompt.
func (tp *TaskProgress) FormatContextBlock() string {
	tp.mu.RLock()
	defer tp.mu.RUnlock()

	elapsed := time.Since(tp.startedAt).Truncate(time.Second)
	var b strings.Builder

	b.WriteString("## 현재 백그라운드 작업 진행 중\n\n")
	fmt.Fprintf(&b, "원래 요청: %q\n", truncateStr(tp.userMessage, 300))
	fmt.Fprintf(&b, "시작: %s 전 | 턴: %d/25\n", formatDuration(elapsed), tp.turnCount)

	if len(tp.toolLog) > 0 {
		b.WriteString("\n실행한 도구:\n")
		// Show last 8 tools to keep context manageable.
		start := 0
		if len(tp.toolLog) > 8 {
			start = len(tp.toolLog) - 8
			fmt.Fprintf(&b, "  ... (%d개 도구 생략)\n", start)
		}
		for i := start; i < len(tp.toolLog); i++ {
			entry := tp.toolLog[i]
			status := "✓"
			if !entry.Done {
				status = "⏳ 진행 중"
			}
			fmt.Fprintf(&b, "  %d. %s(%s) → %s", i+1, entry.Name, entry.Input, status)
			if entry.Done && entry.Output != "" {
				fmt.Fprintf(&b, " [%s]", entry.Output)
			}
			b.WriteByte('\n')
		}
	}

	if tp.currentTool != "" {
		fmt.Fprintf(&b, "\n현재 실행 중: %s\n", tp.currentTool)
	}

	if lastText := tp.lastText.String(); lastText != "" {
		b.WriteString("\n마지막 출력:\n")
		b.WriteString(truncateStr(lastText, 500))
		b.WriteByte('\n')
	}

	b.WriteString("\n이 작업은 백그라운드에서 계속 진행 중입니다.\n")
	return b.String()
}

// truncateStr truncates s to maxLen, appending "…" if truncated.
func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "…"
}

// formatDuration formats a duration in Korean-friendly "Xm Ys" style.
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%d초", int(d.Seconds()))
	}
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	if s == 0 {
		return fmt.Sprintf("%d분", m)
	}
	return fmt.Sprintf("%d분 %d초", m, s)
}

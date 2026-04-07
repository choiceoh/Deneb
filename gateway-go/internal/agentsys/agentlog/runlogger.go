package agentlog

import (
	"encoding/json"
	"log/slog"
	"time"
)

const maxMessageLen = 4096

// RunLogger is a convenience wrapper for logging a single agent run.
// All methods are nil-safe: if the RunLogger or its Writer is nil, calls are no-ops.
type RunLogger struct {
	w       *Writer
	session string
	runID   string
	start   time.Time
}

// NewRunLogger creates a RunLogger for a specific run.
// Returns nil if w is nil (all methods are nil-safe).
func NewRunLogger(w *Writer, session, runID string) *RunLogger {
	if w == nil {
		return nil
	}
	return &RunLogger{
		w:       w,
		session: session,
		runID:   runID,
		start:   time.Now(),
	}
}

// LogStart records agent run start.
func (rl *RunLogger) LogStart(data RunStartData) {
	if rl == nil {
		return
	}
	if len(data.Message) > maxMessageLen {
		data.Message = data.Message[:maxMessageLen] + "..."
	}
	rl.emit(TypeRunStart, data)
}

// LogPrep records context preparation completion.
func (rl *RunLogger) LogPrep(data RunPrepData) {
	if rl == nil {
		return
	}
	rl.emit(TypeRunPrep, data)
}

// LogTurnLLM records an LLM turn completion.
func (rl *RunLogger) LogTurnLLM(data TurnLLMData) {
	if rl == nil {
		return
	}
	rl.emit(TypeTurnLLM, data)
}

// LogTurnTool records a tool execution completion.
func (rl *RunLogger) LogTurnTool(data TurnToolData) {
	if rl == nil {
		return
	}
	rl.emit(TypeTurnTool, data)
}

// LogEnd records agent run completion.
func (rl *RunLogger) LogEnd(data RunEndData) {
	if rl == nil {
		return
	}
	data.TotalMs = time.Since(rl.start).Milliseconds()
	rl.emit(TypeRunEnd, data)
}

// LogError records agent run failure.
func (rl *RunLogger) LogError(data RunErrorData) {
	if rl == nil {
		return
	}
	rl.emit(TypeRunError, data)
}

func (rl *RunLogger) emit(entryType string, data any) {
	raw, err := json.Marshal(data)
	if err != nil {
		slog.Warn("agentlog: marshal failed", "type", entryType, "run", rl.runID, "err", err)
		return
	}
	if err := rl.w.Append(LogEntry{
		Ts:      time.Now().UnixMilli(),
		Type:    entryType,
		RunID:   rl.runID,
		Session: rl.session,
		Data:    raw,
	}); err != nil {
		slog.Warn("agentlog: append failed", "type", entryType, "run", rl.runID, "err", err)
	}
}

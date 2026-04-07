package rlm

import (
	"sync"
	"time"
)

// IterationTrace captures detailed data for a single RLM loop iteration.
type IterationTrace struct {
	Iter         int       `json:"iter"`
	StartedAt    time.Time `json:"started_at"`
	LLMElapsed   int64     `json:"llm_elapsed_ms"`   // LLM call duration
	ExecElapsed  int64     `json:"exec_elapsed_ms"`  // total REPL execution duration
	TotalElapsed int64     `json:"total_elapsed_ms"` // entire iteration duration
	ResponseLen  int       `json:"response_len"`     // LLM response text length
	CodeBlocks   int       `json:"code_blocks"`      // number of code blocks extracted
	TokensIn     int       `json:"tokens_in"`        // estimated input tokens
	TokensOut    int       `json:"tokens_out"`       // estimated output tokens
	HasError     bool      `json:"has_error"`        // any REPL error in this iteration
	HasFinal     bool      `json:"has_final"`        // FINAL() detected
	Compacted    bool      `json:"compacted"`        // compaction occurred this iteration
	ExecOutputs  []string  `json:"exec_outputs"`     // REPL stdout per block (truncated)
	ExecErrors   []string  `json:"exec_errors"`      // REPL errors per block (empty if none)
	CodeSnippets []string  `json:"code_snippets"`    // first 200 chars of each code block
}

// Trace is the complete observation record for one RLM loop execution.
type Trace struct {
	ID          string           `json:"id"` // unique trace ID (timestamp-based)
	StartedAt   time.Time        `json:"started_at"`
	FinishedAt  time.Time        `json:"finished_at"`
	ElapsedMS   int64            `json:"elapsed_ms"`
	UserPrompt  string           `json:"user_prompt"` // first 500 chars
	Model       string           `json:"model"`
	StopReason  string           `json:"stop_reason"`
	Iterations  int              `json:"iterations"`
	TotalIn     int              `json:"total_tokens_in"`
	TotalOut    int              `json:"total_tokens_out"`
	Compactions int              `json:"compactions"`
	Errors      int              `json:"errors"`
	FinalLen    int              `json:"final_answer_len"`
	Steps       []IterationTrace `json:"steps"`
}

// TraceSummary is a compact view for listing recent traces.
type TraceSummary struct {
	ID         string `json:"id"`
	StartedAt  string `json:"started_at"` // RFC3339
	ElapsedMS  int64  `json:"elapsed_ms"`
	Model      string `json:"model"`
	Prompt     string `json:"prompt"` // first 80 chars
	StopReason string `json:"stop_reason"`
	Iterations int    `json:"iterations"`
	Errors     int    `json:"errors"`
	FinalLen   int    `json:"final_answer_len"`
}

// Summary returns a compact summary of the trace.
func (t *Trace) Summary() TraceSummary {
	prompt := t.UserPrompt
	if len(prompt) > 80 {
		prompt = prompt[:80] + "..."
	}
	return TraceSummary{
		ID:         t.ID,
		StartedAt:  t.StartedAt.Format(time.RFC3339),
		ElapsedMS:  t.ElapsedMS,
		Model:      t.Model,
		Prompt:     prompt,
		StopReason: t.StopReason,
		Iterations: t.Iterations,
		Errors:     t.Errors,
		FinalLen:   t.FinalLen,
	}
}

// truncate returns s truncated to maxLen with "..." suffix if needed.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// ── AgentTrace (root LLM + worker LLM) ─────────────────────────────────────

// AgentTrace captures a single LLM agent run (root or worker).
type AgentTrace struct {
	ID         string    `json:"id"`
	Kind       string    `json:"kind"`                  // "root" or "worker"
	SessionKey string    `json:"session_key,omitempty"` // root only
	ParentID   string    `json:"parent_id,omitempty"`   // worker: root run's client_run_id
	StartedAt  time.Time `json:"started_at"`
	ElapsedMS  int64     `json:"elapsed_ms"`
	Model      string    `json:"model"`
	Prompt     string    `json:"prompt,omitempty"` // first 200 chars (worker only)
	StopReason string    `json:"stop_reason"`
	Turns      int       `json:"turns"`
	TokensIn   int       `json:"tokens_in"`
	TokensOut  int       `json:"tokens_out"`
	ToolCalls  int       `json:"tool_calls"`
	Tools      []string  `json:"tools,omitempty"` // unique tool names used
	Error      string    `json:"error,omitempty"`
}

// AgentTraceSummary is a compact view for listing recent agent traces.
type AgentTraceSummary struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	SessionKey string `json:"session_key,omitempty"`
	StartedAt  string `json:"started_at"` // RFC3339
	ElapsedMS  int64  `json:"elapsed_ms"`
	Model      string `json:"model"`
	StopReason string `json:"stop_reason"`
	Turns      int    `json:"turns"`
	TokensIn   int    `json:"tokens_in"`
	TokensOut  int    `json:"tokens_out"`
	ToolCalls  int    `json:"tool_calls"`
	Error      string `json:"error,omitempty"`
}

// Summary returns a compact summary of the agent trace.
func (t *AgentTrace) Summary() AgentTraceSummary {
	return AgentTraceSummary{
		ID:         t.ID,
		Kind:       t.Kind,
		SessionKey: t.SessionKey,
		StartedAt:  t.StartedAt.Format(time.RFC3339),
		ElapsedMS:  t.ElapsedMS,
		Model:      t.Model,
		StopReason: t.StopReason,
		Turns:      t.Turns,
		TokensIn:   t.TokensIn,
		TokensOut:  t.TokensOut,
		ToolCalls:  t.ToolCalls,
		Error:      t.Error,
	}
}

// AgentTraceStore keeps recent agent traces in a ring buffer.
// Safe for concurrent use.
type AgentTraceStore struct {
	mu    sync.Mutex
	buf   []AgentTrace
	head  int
	count int
	cap   int
}

// NewAgentTraceStore creates a store that retains up to cap agent traces.
func NewAgentTraceStore(cap int) *AgentTraceStore {
	if cap <= 0 {
		cap = 50
	}
	return &AgentTraceStore{
		buf: make([]AgentTrace, cap),
		cap: cap,
	}
}

// Add stores a completed agent trace.
func (s *AgentTraceStore) Add(t AgentTrace) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf[s.head] = t
	s.head = (s.head + 1) % s.cap
	if s.count < s.cap {
		s.count++
	}
}

// Latest returns the most recent agent trace, or nil if empty.
func (s *AgentTraceStore) Latest() *AgentTrace {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.count == 0 {
		return nil
	}
	idx := (s.head - 1 + s.cap) % s.cap
	t := s.buf[idx]
	return &t
}

// Get returns the agent trace with the given ID, or nil.
func (s *AgentTraceStore) Get(id string) *AgentTrace {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := 0; i < s.count; i++ {
		idx := (s.head - 1 - i + s.cap*2) % s.cap
		if s.buf[idx].ID == id {
			t := s.buf[idx]
			return &t
		}
	}
	return nil
}

// List returns summaries of recent agent traces, newest first.
// kind filters by "root"/"worker"; empty string returns all.
func (s *AgentTraceStore) List(limit int, kind string) []AgentTraceSummary {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 {
		limit = 20
	}
	result := make([]AgentTraceSummary, 0, limit)
	for i := 0; i < s.count && len(result) < limit; i++ {
		idx := (s.head - 1 - i + s.cap*2) % s.cap
		t := &s.buf[idx]
		if kind != "" && t.Kind != kind {
			continue
		}
		result = append(result, t.Summary())
	}
	return result
}

// Count returns the number of stored agent traces.
func (s *AgentTraceStore) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.count
}

// ── TraceStore (RLM loop) ───────────────────────────────────────────────────

const defaultTraceCapacity = 20

// TraceStore keeps the most recent RLM traces in a ring buffer.
// Safe for concurrent use.
type TraceStore struct {
	mu    sync.Mutex
	buf   []Trace
	head  int // next write position
	count int
	cap   int
}

// NewTraceStore creates a store that retains up to cap traces.
func NewTraceStore(cap int) *TraceStore {
	if cap <= 0 {
		cap = defaultTraceCapacity
	}
	return &TraceStore{
		buf: make([]Trace, cap),
		cap: cap,
	}
}

// Add stores a completed trace, evicting the oldest if at capacity.
func (s *TraceStore) Add(t Trace) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf[s.head] = t
	s.head = (s.head + 1) % s.cap
	if s.count < s.cap {
		s.count++
	}
}

// Latest returns the most recent trace, or nil if empty.
func (s *TraceStore) Latest() *Trace {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.count == 0 {
		return nil
	}
	idx := (s.head - 1 + s.cap) % s.cap
	t := s.buf[idx]
	return &t
}

// Get returns the trace with the given ID, or nil if not found.
func (s *TraceStore) Get(id string) *Trace {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := 0; i < s.count; i++ {
		idx := (s.head - 1 - i + s.cap*2) % s.cap
		if s.buf[idx].ID == id {
			t := s.buf[idx]
			return &t
		}
	}
	return nil
}

// List returns summaries of recent traces, newest first.
func (s *TraceStore) List(limit int) []TraceSummary {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit <= 0 || limit > s.count {
		limit = s.count
	}
	result := make([]TraceSummary, 0, limit)
	for i := 0; i < limit; i++ {
		idx := (s.head - 1 - i + s.cap*2) % s.cap
		result = append(result, s.buf[idx].Summary())
	}
	return result
}

// Count returns the number of stored traces.
func (s *TraceStore) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.count
}

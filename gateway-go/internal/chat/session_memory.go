// session_memory.go — Structured session memory: tracks the current working
// state of a session so the agent can maintain continuity across turns and runs.
//
// Updated by a lightweight LLM call after each agent run. Injected into the
// system prompt as a [Session State] block so the agent knows what it was doing.
package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

// SessionMemory holds the structured working state of a single session.
type SessionMemory struct {
	Title     string   `json:"title"`               // Session topic (1-2 sentences)
	Current   string   `json:"current"`             // Current state ("3/10 files modified")
	TaskSpec  string   `json:"task_spec,omitempty"` // Original user request/goal
	Files     []string `json:"files,omitempty"`     // Files being worked on
	Functions []string `json:"functions,omitempty"` // Functions being worked on
	Workflow  []string `json:"workflow,omitempty"`  // Steps: completed / in-progress / pending
	Errors    []string `json:"errors,omitempty"`    // Errors + corrections
	Learnings []string `json:"learnings,omitempty"` // Lessons learned this session
	Results   []string `json:"results,omitempty"`   // Key outputs
	Worklog   []Entry  `json:"worklog,omitempty"`   // Chronological log
}

// Entry is a timestamped worklog item.
type Entry struct {
	Time string `json:"time"` // "HH:MM"
	Text string `json:"text"`
}

// Section length limits.
const (
	maxTitleRunes     = 100
	maxCurrentRunes   = 200
	maxTaskSpecRunes  = 500
	maxFiles          = 20
	maxFunctions      = 15
	maxWorkflow       = 10
	maxErrors         = 10
	maxLearnings      = 10
	maxResults        = 10
	maxWorklog        = 20
	maxEntryTextRunes = 200
)

// Trim enforces length limits on all sections (in-place).
func (m *SessionMemory) Trim() {
	m.Title = truncateRunes(m.Title, maxTitleRunes)
	m.Current = truncateRunes(m.Current, maxCurrentRunes)
	m.TaskSpec = truncateRunes(m.TaskSpec, maxTaskSpecRunes)
	m.Files = tailSlice(m.Files, maxFiles)
	m.Functions = tailSlice(m.Functions, maxFunctions)
	m.Workflow = tailSlice(m.Workflow, maxWorkflow)
	m.Errors = tailSlice(m.Errors, maxErrors)
	m.Learnings = tailSlice(m.Learnings, maxLearnings)
	m.Results = tailSlice(m.Results, maxResults)
	m.Worklog = tailSlice(m.Worklog, maxWorklog)
	for i := range m.Worklog {
		m.Worklog[i].Text = truncateRunes(m.Worklog[i].Text, maxEntryTextRunes)
	}
}

// IsEmpty returns true if the memory has no meaningful content.
func (m *SessionMemory) IsEmpty() bool {
	return m.Title == "" && m.Current == "" && m.TaskSpec == "" &&
		len(m.Files) == 0 && len(m.Functions) == 0 &&
		len(m.Workflow) == 0 && len(m.Errors) == 0 &&
		len(m.Learnings) == 0 && len(m.Results) == 0 &&
		len(m.Worklog) == 0
}

// FormatForPrompt renders the session memory as a compact text block for
// injection into the system prompt. Returns "" if the memory is empty.
func (m *SessionMemory) FormatForPrompt() string {
	if m.IsEmpty() {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Session State\n")
	if m.Title != "" {
		fmt.Fprintf(&b, "Title: %s\n", m.Title)
	}
	if m.Current != "" {
		fmt.Fprintf(&b, "Current: %s\n", m.Current)
	}
	if m.TaskSpec != "" {
		fmt.Fprintf(&b, "Task: %s\n", m.TaskSpec)
	}
	if len(m.Files) > 0 {
		fmt.Fprintf(&b, "Files: %s\n", strings.Join(m.Files, ", "))
	}
	if len(m.Functions) > 0 {
		fmt.Fprintf(&b, "Functions: %s\n", strings.Join(m.Functions, ", "))
	}
	if len(m.Workflow) > 0 {
		fmt.Fprintf(&b, "Workflow: %s\n", strings.Join(m.Workflow, " / "))
	}
	if len(m.Errors) > 0 {
		for i, e := range m.Errors {
			fmt.Fprintf(&b, "Error %d: %s\n", i+1, e)
		}
	}
	if len(m.Learnings) > 0 {
		for i, l := range m.Learnings {
			fmt.Fprintf(&b, "Learning %d: %s\n", i+1, l)
		}
	}
	if len(m.Results) > 0 {
		for i, r := range m.Results {
			fmt.Fprintf(&b, "Result %d: %s\n", i+1, r)
		}
	}
	if len(m.Worklog) > 0 {
		b.WriteString("Worklog:\n")
		for _, e := range m.Worklog {
			fmt.Fprintf(&b, "  [%s] %s\n", e.Time, e.Text)
		}
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// In-memory store (sessionKey → *SessionMemory)
// ---------------------------------------------------------------------------

// SessionMemoryStore is a thread-safe in-memory store for session memories.
type SessionMemoryStore struct {
	mu      sync.RWMutex
	entries map[string]*SessionMemory
	baseDir string // disk persistence directory
}

// NewSessionMemoryStore creates a store that persists to baseDir.
// If baseDir is empty, persistence is disabled (in-memory only).
func NewSessionMemoryStore(baseDir string) *SessionMemoryStore {
	return &SessionMemoryStore{
		entries: make(map[string]*SessionMemory),
		baseDir: baseDir,
	}
}

// Get returns the session memory for the given key, or nil.
func (s *SessionMemoryStore) Get(sessionKey string) *SessionMemory {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.entries[sessionKey]
}

// Set stores the session memory and persists to disk.
func (s *SessionMemoryStore) Set(sessionKey string, mem *SessionMemory) {
	s.mu.Lock()
	s.entries[sessionKey] = mem
	s.mu.Unlock()
	if s.baseDir != "" {
		go s.saveToDisk(sessionKey, mem)
	}
}

// Delete removes the session memory from store and disk.
func (s *SessionMemoryStore) Delete(sessionKey string) {
	s.mu.Lock()
	delete(s.entries, sessionKey)
	s.mu.Unlock()
	if s.baseDir != "" {
		path := s.diskPath(sessionKey)
		os.Remove(path) // best-effort
	}
}

func (s *SessionMemoryStore) diskPath(sessionKey string) string {
	safe := sanitizeSessionKey(sessionKey)
	return filepath.Join(s.baseDir, safe+".memory.json")
}

func (s *SessionMemoryStore) saveToDisk(sessionKey string, mem *SessionMemory) {
	path := s.diskPath(sessionKey)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	data, err := json.Marshal(mem)
	if err != nil {
		return
	}
	// Atomic write via temp file.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	os.Rename(tmp, path)
}

// LoadFromDisk loads all persisted session memories into the in-memory store.
func (s *SessionMemoryStore) LoadFromDisk() int {
	if s.baseDir == "" {
		return 0
	}
	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		return 0
	}
	loaded := 0
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".memory.json") {
			continue
		}
		sessionKey := strings.TrimSuffix(name, ".memory.json")
		// Restore colon from filesystem-safe encoding.
		sessionKey = unsanitizeSessionKey(sessionKey)

		data, err := os.ReadFile(filepath.Join(s.baseDir, name))
		if err != nil {
			continue
		}
		var mem SessionMemory
		if err := json.Unmarshal(data, &mem); err != nil {
			continue
		}
		if !mem.IsEmpty() {
			s.entries[sessionKey] = &mem
			loaded++
		}
	}
	return loaded
}

// sanitizeSessionKey makes a session key safe for use as a filename.
func sanitizeSessionKey(key string) string {
	return strings.ReplaceAll(key, ":", "_")
}

// unsanitizeSessionKey reverses sanitizeSessionKey.
func unsanitizeSessionKey(key string) string {
	return strings.ReplaceAll(key, "_", ":")
}

// ---------------------------------------------------------------------------
// LLM-based update
// ---------------------------------------------------------------------------

const (
	sessionMemoryUpdateTimeout = 10 * time.Second
	sessionMemoryMaxTokens     = 1024
)

// sessionMemoryUpdatePrompt is the prompt template for the lightweight LLM.
const sessionMemoryUpdatePrompt = `현재 세션 메모리:
%s

이번 실행 요약:
- 사용자 메시지: %s
- 에이전트 응답: %s
- 턴 수: %d
- 결과: %s

세션 메모리를 업데이트하여 JSON으로 반환하세요.
반환 형식: {"title":"...","current":"...","task_spec":"...","files":[],"functions":[],"workflow":[],"errors":[],"learnings":[],"results":[],"worklog":[{"time":"HH:MM","text":"..."}]}
변화가 없으면 null을 반환하세요.`

// UpdateSessionMemory calls the lightweight LLM to update session memory
// based on the latest run. Non-blocking: runs in its own goroutine.
func UpdateSessionMemory(
	ctx context.Context,
	store *SessionMemoryStore,
	sessionKey string,
	userMessage string,
	agentText string,
	turns int,
	stopReason string,
	logger *slog.Logger,
) {
	if store == nil {
		return
	}

	lwClient := getLightweightClient()
	if lwClient == nil {
		return
	}
	if !checkSglangHealth() {
		return
	}

	memCtx, cancel := context.WithTimeout(ctx, sessionMemoryUpdateTimeout)
	defer cancel()

	// Build the current memory representation.
	existing := store.Get(sessionKey)
	var existingJSON string
	if existing != nil && !existing.IsEmpty() {
		data, _ := json.Marshal(existing)
		existingJSON = string(data)
	} else {
		existingJSON = "없음 (첫 실행)"
	}

	prompt := fmt.Sprintf(sessionMemoryUpdatePrompt,
		existingJSON,
		truncateRunes(userMessage, 100),
		truncateRunes(agentText, 300),
		turns,
		stopReason,
	)

	resp, err := lwClient.CompleteOpenAI(memCtx, llm.ChatRequest{
		Model: getLightweightModel(),
		Messages: []llm.Message{
			llm.NewTextMessage("user", prompt),
		},
		MaxTokens: sessionMemoryMaxTokens,
	})
	if err != nil {
		logger.Debug("session memory update failed", "error", err)
		return
	}

	resp = strings.TrimSpace(resp)
	if resp == "" || resp == "null" {
		return
	}

	// Extract JSON from response (handle markdown code fences).
	resp = extractJSON(resp)

	var updated SessionMemory
	if err := json.Unmarshal([]byte(resp), &updated); err != nil {
		logger.Debug("session memory update: invalid JSON", "error", err, "response", truncateRunes(resp, 200))
		return
	}

	updated.Trim()
	store.Set(sessionKey, &updated)
	logger.Debug("session memory updated", "session", sessionKey, "title", updated.Title)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// truncateRunes truncates a string to maxRunes runes, appending "..." if truncated.
func truncateRunes(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}

// tailSlice keeps only the last n elements of a slice.
func tailSlice[T any](s []T, n int) []T {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// extractJSON strips markdown code fences from an LLM response to get raw JSON.
func extractJSON(s string) string {
	// Try to find ```json ... ``` block.
	if idx := strings.Index(s, "```json"); idx >= 0 {
		s = s[idx+7:]
		if end := strings.Index(s, "```"); end >= 0 {
			return strings.TrimSpace(s[:end])
		}
	}
	// Try ``` ... ``` block.
	if idx := strings.Index(s, "```"); idx >= 0 {
		s = s[idx+3:]
		if end := strings.Index(s, "```"); end >= 0 {
			return strings.TrimSpace(s[:end])
		}
	}
	return s
}

// session_memory.go — Structured session memory: tracks the agent's working
// state across runs within a session. Complements Aurora (past conversation
// summaries) by preserving the *current* task state — especially valuable
// after compaction when detailed context is lost.
//
// Design choices:
//   - All sections are plain strings (not arrays/structs). LLMs generate
//     freeform text far more reliably than nested JSON. Simpler parsing,
//     fewer failure modes.
//   - Sections are agent-generic (not coding-specific). Files/Functions are
//     handled by RunCache and proactive context; Learnings are handled by
//     Memory Store fact extraction. Session memory fills a different niche:
//     task continuity within a session.
//   - Updated per-run (not per-turn) to balance quality vs cost. One
//     lightweight LLM call per user message, sharing the memoryExtractSem.
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
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/chat/pilot"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

// SessionMemory holds the structured working state of a single session.
// All sections are plain strings — the lightweight LLM generates freeform
// text for each, not structured arrays. This is intentional: freeform text
// is more robust against LLM output variance and more flexible in content.
type SessionMemory struct {
	Summary     string `json:"summary"`                // What this session is about (1-2 sentences)
	State       string `json:"state"`                  // What just happened / what's next
	TaskContext string `json:"task_context,omitempty"` // Active goals, constraints, specs
	Progress    string `json:"progress,omitempty"`     // Completed and pending items
	Decisions   string `json:"decisions,omitempty"`    // Key choices made and rationale (survives compaction)
	Errors      string `json:"errors,omitempty"`       // Unresolved issues or recent corrections
	Worklog     string `json:"worklog,omitempty"`      // Chronological log of significant actions
}

// Section character limits. Each section is truncated independently.
// Total budget: ~5000 chars (~1500 tokens) — modest relative to the
// 100K token context budget.
const (
	smLimitSummary     = 300
	smLimitState       = 300
	smLimitTaskContext = 800
	smLimitProgress    = 600
	smLimitDecisions   = 600
	smLimitErrors      = 400
	smLimitWorklog     = 800
)

// Trim enforces character limits on all sections (in-place).
func (m *SessionMemory) Trim() {
	m.Summary = truncRunes(m.Summary, smLimitSummary)
	m.State = truncRunes(m.State, smLimitState)
	m.TaskContext = truncRunes(m.TaskContext, smLimitTaskContext)
	m.Progress = truncRunes(m.Progress, smLimitProgress)
	m.Decisions = truncRunes(m.Decisions, smLimitDecisions)
	m.Errors = truncRunes(m.Errors, smLimitErrors)
	m.Worklog = truncRunes(m.Worklog, smLimitWorklog)
}

// IsEmpty returns true when the memory has no meaningful content.
func (m *SessionMemory) IsEmpty() bool {
	return m.Summary == "" && m.State == "" && m.TaskContext == "" &&
		m.Progress == "" && m.Decisions == "" && m.Errors == "" &&
		m.Worklog == ""
}

// FormatForPrompt renders the session memory as a compact text block for
// the system prompt. Empty sections are omitted. Returns "" if all empty.
func (m *SessionMemory) FormatForPrompt() string {
	if m.IsEmpty() {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Session State\n")
	b.WriteString("이 세션의 구조화된 작업 상태입니다. 이전 대화에서 자동 생성되었습니다.\n\n")
	writeSection(&b, "요약", m.Summary)
	writeSection(&b, "현재 상태", m.State)
	writeSection(&b, "작업 컨텍스트", m.TaskContext)
	writeSection(&b, "진행 상황", m.Progress)
	writeSection(&b, "결정 사항", m.Decisions)
	writeSection(&b, "오류/문제", m.Errors)
	writeSection(&b, "작업 이력", m.Worklog)
	return b.String()
}

func writeSection(b *strings.Builder, label, content string) {
	if content == "" {
		return
	}
	fmt.Fprintf(b, "### %s\n%s\n\n", label, content)
}

// ---------------------------------------------------------------------------
// In-memory store (sessionKey → *SessionMemory)
// ---------------------------------------------------------------------------

// SessionMemoryStore is a thread-safe in-memory store backed by disk.
type SessionMemoryStore struct {
	mu      sync.RWMutex
	entries map[string]*SessionMemory
	baseDir string // empty = no disk persistence
}

// NewSessionMemoryStore creates a store. Pass empty baseDir for in-memory only.
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

// Set stores the session memory and persists to disk (async).
func (s *SessionMemoryStore) Set(sessionKey string, mem *SessionMemory) {
	s.mu.Lock()
	s.entries[sessionKey] = mem
	s.mu.Unlock()
	if s.baseDir != "" {
		go s.saveToDisk(sessionKey, mem)
	}
}

// Delete removes session memory from store and disk.
func (s *SessionMemoryStore) Delete(sessionKey string) {
	s.mu.Lock()
	delete(s.entries, sessionKey)
	s.mu.Unlock()
	if s.baseDir != "" {
		os.Remove(s.diskPath(sessionKey)) // best-effort
	}
}

func (s *SessionMemoryStore) diskPath(sessionKey string) string {
	return filepath.Join(s.baseDir, sanitizeKey(sessionKey)+".memory.json")
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
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	os.Rename(tmp, path)
}

// LoadFromDisk loads all persisted session memories into the in-memory store.
// Returns the number of entries loaded.
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
		key := unsanitizeKey(strings.TrimSuffix(name, ".memory.json"))
		data, err := os.ReadFile(filepath.Join(s.baseDir, name))
		if err != nil {
			continue
		}
		var mem SessionMemory
		if err := json.Unmarshal(data, &mem); err != nil {
			continue
		}
		if !mem.IsEmpty() {
			s.entries[key] = &mem
			loaded++
		}
	}
	return loaded
}

// sanitizeKey makes a session key safe for use as a filename.
func sanitizeKey(key string) string {
	return strings.ReplaceAll(key, ":", "_")
}

// unsanitizeKey reverses sanitizeKey.
func unsanitizeKey(key string) string {
	return strings.ReplaceAll(key, "_", ":")
}

// ---------------------------------------------------------------------------
// LLM-based update
// ---------------------------------------------------------------------------

const sessionMemoryUpdateTimeout = 20 * time.Second

// sessionMemorySystemPrompt instructs the lightweight LLM on how to update
// session memory. Korean because the primary interaction language is Korean.
const sessionMemorySystemPrompt = `당신은 AI 에이전트의 세션 메모리 관리자입니다.
대화 내용을 분석하여 세션 메모리를 업데이트하세요.

규칙:
- 모든 섹션은 한국어로 작성
- 간결하게 (각 섹션 2-5줄)
- 이전 메모리의 중요한 내용을 보존하되, 최신 정보로 갱신
- 해결된 에러는 errors에서 제거하고 해결 기록만 남기기
- worklog는 시간순 (최근 것만, 오래된 항목은 자연스럽게 탈락)
- 변화가 없으면 정확히 null 반환 (JSON 아닌 텍스트 null)

JSON 형식으로 반환:
{"summary":"...","state":"...","task_context":"...","progress":"...","decisions":"...","errors":"...","worklog":"..."}`

// UpdateSessionMemory calls the lightweight LLM to update session memory
// based on the latest run. Designed to be called inside the existing
// post-run goroutine alongside memory extraction (shares memoryExtractSem).
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
	lwClient := pilot.GetLightweightClient()
	if lwClient == nil {
		return
	}
	if !pilot.CheckSglangHealth() {
		return
	}

	memCtx, cancel := context.WithTimeout(ctx, sessionMemoryUpdateTimeout)
	defer cancel()

	// Build the user message for the update LLM call.
	existing := store.Get(sessionKey)
	var existingJSON string
	if existing != nil && !existing.IsEmpty() {
		data, _ := json.Marshal(existing)
		existingJSON = string(data)
	} else {
		existingJSON = "없음 (첫 실행)"
	}

	userPrompt := fmt.Sprintf(`현재 세션 메모리:
%s

이번 실행:
- 사용자: %s
- 에이전트: %s
- 턴: %d, 결과: %s

세션 메모리를 업데이트하세요.`,
		existingJSON,
		truncRunes(userMessage, 200),
		truncRunes(agentText, 1500),
		turns,
		stopReason,
	)

	resp, err := lwClient.Complete(memCtx, llm.ChatRequest{
		Model: pilot.GetLightweightModel(),
		Messages: []llm.Message{
			llm.NewTextMessage("system", sessionMemorySystemPrompt),
			llm.NewTextMessage("user", userPrompt),
		},
		MaxTokens: 1024,
	})
	if err != nil {
		logger.Debug("session memory update failed", "error", err)
		return
	}

	resp = strings.TrimSpace(resp)
	if resp == "" || resp == "null" {
		return
	}

	// Strip markdown code fences if present.
	resp = stripCodeFence(resp)

	var updated SessionMemory
	if err := json.Unmarshal([]byte(resp), &updated); err != nil {
		logger.Debug("session memory: invalid JSON from LLM",
			"error", err, "resp", truncRunes(resp, 200))
		return
	}

	updated.Trim()
	if updated.IsEmpty() {
		return
	}

	store.Set(sessionKey, &updated)
	logger.Debug("session memory updated",
		"session", sessionKey, "summary", truncRunes(updated.Summary, 60))
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// truncRunes truncates s to maxRunes runes, appending "…" if truncated.
func truncRunes(s string, maxRunes int) string {
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxRunes]) + "…"
}

// stripCodeFence removes ```json ... ``` or ``` ... ``` wrapping.
func stripCodeFence(s string) string {
	if idx := strings.Index(s, "```json"); idx >= 0 {
		s = s[idx+7:]
		if end := strings.Index(s, "```"); end >= 0 {
			return strings.TrimSpace(s[:end])
		}
	}
	if idx := strings.Index(s, "```"); idx >= 0 {
		s = s[idx+3:]
		if end := strings.Index(s, "```"); end >= 0 {
			return strings.TrimSpace(s[:end])
		}
	}
	return s
}

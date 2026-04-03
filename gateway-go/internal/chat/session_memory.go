// session_memory.go — Structured session memory: tracks the agent's working
// state across runs within a session. Complements Aurora (past conversation
// summaries) by preserving the *current* task state — especially valuable
// after compaction when detailed context is lost.
//
// Design choices:
//   - Markdown-based format (not JSON). LLMs generate freeform markdown far
//     more reliably than nested JSON, and the output is directly injectable
//     into system prompts without conversion.
//   - Claude Code–inspired forked-agent pattern: the local sglang model receives
//     the FULL recent transcript (not just truncated snippets) so it has complete
//     visibility into what happened — tool calls, errors, reasoning, etc.
//   - Updated per-run (not per-turn) to balance quality vs cost. One sglang
//     call per user message, routed through the centralized sglang hub.
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
	"github.com/choiceoh/deneb/gateway-go/internal/sglang"
)

// ---------------------------------------------------------------------------
// Session memory template — Claude Code–inspired markdown sections
// ---------------------------------------------------------------------------

// sessionMemoryTemplate is the initial template for new session memory files.
// Section headers and italic descriptions are structural — only the content
// below them is updated by the LLM.
const sessionMemoryTemplate = `# Session Title
_세션을 설명하는 5-10 단어의 구체적인 제목_

# Current State
_지금 진행 중인 작업, 아직 완료되지 않은 항목, 즉시 다음 단계_

# Task Specification
_사용자가 요청한 것, 설계 결정, 제약 조건 등 설명적 컨텍스트_

# Files and Functions
_중요 파일 목록, 각 파일의 역할, 왜 관련 있는지 간략히_

# Workflow
_보통 실행하는 명령어와 순서, 출력 해석 방법_

# Errors and Corrections
_발생한 에러와 해결 방법, 사용자가 수정한 것, 실패한 접근법 (다시 시도 금지)_

# Decisions
_내린 결정과 근거, 거부된 대안, 사용자가 확인한 선택_

# Worklog
_시간순으로 시도한 것, 한 것을 매우 간결하게. 도구 사용 포함 (어떤 파일을 읽고/수정했는지, 시도→실패→성공 흐름)_
`

// Session memory limits.
const (
	// maxSessionMemoryTokens is the hard cap for total session memory content.
	// 30K tokens (~120K chars) — generous budget since storage is local and
	// prompt injection is independently capped by per-section truncation.
	maxSessionMemoryTokens = 30_000

	// maxSectionTokens is the per-section soft cap (matches Claude Code's MAX_SECTION_LENGTH).
	maxSectionTokens = 2000

	// maxTranscriptMessages is the maximum number of recent transcript messages
	// sent to the sglang model for session memory extraction.
	maxTranscriptMessages = 30

	// maxTranscriptChars caps the formatted transcript text sent to sglang.
	// 32K chars ≈ 8K tokens — sufficient for recent context while preventing
	// long conversations from exhausting KV cache on the local model.
	maxTranscriptChars = 32_000

	// sessionMemoryUpdateTimeout is the max time for a single sglang call.
	// Increased from 20s to 60s because the model now receives full transcript.
	sessionMemoryUpdateTimeout = 60 * time.Second

	// sessionMemoryDebounce is the minimum interval between session memory updates.
	// If the previous update completed less than this ago, skip the current one.
	sessionMemoryDebounce = 30 * time.Second

	// trivialDeltaUserChars / trivialDeltaResponseChars define the threshold
	// below which a turn is considered too trivial to warrant a session memory
	// update (e.g., "응" + short acknowledgment).
	trivialDeltaUserChars     = 20
	trivialDeltaResponseChars = 100
)

// ---------------------------------------------------------------------------
// In-memory store (sessionKey → markdown string)
// ---------------------------------------------------------------------------

// SessionMemoryStore is a thread-safe in-memory store backed by disk.
// Stores session memory as markdown strings (not structured JSON).
type SessionMemoryStore struct {
	mu          sync.RWMutex
	entries     map[string]string
	lastTotal   map[string]int       // tracks transcript total at last update per session
	lastUpdated map[string]time.Time // tracks when last update completed per session
	baseDir     string               // empty = no disk persistence
}

// NewSessionMemoryStore creates a store. Pass empty baseDir for in-memory only.
func NewSessionMemoryStore(baseDir string) *SessionMemoryStore {
	return &SessionMemoryStore{
		entries:     make(map[string]string),
		lastTotal:   make(map[string]int),
		lastUpdated: make(map[string]time.Time),
		baseDir:     baseDir,
	}
}

// Get returns the session memory markdown for the given key, or "".
func (s *SessionMemoryStore) Get(sessionKey string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.entries[sessionKey]
}

// Set stores the session memory and persists to disk (async).
func (s *SessionMemoryStore) Set(sessionKey string, content string) {
	s.mu.Lock()
	s.entries[sessionKey] = content
	s.mu.Unlock()
	if s.baseDir != "" {
		go s.saveToDisk(sessionKey, content)
	}
}

// GetLastTotal returns the transcript message count at the last session memory update.
func (s *SessionMemoryStore) GetLastTotal(sessionKey string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastTotal[sessionKey]
}

// SetLastTotal records the transcript message count after a successful update.
func (s *SessionMemoryStore) SetLastTotal(sessionKey string, total int) {
	s.mu.Lock()
	s.lastTotal[sessionKey] = total
	s.lastUpdated[sessionKey] = time.Now()
	s.mu.Unlock()
}

// ShouldDebounce returns true if the last update was too recent to warrant another.
func (s *SessionMemoryStore) ShouldDebounce(sessionKey string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.lastUpdated[sessionKey]
	return ok && time.Since(t) < sessionMemoryDebounce
}

// Delete removes session memory from store and disk.
func (s *SessionMemoryStore) Delete(sessionKey string) {
	s.mu.Lock()
	delete(s.entries, sessionKey)
	delete(s.lastTotal, sessionKey)
	delete(s.lastUpdated, sessionKey)
	s.mu.Unlock()
	if s.baseDir != "" {
		os.Remove(s.diskPath(sessionKey)) // best-effort
	}
}

func (s *SessionMemoryStore) diskPath(sessionKey string) string {
	return filepath.Join(s.baseDir, sanitizeKey(sessionKey)+".memory.md")
}

func (s *SessionMemoryStore) saveToDisk(sessionKey string, content string) {
	path := s.diskPath(sessionKey)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
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
		if !strings.HasSuffix(name, ".memory.md") {
			continue
		}
		key := unsanitizeKey(strings.TrimSuffix(name, ".memory.md"))
		data, err := os.ReadFile(filepath.Join(s.baseDir, name))
		if err != nil {
			continue
		}
		content := strings.TrimSpace(string(data))
		if content != "" && !isTemplateOnly(content) {
			s.entries[key] = content
			loaded++
		}
	}
	return loaded
}

// FormatForPrompt returns the session memory content for system prompt injection.
// Wraps in a section header if non-empty. Returns "" if empty.
func FormatForPrompt(content string) string {
	if content == "" || isTemplateOnly(content) {
		return ""
	}
	// Truncate sections that are too long for prompt injection.
	truncated := truncateSessionMemoryForPrompt(content)
	var b strings.Builder
	b.WriteString("## Session State\n")
	b.WriteString("이 세션의 작업 상태입니다. 이전 대화에서 자동 생성되었습니다.\n\n")
	b.WriteString(truncated)
	return b.String()
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
// LLM-based update (forked-agent pattern via local sglang)
// ---------------------------------------------------------------------------

// sessionMemorySystemPrompt instructs the sglang model to update session memory.
// The model receives the full conversation transcript and current notes, then
// outputs the complete updated markdown.
const sessionMemorySystemPrompt = `당신은 AI 에이전트의 세션 메모리 관리자입니다.
대화 내용을 분석하여 세션 메모리 파일을 업데이트하세요.

중요: 이 메시지와 지시사항은 실제 사용자 대화의 일부가 아닙니다. "노트 작성"이나 이 업데이트 지시를 세션 메모리 내용에 포함하지 마세요.

규칙:
- 모든 섹션은 한국어로 작성
- 섹션 헤더(# 으로 시작하는 줄)와 이탤릭 설명(_으로 감싼 줄)은 절대 수정/삭제/추가하지 마세요
- 이탤릭 설명 아래의 실제 내용만 업데이트하세요
- 구체적이고 정보 밀도 높게 작성: 파일 경로, 함수명, 에러 메시지, 정확한 명령어 등 포함
- 각 섹션은 ~2000 토큰 이내로 유지. 한도에 가까워지면 덜 중요한 내용을 압축하되 핵심 정보는 보존
- "Current State"는 항상 최신 작업을 반영하도록 업데이트 (compaction 후 연속성에 중요)
- "Worklog"는 시간순으로 주요 작업 기록, 도구 사용 내역 포함 (어떤 파일을 읽고/수정했는지, 시도→실패→성공 흐름)
- 해결된 에러는 "Errors and Corrections"에서 제거하고 해결 기록만 남기기
- 변화가 없으면 정확히 NO_CHANGE 반환 (다른 텍스트 없이)
- CLAUDE.md에 이미 있는 정보는 포함하지 마세요

전체 업데이트된 마크다운을 반환하세요. 구조를 유지하면서 내용만 업데이트합니다.`

// UpdateSessionMemory calls the local sglang model with the full recent
// transcript to update the session memory markdown. Designed to be called
// inside the existing post-run goroutine alongside memory extraction.
//
// Unlike the previous implementation that received truncated snippets, this
// sends the full recent conversation so the model has complete visibility into
// tool calls, errors, file paths, and reasoning.
func UpdateSessionMemory(
	ctx context.Context,
	store *SessionMemoryStore,
	transcript TranscriptStore,
	sessionKey string,
	toolSummary string,
	logger *slog.Logger,
) {
	if store == nil {
		return
	}
	// Debounce: skip if last update was too recent.
	if store.ShouldDebounce(sessionKey) {
		logger.Debug("session memory: debounced (too recent)")
		return
	}
	if !pilot.CheckSglangHealth() {
		return
	}

	memCtx, cancel := context.WithTimeout(ctx, sessionMemoryUpdateTimeout)
	defer cancel()

	// Load current session memory (or template for first run).
	currentMemory := store.Get(sessionKey)
	if currentMemory == "" {
		currentMemory = sessionMemoryTemplate
	}

	// Load recent transcript messages — only new messages since last update.
	var transcriptText string
	var currentTotal int
	if transcript != nil {
		msgs, total, err := transcript.Load(sessionKey, maxTranscriptMessages)
		if err != nil {
			logger.Debug("session memory: failed to load transcript", "error", err)
		} else {
			currentTotal = total
			lastTotal := store.GetLastTotal(sessionKey)

			// Calculate how many messages are new since last update.
			newCount := total - lastTotal
			if newCount <= 0 {
				logger.Debug("session memory: no new messages since last update",
					"total", total, "lastTotal", lastTotal)
				return
			}

			// From the loaded messages (tail of transcript), take only the new ones.
			// If newCount > len(msgs), all loaded messages are new (plus some we can't see).
			if newCount < len(msgs) {
				msgs = msgs[len(msgs)-newCount:]
			}

			// Skip trivial deltas: short user message + short response
			// (e.g., "응" + "네, 알겠습니다") — nothing meaningful to update.
			if lastTotal > 0 && isTrivialDelta(msgs) {
				logger.Debug("session memory: trivial delta, skipping sglang call",
					"newMsgs", newCount)
				store.SetLastTotal(sessionKey, total)
				return
			}

			transcriptText = formatTranscriptForMemory(msgs)
			if lastTotal > 0 {
				logger.Debug("session memory: delta mode",
					"newMsgs", newCount, "totalMsgs", total,
					"deltaChars", len(transcriptText))
			}
		}
	}

	if transcriptText == "" {
		logger.Debug("session memory: no transcript available, skipping")
		return
	}

	// Truncate transcript to prevent KV cache exhaustion on the local model.
	// Keep the most recent portion (tail) since it's the most relevant.
	if len(transcriptText) > maxTranscriptChars {
		transcriptText = transcriptText[len(transcriptText)-maxTranscriptChars:]
		// Find the first complete message boundary to avoid mid-message truncation.
		if idx := strings.Index(transcriptText, "\n["); idx > 0 {
			transcriptText = "[... 이전 대화 생략 ...]\n" + transcriptText[idx+1:]
		}
	}

	// Build the user prompt with full context.
	var userPrompt strings.Builder
	userPrompt.WriteString("위의 새로운 대화 기록을 바탕으로 세션 메모리를 업데이트하세요.\n(이전 대화는 현재 세션 메모리에 이미 반영되어 있습니다.)\n\n")

	if toolSummary != "" {
		clean := strings.TrimPrefix(toolSummary, "[Tools used: ")
		clean = strings.TrimSuffix(clean, "]")
		fmt.Fprintf(&userPrompt, "이번 실행에서 사용한 도구: %s\n\n", clean)
	}

	fmt.Fprintf(&userPrompt, "현재 세션 메모리 내용:\n```\n%s\n```\n\n", currentMemory)
	userPrompt.WriteString("위 세션 메모리를 업데이트하여 전체 마크다운을 반환하세요.")

	// Build messages: conversation transcript as context + update instruction.
	// The transcript goes as prior messages so the model "sees" the full conversation,
	// then the update instruction is the final user message.
	messages := buildMemoryUpdateMessages(transcriptText, userPrompt.String())

	// Submit through the centralized sglang hub for token budget management
	// and zombie request prevention. The hub injects noThinking + server-side
	// timeout automatically.
	sHub := pilot.GetSglangHub()
	if sHub == nil {
		logger.Debug("session memory: sglang hub not available")
		return
	}

	hubResp, err := sHub.Submit(memCtx, sglang.Request{
		System:    sessionMemorySystemPrompt,
		Messages:  messages,
		MaxTokens: 2048,
		Priority:  sglang.PriorityNormal,
		CallerTag: "session_memory",
		NoCache:   true, // session memory is always unique
	})
	if err != nil {
		logger.Debug("session memory update failed", "error", err)
		return
	}
	resp := hubResp.Text

	resp = strings.TrimSpace(resp)
	if resp == "" || resp == "NO_CHANGE" {
		store.SetLastTotal(sessionKey, currentTotal)
		return
	}

	// Strip markdown code fences if the model wrapped the output.
	resp = stripCodeFence(resp)

	// Validate: must contain at least one section header.
	if !strings.Contains(resp, "# ") {
		logger.Debug("session memory: invalid output (no section headers)",
			"resp_len", len(resp))
		return
	}

	// Enforce total token budget.
	resp = enforceTokenBudget(resp, maxSessionMemoryTokens)

	store.Set(sessionKey, resp)
	store.SetLastTotal(sessionKey, currentTotal)
	// Extract title for log (first line after "# Session Title\n").
	title := extractTitle(resp)
	logger.Debug("session memory updated",
		"session", sessionKey, "title", truncRunes(title, 60),
		"deltaTotal", currentTotal)
}

// isTrivialDelta returns true when new messages are too short/simple to
// warrant an sglang call. Checks total content length across user and
// assistant messages — if both are tiny, nothing meaningful changed.
func isTrivialDelta(msgs []ChatMessage) bool {
	var userChars, respChars int
	for _, m := range msgs {
		n := utf8.RuneCount(m.Content)
		switch m.Role {
		case "user":
			userChars += n
		case "assistant":
			respChars += n
		}
	}
	return userChars < trivialDeltaUserChars && respChars < trivialDeltaResponseChars
}

// ---------------------------------------------------------------------------
// Transcript formatting
// ---------------------------------------------------------------------------

// formatTranscriptForMemory formats ChatMessages into a readable conversation
// format for the sglang model. Includes role labels, timestamps, and rich
// content blocks (tool_use, tool_result) when available.
func formatTranscriptForMemory(msgs []ChatMessage) string {
	if len(msgs) == 0 {
		return ""
	}
	var b strings.Builder
	for _, msg := range msgs {
		role := msg.Role
		if role == "" {
			role = "user"
		}
		if msg.Timestamp > 0 {
			t := time.UnixMilli(msg.Timestamp)
			fmt.Fprintf(&b, "[%s %s]\n", t.Format("15:04"), role)
		} else {
			fmt.Fprintf(&b, "[%s]\n", role)
		}
		b.WriteString(formatRichContent(msg.Content))
		b.WriteString("\n\n")
	}
	return b.String()
}

// formatRichContent converts a ChatMessage's json.RawMessage content into a
// human-readable string that preserves tool call structure. For text-only
// messages, returns the plain text. For rich messages (ContentBlock arrays),
// formats each block with type annotations so the session memory LLM can
// see the full action flow.
func formatRichContent(content json.RawMessage) string {
	if len(content) == 0 {
		return ""
	}

	// Try JSON string first (text-only, legacy format).
	var s string
	if json.Unmarshal(content, &s) == nil {
		return s
	}

	// Try ContentBlock array (rich format from Task 1).
	var blocks []json.RawMessage
	if json.Unmarshal(content, &blocks) != nil {
		return string(content)
	}

	var b strings.Builder
	for _, raw := range blocks {
		var base struct {
			Type      string          `json:"type"`
			Text      string          `json:"text,omitempty"`
			Name      string          `json:"name,omitempty"`
			Input     json.RawMessage `json:"input,omitempty"`
			ToolUseID string          `json:"tool_use_id,omitempty"`
			Content   string          `json:"content,omitempty"`
			IsError   bool            `json:"is_error,omitempty"`
		}
		if json.Unmarshal(raw, &base) != nil {
			continue
		}
		switch base.Type {
		case "text":
			if base.Text != "" {
				b.WriteString(base.Text)
				b.WriteByte('\n')
			}
		case "tool_use":
			inputSummary := truncRunes(string(base.Input), 200)
			fmt.Fprintf(&b, "[tool:%s(%s)]\n", base.Name, inputSummary)
		case "tool_result":
			output := truncRunes(base.Content, 500)
			if base.IsError {
				fmt.Fprintf(&b, "[result → ERROR: %s]\n", output)
			} else {
				fmt.Fprintf(&b, "[result → %s]\n", output)
			}
		default:
			// Skip thinking blocks and other internal types.
		}
	}
	return b.String()
}

// buildMemoryUpdateMessages constructs the message array for the sglang call.
// The transcript is sent as a user message (context), followed by the update
// instruction as another user message. This gives the model full visibility
// into the conversation while keeping the instruction separate.
func buildMemoryUpdateMessages(transcriptText, instruction string) []llm.Message {
	return []llm.Message{
		llm.NewTextMessage("user", "다음은 이 세션의 새로운 대화 기록입니다 (이전 대화는 세션 메모리에 이미 반영됨):\n\n"+transcriptText),
		llm.NewTextMessage("assistant", "새 대화 기록을 확인했습니다. 세션 메모리 업데이트 지시를 주세요."),
		llm.NewTextMessage("user", instruction),
	}
}

// ---------------------------------------------------------------------------
// Token budget enforcement
// ---------------------------------------------------------------------------

// enforceTokenBudget truncates oversized sections to fit within the total
// token budget. Uses rough estimation (chars/4 for token count).
func enforceTokenBudget(content string, maxTokens int) string {
	if roughTokenCount(content) <= maxTokens {
		return content
	}

	// Parse sections and truncate the largest ones.
	sections := parseSections(content)
	maxCharsPerSection := maxSectionTokens * 4 // rough char estimate

	var b strings.Builder
	for _, sec := range sections {
		b.WriteString(sec.header)
		b.WriteByte('\n')
		body := sec.body
		if len(body) > maxCharsPerSection {
			// Truncate at line boundary.
			lines := strings.Split(body, "\n")
			var kept strings.Builder
			for _, line := range lines {
				if kept.Len()+len(line)+1 > maxCharsPerSection {
					break
				}
				kept.WriteString(line)
				kept.WriteByte('\n')
			}
			kept.WriteString("[... 섹션이 길어서 잘림 ...]\n")
			body = kept.String()
		}
		b.WriteString(body)
	}
	return strings.TrimSpace(b.String())
}

type section struct {
	header string
	body   string
}

func parseSections(content string) []section {
	lines := strings.Split(content, "\n")
	var sections []section
	var current *section

	for _, line := range lines {
		if strings.HasPrefix(line, "# ") {
			if current != nil {
				sections = append(sections, *current)
			}
			current = &section{header: line}
		} else if current != nil {
			current.body += line + "\n"
		}
	}
	if current != nil {
		sections = append(sections, *current)
	}
	return sections
}

func roughTokenCount(s string) int {
	return len(s) / 4
}

// extractTitle returns the content after "# Session Title" header.
func extractTitle(content string) string {
	lines := strings.Split(content, "\n")
	inTitle := false
	for _, line := range lines {
		if strings.HasPrefix(line, "# Session Title") {
			inTitle = true
			continue
		}
		if inTitle {
			// Hit next section header — no title content found.
			if strings.HasPrefix(line, "# ") {
				break
			}
			trimmed := strings.TrimSpace(line)
			// Skip italic description lines.
			if strings.HasPrefix(trimmed, "_") && strings.HasSuffix(trimmed, "_") {
				continue
			}
			if trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// isTemplateOnly returns true if the content is just the empty template
// (all sections have only italic descriptions, no real content).
func isTemplateOnly(content string) bool {
	lines := strings.Split(strings.TrimSpace(content), "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// Skip section headers.
		if strings.HasPrefix(trimmed, "# ") {
			continue
		}
		// Skip italic description lines.
		if strings.HasPrefix(trimmed, "_") && strings.HasSuffix(trimmed, "_") {
			continue
		}
		// Found real content — not template-only.
		return false
	}
	return true
}

// truncRunes truncates s to maxRunes runes, appending "…" if truncated.
func truncRunes(s string, maxRunes int) string {
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxRunes]) + "…"
}

// stripCodeFence removes ```markdown ... ``` or ``` ... ``` wrapping.
func stripCodeFence(s string) string {
	for _, prefix := range []string{"```markdown", "```md", "```json", "```"} {
		if idx := strings.Index(s, prefix); idx >= 0 {
			inner := s[idx+len(prefix):]
			if end := strings.Index(inner, "```"); end >= 0 {
				return strings.TrimSpace(inner[:end])
			}
		}
	}
	return s
}

// truncateSessionMemoryForPrompt truncates each section to maxSectionTokens
// when injecting into the system prompt. Prevents oversized session memory
// from consuming the entire prompt budget.
func truncateSessionMemoryForPrompt(content string) string {
	sections := parseSections(content)
	maxChars := maxSectionTokens * 4

	var b strings.Builder
	for _, sec := range sections {
		b.WriteString(sec.header)
		b.WriteByte('\n')
		body := sec.body
		if len(body) > maxChars {
			lines := strings.Split(body, "\n")
			var kept strings.Builder
			for _, line := range lines {
				if kept.Len()+len(line)+1 > maxChars {
					break
				}
				kept.WriteString(line)
				kept.WriteByte('\n')
			}
			kept.WriteString("[... 잘림 ...]\n")
			body = kept.String()
		}
		b.WriteString(body)
	}
	return strings.TrimSpace(b.String())
}

package chatport

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// ChatMessage represents a message in a session transcript. Content remains
// raw JSON so the stable boundary supports both legacy string content and rich
// content-block arrays without depending on an LLM provider package.
type ChatMessage struct {
	Role        string           `json:"role"`
	Content     rawJSON          `json:"content,omitempty"`
	Attachments []ChatAttachment `json:"attachments,omitempty"`
	Timestamp   int64            `json:"timestamp,omitempty"`
	ParentID    string           `json:"parentId,omitempty"`
	ID          string           `json:"id,omitempty"`
}

// NewTextChatMessage creates a text-only transcript message.
func NewTextChatMessage(role, text string, ts int64) ChatMessage {
	return ChatMessage{
		Role:      role,
		Content:   MarshalJSONString(text),
		Timestamp: ts,
	}
}

// TextContent extracts plain text from legacy string content or rich text
// blocks. Unknown content stays observable as raw JSON.
func (m *ChatMessage) TextContent() string {
	if len(m.Content) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(m.Content, &text); err == nil {
		return text
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text,omitempty"`
	}
	if err := json.Unmarshal(m.Content, &blocks); err == nil && len(blocks) > 0 {
		var texts []string
		recognized := true
		for _, block := range blocks {
			if block.Type == "text" && block.Text != "" {
				texts = append(texts, block.Text)
				continue
			}
			if !knownNonTextBlock(block.Type) {
				recognized = false
			}
		}
		if len(texts) > 0 {
			return joinTexts(texts)
		}
		// Parsed cleanly and every block is a known non-text kind: there is
		// simply no text here. Falling through to the raw-JSON escape hatch
		// dumped the whole serialized block array — including a thinking
		// block's cryptographic `signature`, which is pure provider bookkeeping.
		// Measured 2026-08-26 from the puppet seat: the skill-relevance judge's
		// prompt was 6,114 chars of which 4,340 (71%) were one signature, and
		// the conversation it had to classify was truncated away to make room.
		// The same fallback feeds recall evidence notes, polaris rows, the
		// sub-agent last-text fallback, and transcript SEARCH — which then
		// matches inside base64.
		if recognized {
			return ""
		}
	}
	return string(m.Content)
}

// SearchableText renders a message for TEXT SEARCH, including the parts
// TextContent deliberately omits: a tool call's name and (capped) input, a tool
// result's (capped) content, and thinking prose.
//
// TextContent answers "what did this message SAY", which is what display,
// recall notes and sub-agent last-text want; search asks "does this message
// MENTION x", where a tool name is a legitimate hit. Before the raw-JSON escape
// hatch was narrowed, search matched tool names by accident — and also matched
// inside a thinking block's base64 signature, so a short query hit noise. This
// restores the signal without the noise.
//
// Mirrors the block rendering polaris already uses for its FTS index
// (pipeline/polaris/store.go): same caps, same shape, so the two search paths
// cannot disagree about what a transcript contains. The signature is never
// included — it is provider bookkeeping, not content.
func (m *ChatMessage) SearchableText() string {
	if len(m.Content) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(m.Content, &text); err == nil {
		return text
	}
	var blocks []struct {
		Type     string          `json:"type"`
		Text     string          `json:"text,omitempty"`
		Thinking string          `json:"thinking,omitempty"`
		Name     string          `json:"name,omitempty"`
		Input    json.RawMessage `json:"input,omitempty"`
		Content  string          `json:"content,omitempty"`
	}
	if err := json.Unmarshal(m.Content, &blocks); err != nil {
		return m.TextContent()
	}
	var parts []string
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if b.Text != "" {
				parts = append(parts, b.Text)
			}
		case "thinking":
			if b.Thinking != "" {
				parts = append(parts, b.Thinking)
			}
		case "tool_use":
			if b.Name != "" {
				parts = append(parts, "[도구 "+b.Name+"] "+truncateForSearch(string(b.Input), searchToolInputCap))
			}
		case "tool_result":
			if b.Content != "" {
				parts = append(parts, truncateForSearch(b.Content, searchToolResultCap))
			}
		}
	}
	if len(parts) == 0 {
		return m.TextContent()
	}
	return strings.Join(parts, "\n")
}

// Caps mirror polaris's FTS index so a raw stdout or file dump cannot bloat the
// scanned text (and, in polaris's case, the index) — the roomier result cap is
// the same asymmetry polaris documents.
const (
	searchToolInputCap  = 200
	searchToolResultCap = 2000
)

func truncateForSearch(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	end := maxBytes
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end]
}

// knownNonTextBlock reports whether a block type is one this codebase emits and
// that legitimately carries no transcript text. An UNKNOWN type still falls
// through to the raw-JSON escape hatch, which is what "unknown content stays
// observable" was always for.
func knownNonTextBlock(t string) bool {
	switch t {
	case "thinking", "redacted_thinking", "tool_use", "tool_result", "image", "document":
		return true
	}
	return false
}

// HasContent reports whether a message contains non-empty transcript content.
func (m *ChatMessage) HasContent() bool {
	return len(m.Content) > 0 && string(m.Content) != `""`
}

// MarshalJSONString returns s as a JSON-encoded string.
func MarshalJSONString(s string) json.RawMessage {
	data, _ := json.Marshal(s)
	return data
}

func joinTexts(texts []string) string {
	if len(texts) == 1 {
		return texts[0]
	}
	result := texts[0]
	for _, text := range texts[1:] {
		result += "\n\n" + text
	}
	return result
}

// ChatAttachment represents an attachment on a chat message.
type ChatAttachment struct {
	Type     string `json:"type"`
	MimeType string `json:"mimeType,omitempty"`
	URL      string `json:"url,omitempty"`
	Data     string `json:"data,omitempty"`
	Name     string `json:"name,omitempty"`
	Size     int64  `json:"size,omitempty"`
}

// SearchResult holds matching messages from one transcript.
type SearchResult struct {
	SessionKey string       `json:"sessionKey"`
	Matches    []MatchedMsg `json:"matches"`
}

// MatchedMsg is one matching message with surrounding context.
type MatchedMsg struct {
	Index   int           `json:"index"`
	Message ChatMessage   `json:"message"`
	Context []ChatMessage `json:"context"`
}

// TranscriptStore is the stable persistence and search boundary shared by
// chat, Polaris, and transcript implementations.
type TranscriptStore interface {
	Load(sessionKey string, limit int) ([]ChatMessage, int, error)
	Append(sessionKey string, msg ChatMessage) error
	Delete(sessionKey string) error
	ListKeys() ([]string, error)
	Search(query string, maxResults int) ([]SearchResult, error)
	CloneRecent(srcKey, dstKey string, limit int) error
}

// ToolResultReceipt is the durable completion record for one tool call. It is
// intentionally separate from the transcript: the executor still commits the
// canonical tool_result message as one ordered batch, while this receipt lets a
// restarted gateway recover calls that completed between those two writes.
type ToolResultReceipt struct {
	ToolUseID   string `json:"toolUseId"`
	ToolName    string `json:"toolName"`
	Content     string `json:"content"`
	IsError     bool   `json:"isError,omitempty"`
	CompletedAt int64  `json:"completedAt"`
}

// ToolResultReceiptStore is an optional crash-recovery capability attached to
// transcript stores. Receipts are ephemeral run state, not conversation
// history: callers delete them after the ordered tool_result batch is durable.
type ToolResultReceiptStore interface {
	AppendToolResultReceipt(sessionKey string, receipt ToolResultReceipt) error
	LoadToolResultReceipts(sessionKey string) ([]ToolResultReceipt, error)
	DeleteToolResultReceipts(sessionKey string) error
}

// ToolResultReceiptStoreProvider lets transcript decorators expose the receipt
// capability of their wrapped store without widening TranscriptStore itself.
type ToolResultReceiptStoreProvider interface {
	ToolResultReceiptStore() ToolResultReceiptStore
}

// ResolveToolResultReceiptStore unwraps an optional receipt capability from a
// transcript implementation or decorator.
func ResolveToolResultReceiptStore(store TranscriptStore) ToolResultReceiptStore {
	if store == nil {
		return nil
	}
	if provider, ok := store.(ToolResultReceiptStoreProvider); ok {
		return provider.ToolResultReceiptStore()
	}
	if receipts, ok := store.(ToolResultReceiptStore); ok {
		return receipts
	}
	return nil
}

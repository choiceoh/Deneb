package chatport

import "encoding/json"

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
	if err := json.Unmarshal(m.Content, &blocks); err == nil {
		var texts []string
		for _, block := range blocks {
			if block.Type == "text" && block.Text != "" {
				texts = append(texts, block.Text)
			}
		}
		if len(texts) > 0 {
			return joinTexts(texts)
		}
	}
	return string(m.Content)
}

// HasContent reports whether a message contains non-empty transcript content.
func (m *ChatMessage) HasContent() bool {
	return len(m.Content) > 0 && string(m.Content) != `""`
}

// MarshalJSONString returns s as a JSON-encoded string.
func MarshalJSONString(s string) rawJSON {
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

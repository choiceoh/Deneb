// Conversation label resolution — generates a display name for a conversation
// based on available context fields.
//
// Mirrors src/channels/conversation-label.ts.
package channel

import (
	"regexp"
	"strings"
)

var numericIDRegex = regexp.MustCompile(`^[0-9]+$`)

// ConversationLabelFields holds the context fields used for label resolution.
type ConversationLabelFields struct {
	ConversationLabel string
	ThreadLabel       string
	ChatType          string // "direct", "group", etc.
	SenderName        string
	From              string
	GroupChannel      string
	GroupSubject      string
	GroupSpace        string
}

// ResolveConversationLabel resolves a display label for a conversation.
// Priority: ConversationLabel > ThreadLabel > sender/group context.
func ResolveConversationLabel(f ConversationLabelFields) string {
	if explicit := strings.TrimSpace(f.ConversationLabel); explicit != "" {
		return explicit
	}
	if threadLabel := strings.TrimSpace(f.ThreadLabel); threadLabel != "" {
		return threadLabel
	}

	chatType := normalizeChatType(f.ChatType)
	if chatType == "direct" {
		if name := strings.TrimSpace(f.SenderName); name != "" {
			return name
		}
		if from := strings.TrimSpace(f.From); from != "" {
			return from
		}
		return ""
	}

	// Group context.
	base := firstNonEmpty(
		strings.TrimSpace(f.GroupChannel),
		strings.TrimSpace(f.GroupSubject),
		strings.TrimSpace(f.GroupSpace),
		strings.TrimSpace(f.From),
	)
	if base == "" {
		return ""
	}

	id := extractConversationID(f.From)
	if id == "" {
		return base
	}
	if !shouldAppendID(id) {
		return base
	}
	if base == id {
		return base
	}
	if strings.Contains(base, id) {
		return base
	}
	if strings.Contains(strings.ToLower(base), " id:") {
		return base
	}
	if strings.HasPrefix(base, "#") || strings.HasPrefix(base, "@") {
		return base
	}
	return base + " id:" + id
}

// extractConversationID extracts the last segment of a colon-separated from field.
func extractConversationID(from string) string {
	trimmed := strings.TrimSpace(from)
	if trimmed == "" {
		return ""
	}
	parts := strings.Split(trimmed, ":")
	var filtered []string
	for _, p := range parts {
		if p != "" {
			filtered = append(filtered, p)
		}
	}
	if len(filtered) == 0 {
		return trimmed
	}
	return filtered[len(filtered)-1]
}

// shouldAppendID determines if an ID suffix should be added.
func shouldAppendID(id string) bool {
	if numericIDRegex.MatchString(id) {
		return true
	}
	if strings.Contains(id, "@g.us") {
		return true
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

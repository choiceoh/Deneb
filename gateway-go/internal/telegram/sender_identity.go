// Sender identity validation — validates sender information in inbound messages.
//
// Mirrors src/channels/sender-identity.ts.
package telegram

import (
	"fmt"
	"regexp"
	"strings"
)

var e164Regex = regexp.MustCompile(`^\+\d{3,}$`)

// SenderIdentityFields holds the sender-related fields from a message context.
type SenderIdentityFields struct {
	ChatType       string // "direct", "group", "supergroup", "channel"
	SenderID       string
	SenderName     string
	SenderUsername string
	SenderE164     string // E.164 phone number format
}

// ValidateSenderIdentity checks sender identity fields for issues.
// Returns a list of issue descriptions (empty if valid).
func ValidateSenderIdentity(f SenderIdentityFields) []string {
	var issues []string

	isDirect := normalizeChatType(f.ChatType) == "direct"
	senderID := strings.TrimSpace(f.SenderID)
	senderName := strings.TrimSpace(f.SenderName)
	senderUsername := strings.TrimSpace(f.SenderUsername)
	senderE164 := strings.TrimSpace(f.SenderE164)

	if !isDirect {
		if senderID == "" && senderName == "" && senderUsername == "" && senderE164 == "" {
			issues = append(issues, "missing sender identity (SenderID/SenderName/SenderUsername/SenderE164)")
		}
	}

	if senderE164 != "" {
		if !e164Regex.MatchString(senderE164) {
			issues = append(issues, fmt.Sprintf("invalid SenderE164: %s", senderE164))
		}
	}

	if senderUsername != "" {
		if strings.Contains(senderUsername, "@") {
			issues = append(issues, fmt.Sprintf("SenderUsername should not include \"@\": %s", senderUsername))
		}
		if containsWhitespace(senderUsername) {
			issues = append(issues, fmt.Sprintf("SenderUsername should not include whitespace: %s", senderUsername))
		}
	}

	if f.SenderID != "" && senderID == "" {
		issues = append(issues, "SenderID is set but empty")
	}

	return issues
}

func normalizeChatType(raw string) string {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "direct", "private", "dm":
		return "direct"
	case "group", "supergroup", "channel":
		return strings.TrimSpace(strings.ToLower(raw))
	default:
		return raw
	}
}

func containsWhitespace(s string) bool {
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			return true
		}
	}
	return false
}

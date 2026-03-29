package telegram

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseChatID parses a Telegram chat ID from a raw target string.
//
// Supported forms:
//   - "123456789"
//   - "-1001234567890"
//   - "telegram:123456789"
//   - "te:123456789:job-name:1712345678901" (session-like keys)
func ParseChatID(raw string) (int64, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, fmt.Errorf("empty chat ID")
	}

	// Fast-path: plain numeric ID.
	if id, err := strconv.ParseInt(s, 10, 64); err == nil {
		return id, nil
	}

	// Session-like keys may include provider prefix + extra suffix segments.
	if strings.HasPrefix(s, "telegram:") {
		s = strings.TrimPrefix(s, "telegram:")
	} else if strings.HasPrefix(s, "te:") {
		s = strings.TrimPrefix(s, "te:")
	}

	head, _, _ := strings.Cut(s, ":")
	if id, err := strconv.ParseInt(head, 10, 64); err == nil {
		return id, nil
	}

	return 0, fmt.Errorf("invalid chat ID %q", raw)
}

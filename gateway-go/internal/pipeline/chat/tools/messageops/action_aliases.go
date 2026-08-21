package messageops

import "strings"

// normalizeMessageAction maps Korean/underscore aliases onto send|reply|react|thread-reply.
func normalizeMessageAction(raw string) string {
	a := strings.ToLower(strings.TrimSpace(raw))
	switch a {
	case "보내기", "전송":
		return "send"
	case "답장":
		return "reply"
	case "리액션", "반응":
		return "react"
	case "thread_reply", "threadreply":
		return "thread-reply"
	default:
		return a
	}
}

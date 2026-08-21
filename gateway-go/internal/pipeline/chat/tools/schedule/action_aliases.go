package schedule

import "strings"

// normalizeCronAction maps Korean/English aliases onto canonical cron actions.
func normalizeCronAction(raw string) string {
	a := strings.ToLower(strings.TrimSpace(raw))
	switch a {
	case "상태":
		return "status"
	case "목록":
		return "list"
	case "추가", "등록", "create":
		return "add"
	case "수정":
		return "update"
	case "삭제", "제거", "delete":
		return "remove"
	case "실행":
		return "run"
	case "조회":
		return "get"
	case "이력", "실행기록":
		return "runs"
	case "깨우기":
		return "wake"
	default:
		return a
	}
}

// normalizeCalendarAction maps Korean/underscore aliases onto canonical
// calendar actions. Empty stays empty so the existing list default still applies.
func normalizeCalendarAction(raw string) string {
	a := strings.ToLower(strings.TrimSpace(raw))
	switch a {
	case "목록":
		return "list"
	case "조회":
		return "get"
	case "생성", "추가":
		return "create"
	case "수정":
		return "update"
	case "삭제":
		return "delete"
	case "free-slots", "freeslots", "빈시간", "여유":
		return "free_slots"
	case "브리프", "요약":
		return "brief"
	case "준비":
		return "prep"
	case "캡처", "회의록":
		return "capture"
	case "감사", "점검":
		return "audit"
	case "타임라인":
		return "timeline"
	default:
		return a
	}
}

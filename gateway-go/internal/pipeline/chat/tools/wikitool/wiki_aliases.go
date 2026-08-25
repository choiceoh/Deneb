package wikitool

import "strings"

// normalizeWikiAction lowercases and maps Korean/underscore aliases onto the
// canonical wiki actions. Ambiguous words like "기록" are left unmapped.
func normalizeWikiAction(raw string) string {
	a := strings.ToLower(strings.TrimSpace(raw))
	switch a {
	case "검색", "찾기":
		return "search"
	case "읽기":
		return "read"
	case "목록", "목차", "색인":
		return "index"
	case "쓰기":
		return "write"
	case "로그":
		return "log"
	case "일기", "다이어리":
		return "daily"
	case "상태":
		return "status"
	case "닫기", "종료":
		return "close"
	case "재개", "다시열기":
		return "reopen"
	case "수집", "인제스트":
		return "ingest"
	default:
		return a
	}
}

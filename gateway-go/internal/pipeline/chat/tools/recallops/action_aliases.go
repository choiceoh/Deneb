package recallops

import "strings"

// normalizeKnowledgeOp maps action/op aliases onto recall|read|record.
func normalizeKnowledgeOp(raw string) string {
	a := strings.ToLower(strings.TrimSpace(raw))
	switch a {
	case "search", "검색", "찾기", "query":
		return "recall"
	case "읽기", "fetch":
		return "read"
	case "write", "쓰기", "저장":
		return "record"
	default:
		return a
	}
}

// normalizePolarisAction maps Korean/English aliases onto search|describe|expand.
func normalizePolarisAction(raw string) string {
	a := strings.ToLower(strings.TrimSpace(raw))
	switch a {
	case "검색", "찾기":
		return "search"
	case "설명", "요약":
		return "describe"
	case "펼치기", "원문", "복원":
		return "expand"
	default:
		return a
	}
}

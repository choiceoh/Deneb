package phoneevents

import (
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
)

// isElectronicApprovalEvent reports whether a phone notification is an Amaranth /
// Douzone electronic-approval signal. Detection prefers the structured payload
// the Android listener already formats ("종류: 전자결재"); Amaranth package/label
// sources are a secondary match when the body still carries approval keywords.
// Other apps that merely mention "결재" in a chat subject must NOT match — that
// is why package sniffing alone is not enough without an approval keyword.
func isElectronicApprovalEvent(source, text string) bool {
	text = strings.TrimSpace(text)
	if strings.Contains(text, "종류: 전자결재") {
		return true
	}
	if !isAmaranthSource(source) {
		return false
	}
	return hasApprovalKeyword(text)
}

func isAmaranthSource(source string) bool {
	s := strings.TrimSpace(source)
	if s == "" {
		return false
	}
	lower := strings.ToLower(s)
	switch {
	case lower == "com.douzone.bizbox.klago.app":
		return true
	case strings.Contains(lower, "amaranth"):
		return true
	case strings.Contains(s, "아마란스"):
		return true
	case strings.Contains(lower, "douzone"):
		return true
	default:
		return false
	}
}

func hasApprovalKeyword(text string) bool {
	for _, kw := range []string{"결재", "상신", "반려", "승인요청", "승인 요청"} {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

// electronicApprovalGuidance is injected for e-approval notifications. Prefer the
// browser-enriched body when present; never act on the approval itself — the work
// feed card carries 승인/반려 chips for the operator.
func electronicApprovalGuidance() string {
	return `이것은 전자결재 알림이다. 아래 [브라우저에서 읽은 결재 본문]이 있으면 그것을 근거로, ` +
		`없으면 알림 텍스트만으로 업무 피드용 요약을 한 메시지로 작성하라 ` +
		`(제목·기안자·금액/요지·결재선·마감·긴급도·다음에 할 일). ` +
		`본문의 행·열 표는 핵심 행과 합계의 표 구조를 보존하라. 첨부는 제목 목록만 선행 제공된다. ` +
		`판단에 꼭 필요한 첨부만 groupware action=attachment로 하나씩 골라 읽고, 모든 첨부를 자동으로 읽지 마라. ` +
		`결재를 대신 승인·반려·상신하지 마라 — 피드 카드의 승인/반려 칩으로만 처리한다. ` +
		`이미 처리 완료된 중복 상태 푸시처럼 알릴 가치가 전혀 없으면 다른 말 없이 %s 만 출력하라.`
}

const (
	approvalActionApprove = "approval:approve"
	approvalActionReject  = "approval:reject"
)

// extractGroupwareDocID pulls `id: 99178` from an enriched groupware read body.
func extractGroupwareDocID(body string) string {
	for _, line := range strings.Split(body, "\n") {
		s := strings.TrimSpace(line)
		if strings.HasPrefix(s, "문서ID:") {
			return strings.TrimSpace(strings.TrimPrefix(s, "문서ID:"))
		}
		if strings.HasPrefix(s, "문서ID=") {
			return strings.TrimSpace(strings.TrimPrefix(s, "문서ID="))
		}
		if strings.HasPrefix(s, "id:") {
			return strings.TrimSpace(strings.TrimPrefix(s, "id:"))
		}
		if idx := strings.Index(s, "id="); idx >= 0 {
			id := strings.TrimSpace(s[idx+len("id="):])
			if i := strings.IndexAny(id, " ·\t"); i > 0 {
				id = id[:i]
			}
			return id
		}
	}
	return ""
}

func groupwareApprovalFeedActions() []workfeed.Action {
	return []workfeed.Action{
		{ID: approvalActionApprove, Kind: workfeed.ActionAck, Label: "승인"},
		{ID: approvalActionReject, Kind: workfeed.ActionAck, Label: "반려"},
	}
}

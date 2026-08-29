// progress_label.go — gateway-owned Korean narration for chat run phases.
//
// One wording source for every surface that narrates a running turn: the
// owning stream's `progress` SSE frames (nativeapi), and the spectate plane's
// agent.event phase.changed payloads (a turn watched from another window).
// Lives in toolport for the same reason the result digests do — handlers and
// the chat pipeline both need it, and this leaf is their shared vocabulary.
package toolport

// ChatProgressLabel maps a machine phase to the user-facing status line.
// Unknown phases fall back to a generic preparing message so a new server
// phase never renders as a raw token.
func ChatProgressLabel(phase string) string {
	switch phase {
	case "accepted":
		return "요청을 받았습니다"
	case "preparing":
		return "대화 맥락을 준비하고 있습니다"
	case "recalling":
		return "관련 기억을 확인하고 있습니다"
	case "thinking":
		return "해결 방법을 검토하고 있습니다"
	case "working":
		return "도구로 필요한 내용을 확인하고 있습니다"
	case "reviewing":
		return "확인한 결과를 검토하고 있습니다"
	case "writing":
		return "답변을 작성하고 있습니다"
	case "wrapping_up":
		return "마무리 답변을 작성하고 있습니다"
	case "finalizing":
		return "답변을 정리하고 있습니다"
	default:
		return "응답을 준비하고 있습니다"
	}
}

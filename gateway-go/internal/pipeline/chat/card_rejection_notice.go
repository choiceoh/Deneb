package chat

import (
	"fmt"
	"strings"
	"sync"
)

// A rejected deneb-ui card is invisible to its author: the delivery boundary
// (denebui.NormalizeFinalReply) turns it into plain text, the user sees a
// flattened answer, and nothing in the conversation says a card was dropped or
// why. The model therefore re-emits the same broken card next turn. Observed
// 2026-08-25 in puppet mode: an <input> without the required id downgraded an
// entire card, `errors` came back empty, and the reason lived only in an
// operator log line.
//
// The notice below closes that loop. It rides the per-turn TAIL (run_tail_inject
// .go), never the system prompt — a rejection is turn-scoped and must not
// invalidate the APC prefix — and is consumed on read so one mistake produces
// exactly one correction hint.
var cardRejections sync.Map // sessionKey -> string

// recordCardRejection stores the correction hint for the next turn of this
// session. A later rejection overwrites an unread one: the newest failure is
// the one worth fixing.
func recordCardRejection(sessionKey, reason, issue string) {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return
	}
	detail := strings.TrimSpace(issue)
	if detail == "" {
		detail = reason
	}
	cardRejections.Store(sessionKey, fmt.Sprintf(
		"[직전 턴 알림] 네가 낸 deneb-ui 카드가 전달 직전에 거부돼 평문으로 바뀌어 나갔다: %s\n"+
			"같은 내용을 카드로 다시 낼 생각이면 이 문제부터 고쳐라. 못 고치겠으면 산문으로 답해라 — "+
			"거부된 카드는 사용자에게 납작한 텍스트로 보인다.", detail,
	))
}

// takeCardRejectionNotice returns and clears this session's pending hint.
func takeCardRejectionNotice(sessionKey string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return ""
	}
	if value, ok := cardRejections.LoadAndDelete(sessionKey); ok {
		if note, _ := value.(string); note != "" {
			return note
		}
	}
	return ""
}

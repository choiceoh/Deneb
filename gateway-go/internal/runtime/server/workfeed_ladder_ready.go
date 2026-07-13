package server

// Graduation-ladder READY card — the LadderWatchTask's operator surface. A
// ladder row whose evidence crossed its threshold is a standing decision the
// GRAD dashboard alone would leave unnoticed; the card brings it to the feed
// once per transition. Informational + ack (the actual flips are env knobs /
// allowlist edits done in a session, not chat actions).

import (
	"fmt"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
)

const ladderReadySource = "genesis-ladder"

// postLadderReadyCard surfaces one graduation-ladder row that just reached
// READY. Best-effort: a feed failure must never affect the watch cycle.
func (s *Server) postLadderReadyCard(title, detail string) {
	nf := s.nativeWorkFeedStore()
	if nf == nil {
		return
	}
	item := workfeed.Item{
		Source:  ladderReadySource,
		Title:   "졸업 사다리 준비됨: " + title,
		Summary: detail,
		Status:  "unread",
		Body: fmt.Sprintf(`자율성 졸업 사다리의 한 행이 증거 임계에 도달했습니다.

- 행: %s
- 증거: %s

잠금 해제는 자동으로 실행되지 않습니다 — 결정하시려면 채팅에서 지시하세요 (해당 노브/허용목록 변경은 세션 작업입니다). 상세 근거는 더보기 → 재귀적 자가개선의 '졸업 사다리' 카드에 있습니다.`, title, detail),
		Actions: []workfeed.Action{
			{ID: "ladder:ack", Kind: workfeed.ActionAck, Label: "확인"},
		},
	}
	if _, err := nf.Append(item); err != nil {
		s.logger.Warn("ladder-ready card post failed", "row", title, "error", err)
	}
}

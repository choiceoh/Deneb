package server

// Meta-evolution proposal card — RSI P2 propose-only adoption depends on the
// operator SEEING each weekly proposal; without this the .proposed file waits
// invisibly on disk.

import (
	"fmt"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
)

const metaProposalSource = "genesis-meta"

// postMetaProposalCard surfaces one slow-loop proposal in the work feed.
// Best-effort: a feed failure must never affect the meta-evolution cycle.
func (s *Server) postMetaProposalCard(artifact, epoch, reason, path string) {
	nf := s.nativeWorkFeedStore()
	if nf == nil {
		return
	}
	epochLabel := map[string]string{
		"producer":  "producer (evolve 프롬프트)",
		"evaluator": "evaluator (judge 프롬프트)",
	}[epoch]
	if epochLabel == "" {
		epochLabel = epoch
	}
	body := fmt.Sprintf(`주간 자가개선 슬로우 루프가 메타 아티팩트 개정을 제안했습니다 (게이트 통과, propose-only).

- 대상: %s
- Epoch: %s
- 사유: %s
- 제안 파일: %s

채택하려면 제안 파일을 검토한 뒤 .proposed 확장자를 제거해 원본 자리에 두면 됩니다. 채택하지 않으면 그대로 두거나 삭제하세요 — 다음 주 사이클은 이 결정을 원장에서 읽습니다.`,
		artifact, epochLabel, reason, path)
	if _, err := nf.Append(workfeed.Item{
		Source:  metaProposalSource,
		Title:   "메타 개정 제안: " + artifact,
		Summary: reason,
		Body:    body,
		RefType: "file",
		RefID:   path,
		Status:  "unread",
	}); err != nil {
		s.logger.Warn("meta proposal 카드 생성 실패", "artifact", artifact, "error", err)
	}
}

package server

// Meta-evolution proposal card — RSI P2 propose-only adoption depends on the
// operator SEEING each weekly proposal; without this the .proposed file waits
// invisibly on disk.

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
)

const (
	metaProposalSource       = "genesis-meta"
	metaProposalActionAdopt  = "meta:adopt"
	metaProposalActionReject = "meta:reject"
)

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

아래 버튼으로 채택하거나 기각하세요 — 결정은 메타 경험 원장에 기록되어 다음 사이클이 읽습니다.`,
		artifact, epochLabel, reason, path)
	if _, err := nf.Append(workfeed.Item{
		Source:   metaProposalSource,
		Title:    "메타 개정 제안: " + artifact,
		Summary:  reason,
		Body:     body,
		RefType:  "file",
		RefID:    path,
		Status:   "unread",
		Question: true, // render the decision chips inline
		Actions: []workfeed.Action{
			{ID: metaProposalActionAdopt, Kind: workfeed.ActionAck, Label: "채택"},
			{ID: metaProposalActionReject, Kind: workfeed.ActionAck, Label: "기각"},
		},
	}); err != nil {
		s.logger.Warn("meta proposal 카드 생성 실패", "artifact", artifact, "error", err)
	}
}

// handleMetaProposalAction applies the operator's feed-card decision: adopt
// promotes the .proposed into the live artifact, reject discards it. Either
// way the decision lands in the meta-experience ledger so the next weekly
// cycles read it. Best-effort — the card has already settled.
func (s *Server) handleMetaProposalAction(item workfeed.Item, actionID string) {
	if s.genesisMeta == nil || s.genesisTracker == nil {
		return
	}
	artifact := strings.TrimSuffix(filepath.Base(strings.TrimSpace(item.RefID)), ".proposed")
	if artifact == "" || artifact == "." {
		return
	}
	fallback := genesis.DefaultMetaArtifacts()[artifact]
	fromVersion := s.genesisMeta.Version(artifact, fallback)
	switch actionID {
	case metaProposalActionAdopt:
		toVersion, err := s.genesisMeta.AdoptProposal(artifact)
		if err != nil {
			s.logger.Warn("meta proposal 채택 실패", "artifact", artifact, "error", err)
			return
		}
		if err := s.genesisTracker.LogMetaRevision(genesis.MetaRevisionRecord{
			Artifact:    artifact,
			FromVersion: fromVersion,
			ToVersion:   toVersion,
			Action:      "adopted",
			Reason:      "operator adopted from feed card",
		}); err != nil {
			s.logger.Warn("meta adoption ledger write failed", "artifact", artifact, "error", err)
		}
		s.logger.Info("meta proposal adopted from feed card", "artifact", artifact, "from", fromVersion, "to", toVersion)
	case metaProposalActionReject:
		if err := s.genesisMeta.RejectProposal(artifact); err != nil {
			s.logger.Warn("meta proposal 기각 실패", "artifact", artifact, "error", err)
			return
		}
		if err := s.genesisTracker.LogMetaRevision(genesis.MetaRevisionRecord{
			Artifact:    artifact,
			FromVersion: fromVersion,
			Action:      "rejected",
			Reason:      "operator rejected from feed card",
		}); err != nil {
			s.logger.Warn("meta rejection ledger write failed", "artifact", artifact, "error", err)
		}
		s.logger.Info("meta proposal rejected from feed card", "artifact", artifact)
	}
}

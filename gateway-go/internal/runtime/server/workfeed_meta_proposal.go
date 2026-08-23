package server

// Meta-evolution proposal card — RSI P2 propose-only adoption depends on the
// operator SEEING each weekly proposal; without this the .proposed file waits
// invisibly on disk.

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/skilllifecycle"
)

const (
	metaProposalSource       = "genesis-meta"
	metaProposalActionAdopt  = "meta:adopt"
	metaProposalActionReject = "meta:reject"
	metaProposalActionRevert = "meta:revert"
	evolveVerdictSource      = "genesis-evolve-verdict"
	evolveVerdictConfirm     = "evolve-verdict:confirm"
	evolveVerdictRollback    = "evolve-verdict:rollback"
)

// postLowConfidenceEvolveCard turns a borderline-but-admissible judge decision
// into a real operator label. The evolve remains protected by the normal
// post-use rollback watch; this card only adds fast, explicit P3 feedback.
func (s *Server) postLowConfidenceEvolveCard(result genesis.EvolveResult) {
	if !result.Evolved || !result.NeedsOperatorVerdict || result.JudgeMargin == nil {
		return
	}
	nf := s.nativeWorkFeedStore()
	if nf == nil {
		return
	}
	margin := strconv.FormatFloat(*result.JudgeMargin, 'f', 1, 64)
	item := workfeed.Item{
		Source:  evolveVerdictSource,
		Title:   "저신뢰 스킬 개선 확인: " + result.SkillName,
		Summary: fmt.Sprintf("%s → %s · 판정 여유 %s점", result.SkillName, result.NewVersion, margin),
		Body: fmt.Sprintf(`스킬 개선이 모든 자동 게이트를 통과해 적용됐지만 판정 점수 차이가 작습니다.

- 스킬: %s
- 적용 버전: %s
- 판정 여유: %s점

의도한 개선이면 확정하고, 실제로 부적절하면 즉시 이전 버전으로 되돌리세요. 결정은 판정자 공진화(P3)의 실제 라벨로 축적됩니다.`,
			result.SkillName, result.NewVersion, margin),
		RefType: "skill-evolve-verdict",
		RefID:   result.SkillName,
		Metadata: map[string]string{
			"decisionId":   result.SkillName + "@" + result.NewVersion,
			"skill":        result.SkillName,
			"version":      result.NewVersion,
			"judgeVersion": result.JudgeVersion,
			"judgeMargin":  margin,
		},
		Question: true,
		Actions: []workfeed.Action{
			{ID: evolveVerdictConfirm, Kind: workfeed.ActionAck, Label: "개선 확정"},
			{ID: evolveVerdictRollback, Kind: workfeed.ActionAck, Label: "되돌리기"},
		},
		Status: workfeed.StatusUnread,
	}
	if _, err := nf.Append(item); err != nil {
		s.logger.Warn("low-confidence evolve 카드 생성 실패", "skill", result.SkillName, "error", err)
	}
}

// handleEvolveVerdictAction applies one settled low-confidence verdict and
// writes an idempotent, version-attributed P3 label. A rollback is recorded
// only when the exact card version is still live and restoration succeeds.
func (s *Server) handleEvolveVerdictAction(item workfeed.Item, actionID string) error {
	if s.genesisTracker == nil || s.genesisEvolver == nil {
		return fmt.Errorf("evolve verdict subsystem unavailable")
	}
	skill := strings.TrimSpace(item.Metadata["skill"])
	version := strings.TrimSpace(item.Metadata["version"])
	decisionID := strings.TrimSpace(item.Metadata["decisionId"])
	margin, err := strconv.ParseFloat(item.Metadata["judgeMargin"], 64)
	if skill == "" || version == "" || decisionID == "" || err != nil {
		s.logger.Warn("invalid low-confidence evolve verdict card", "ref", item.RefID)
		return fmt.Errorf("invalid low-confidence evolve verdict card")
	}
	if _, settled := s.genesisTracker.OperatorJudgeVerdictByDecisionID(decisionID); settled {
		return nil
	}
	verdict := genesis.OperatorJudgeVerdictConfirm
	if actionID == evolveVerdictRollback {
		if s.skillCatalog == nil {
			return fmt.Errorf("skill catalog unavailable")
		}
		entry, ok := s.skillCatalog.Get(skill)
		if !ok || entry.Skill.Version != version {
			s.logger.Warn("stale evolve rollback verdict ignored", "skill", skill, "cardVersion", version)
			return fmt.Errorf("stale evolve rollback verdict for %s@%s", skill, version)
		}
		if !s.genesisEvolver.RollbackSkillWithResult(skill) {
			s.logger.Warn("operator evolve rollback failed", "skill", skill, "version", version)
			return fmt.Errorf("operator evolve rollback failed for %s@%s", skill, version)
		}
		verdict = genesis.OperatorJudgeVerdictRollback
	} else if actionID != evolveVerdictConfirm {
		return fmt.Errorf("unsupported evolve verdict action %q", actionID)
	}
	if err := s.genesisTracker.LogOperatorJudgeVerdict(genesis.OperatorJudgeVerdict{
		DecisionID:   decisionID,
		Skill:        skill,
		Version:      version,
		Verdict:      verdict,
		JudgeVersion: item.Metadata["judgeVersion"],
		JudgeMargin:  margin,
	}); err != nil {
		s.logger.Warn("operator judge verdict ledger write failed", "skill", skill, "error", err)
		return fmt.Errorf("record operator judge verdict: %w", err)
	}
	return nil
}

// postMetaProposalCard surfaces one slow-loop revision in the work feed.
// adopted=true → auto-adoption NOTIFICATION with a post-hoc veto (되돌리기);
// adopted=false → the legacy decision card (auto-adopt kill switch off).
// Best-effort: a feed failure must never affect the meta-evolution cycle.
func (s *Server) postMetaProposalCard(artifact, epoch, reason, path string, adopted bool) {
	nf := s.nativeWorkFeedStore()
	if nf == nil {
		return
	}
	epochLabel := map[string]string{
		"producer":  "producer (evolve 프롬프트)",
		"evaluator": "evaluator (judge 프롬프트)",
		"genesis":   "genesis (신규 스킬 생성 프롬프트)",
	}[epoch]
	if epochLabel == "" {
		epochLabel = epoch
	}
	item := workfeed.Item{
		Source:  metaProposalSource,
		Summary: reason,
		RefType: "file",
		RefID:   path,
		Status:  "unread",
	}
	if adopted {
		item.Title = "메타 개정 자동 채택: " + artifact
		item.Body = fmt.Sprintf(`주간 자가개선 슬로우 루프가 메타 아티팩트 개정을 벤치 통과로 자동 채택했습니다.

- 대상: %s
- Epoch: %s
- 사유: %s
- 적용 파일: %s

건강 지표가 열화하면 롤백 워치가 자동 복원합니다. 지금 되돌리려면 아래 버튼을 누르세요.`,
			artifact, epochLabel, reason, path)
		item.Actions = []workfeed.Action{
			{ID: metaProposalActionRevert, Kind: workfeed.ActionAck, Label: "되돌리기"},
		}
	} else {
		item.Title = "메타 개정 제안: " + artifact
		item.Body = fmt.Sprintf(`주간 자가개선 슬로우 루프가 메타 아티팩트 개정을 제안했습니다 (게이트 통과, propose-only).

- 대상: %s
- Epoch: %s
- 사유: %s
- 제안 파일: %s

아래 버튼으로 채택하거나 기각하세요 — 결정은 메타 경험 원장에 기록되어 다음 사이클이 읽습니다.`,
			artifact, epochLabel, reason, path)
		item.Question = true // render the decision chips inline
		item.Actions = []workfeed.Action{
			{ID: metaProposalActionAdopt, Kind: workfeed.ActionAck, Label: "채택"},
			{ID: metaProposalActionReject, Kind: workfeed.ActionAck, Label: "기각"},
		}
	}
	// A new proposal for the same path supersedes the card that pointed at the
	// previous content: WriteProposal overwrote <name>.proposed, so adopting the
	// old card would have adopted THIS proposal under the old reason. Settle
	// the stale card first so only one live decision exists per proposal file.
	if !adopted {
		if err := nf.AckBySourceRef(metaProposalSource, path); err != nil {
			s.logger.Warn("meta proposal: stale card settle failed", "artifact", artifact, "error", err)
		}
	}
	if _, err := nf.Append(item); err != nil {
		s.logger.Warn("meta proposal 카드 생성 실패", "artifact", artifact, "error", err)
	}
}

// settleMetaProposalCard closes the decision card of a proposal whose verdict
// window expired (MetaEvolutionTask.expireStaleProposals discarded the file and
// wrote the ledger row). The card is acknowledged, not deleted — the feed
// history keeps the ask; the ledger keeps the outcome.
func (s *Server) settleMetaProposalCard(artifact, path, reason string) {
	nf := s.nativeWorkFeedStore()
	if nf == nil {
		return
	}
	if err := nf.AckBySourceRef(metaProposalSource, path); err != nil {
		s.logger.Warn("meta proposal: expired card settle failed", "artifact", artifact, "error", err)
		return
	}
	s.logger.Info("meta proposal card settled (verdict expired)", "artifact", artifact, "reason", reason)
}

// postDriftFreezeCard notifies the operator that the evolution self-brake
// engaged or released (auto-adopt freeze transition).
func (s *Server) postDriftFreezeCard(frozen bool, reasons []string) {
	nf := s.nativeWorkFeedStore()
	if nf == nil {
		return
	}
	var title, body string
	if frozen {
		title = "⚠️ 자가개선 자동 채택 동결 (자기 브레이크)"
		signalList := "- " + strings.Join(reasons, "\n- ")
		body = fmt.Sprintf(`진화 궤적 자가감사가 드리프트 신호를 감지해 메타 아티팩트 자동 채택을 동결했습니다. 제안은 계속 생성되지만 채택은 다시 사람 결정(propose-only)으로 돌아갑니다.

감지 신호:
%s

궤적이 회복되면(다음 사이클 재감사) 자동으로 해제됩니다. 즉시 해제하려면 ~/.deneb/data/auto_adopt_freeze.json 을 지우세요.`, signalList)
	} else {
		title = "자가개선 자동 채택 재개"
		body = "진화 궤적이 회복되어 자기 브레이크가 풀렸습니다 — 벤치 통과 제안이 다시 자동 채택됩니다."
	}
	if _, err := nf.Append(workfeed.Item{
		Source:  metaProposalSource,
		Title:   title,
		Summary: "메타 자동 채택 " + map[bool]string{true: "동결", false: "재개"}[frozen],
		Body:    body,
		Status:  "unread",
	}); err != nil {
		s.logger.Warn("drift freeze 카드 생성 실패", "frozen", frozen, "error", err)
	}
}

// postMetaRevertedCard notifies the operator that the meta rollback watch
// reverted an adoption. Informational — no decision required.
func (s *Server) postMetaRevertedCard(artifact, reason string) {
	nf := s.nativeWorkFeedStore()
	if nf == nil {
		return
	}
	if _, err := nf.Append(workfeed.Item{
		Source:  metaProposalSource,
		Title:   "메타 개정 자동 복원: " + artifact,
		Summary: reason,
		Body: fmt.Sprintf(`롤백 워치가 채택된 메타 개정을 자동 복원했습니다.

- 대상: %s
- 사유: %s

복원 결정은 메타 경험 원장에 기록되어 다음 사이클이 같은 방향을 재제안하지 않습니다.`, artifact, reason),
		Status: "unread",
	}); err != nil {
		s.logger.Warn("meta reverted 카드 생성 실패", "artifact", artifact, "error", err)
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
	fallback := skilllifecycle.DefaultMetaArtifacts()[artifact]
	fromVersion := s.genesisMeta.Version(artifact, fallback)
	switch actionID {
	case metaProposalActionAdopt:
		toVersion, err := s.genesisMeta.AdoptProposal(artifact)
		if err != nil {
			s.logger.Warn("meta proposal 채택 실패", "artifact", artifact, "error", err)
			return
		}
		// Snapshot evolution health like the auto-adopt path so the meta
		// rollback watch also covers operator feed-card adoptions — without it
		// an operator-adopted revision was never revert-watched (reviewer
		// feedback, #3459).
		eh := s.genesisTracker.EvolutionHealth()
		if err := s.genesisTracker.LogMetaRevision(genesis.MetaRevisionRecord{
			Artifact:    artifact,
			FromVersion: fromVersion,
			ToVersion:   toVersion,
			Action:      "adopted",
			Reason:      "operator adopted from feed card",
			AdoptionHealth: &genesis.MetaAdoptionHealth{
				ConfirmRate:     eh.ConfirmRate,
				FalseAcceptRate: eh.FalseAcceptRate,
				Resolved:        eh.ResolvedEvolves7d,
			},
		}); err != nil {
			s.logger.Warn("meta adoption ledger write failed", "artifact", artifact, "error", err)
		}
		s.logger.Info("meta proposal adopted from feed card", "artifact", artifact, "from", fromVersion, "to", toVersion)
	case metaProposalActionRevert:
		toVersion, err := s.genesisMeta.RevertAdoption(artifact)
		if err != nil {
			s.logger.Warn("meta proposal 되돌리기 실패", "artifact", artifact, "error", err)
			return
		}
		if err := s.genesisTracker.LogMetaRevision(genesis.MetaRevisionRecord{
			Artifact:    artifact,
			FromVersion: fromVersion,
			ToVersion:   toVersion,
			Action:      "operator_reverted",
			Reason:      "operator reverted the auto-adoption from feed card",
		}); err != nil {
			s.logger.Warn("meta revert ledger write failed", "artifact", artifact, "error", err)
		}
		s.logger.Info("meta adoption reverted from feed card", "artifact", artifact, "restored", toVersion)
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

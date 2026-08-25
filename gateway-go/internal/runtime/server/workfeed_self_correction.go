package server

// Self-correction Trust Inbox card (docs/research/improvement-ideas.md 4.7).
// Every NEW proposed self-correction candidate gets one feed card with
// 승인/거절 chips — the same approval:approve/approval:reject action ids the
// native client already renders through its approval dialog (an optional
// comment rides along on reject). Approve records the ledger review
// "accepted" (the candidate then waits for the dispatch lane or a batch
// review); reject records "rejected" with the comment as the review note.

import (
	"errors"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
)

const selfCorrectionSource = "self-correction"

// Shared approval action ids — the native client renders any action whose ID
// starts with "approval:" through the approval dialog, so reusing the
// groupware constants keeps one id vocabulary across every approval surface.
const (
	workfeedApproveAction = groupwareApprovalActionApprove
	workfeedRejectAction  = groupwareApprovalActionReject
)

// postSelfCorrectionCard surfaces one proposed self-correction candidate.
// Delivery failure is returned so the watch does not consume the transition
// before the operator-facing card is durable (same contract as the ladder
// READY card).
func (s *Server) postSelfCorrectionCard(record genesis.SelfCorrectionCandidateRecord) error {
	nf := s.nativeWorkFeedStore()
	if nf == nil {
		return errors.New("native work feed unavailable")
	}
	item := workfeed.Item{
		Source:  selfCorrectionSource,
		Title:   "자기교정 후보: " + record.Title,
		Summary: selfCorrectionCardSummary(record),
		Status:  workfeed.StatusUnread,
		RefType: "self-correction",
		RefID:   record.ID,
		// Question renders the approve/reject decision chips inline.
		Question: true,
		Actions: []workfeed.Action{
			{ID: workfeedApproveAction, Kind: workfeed.ActionAck, Label: "승인"},
			{ID: workfeedRejectAction, Kind: workfeed.ActionAck, Label: "거절"},
		},
		// Korean decision brief first, English record verbatim below — the
		// operator has to be able to READ the thing they are approving.
		Body: selfCorrectionCardBody(record, s.selfCorrectionBrief(record)),
	}
	if _, err := nf.Append(item); err != nil {
		s.logger.Warn("self-correction card post failed", "id", record.ID, "error", err)
		return fmt.Errorf("post self-correction card: %w", err)
	}
	return nil
}

func selfCorrectionCardSummary(record genesis.SelfCorrectionCandidateRecord) string {
	var parts []string
	if record.SkillName != "" {
		parts = append(parts, record.SkillName)
	}
	if record.Scope != "" {
		parts = append(parts, "scope="+record.Scope)
	}
	if record.Surface != "" {
		parts = append(parts, record.Surface)
	}
	summary := strings.Join(parts, " · ")
	if summary == "" && record.Candidate != "" {
		summary = record.Candidate
	}
	return summary
}

func selfCorrectionCardBody(record genesis.SelfCorrectionCandidateRecord, brief string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "자율 개선 루프가 자기교정 후보를 기록했습니다 (후보 ID %s).\n\n", record.ID)
	if brief != "" {
		b.WriteString("## 요약 (판단용)\n")
		b.WriteString(brief)
		b.WriteString("\n\n")
	}
	if record.SkillName != "" {
		fmt.Fprintf(&b, "- 스킬: %s\n", record.SkillName)
	}
	if record.Scope != "" {
		fmt.Fprintf(&b, "- 범위: %s\n", record.Scope)
	}
	if record.Source != "" {
		fmt.Fprintf(&b, "- 생산 주체: %s\n", record.Source)
	}
	if record.Surface != "" {
		fmt.Fprintf(&b, "- 표면 등급: %s\n", record.Surface)
	}
	if len(record.TargetFiles) > 0 {
		fmt.Fprintf(&b, "- 대상 파일: %s\n", strings.Join(record.TargetFiles, ", "))
	}
	b.WriteString("\n")
	if record.Candidate != "" || record.Evidence != "" {
		b.WriteString("### 원문 (기록 그대로)\n")
	}
	if record.Candidate != "" {
		fmt.Fprintf(&b, "**후보**\n%s\n\n", record.Candidate)
	}
	if record.Evidence != "" {
		fmt.Fprintf(&b, "**근거**\n%s\n\n", record.Evidence)
	}
	if record.ProposedChange != "" {
		fmt.Fprintf(&b, "**제안 변경**\n%s\n\n", record.ProposedChange)
	}
	if record.Risk != "" {
		fmt.Fprintf(&b, "**위험**\n%s\n\n", record.Risk)
	}
	b.WriteString("승인하면 원장에 'accepted'로 기록되어 배차 레인(코드 후보) 또는 배치 리뷰가 이어받습니다. 거절하면 의견과 함께 'rejected'로 원장에 남습니다.")
	return b.String()
}

// handleSelfCorrectionCardAction durably records the operator's ledger
// review. An error keeps the card unsettled so the decision can be retried;
// the ledger review itself is idempotent on a repeated status.
func (s *Server) handleSelfCorrectionCardAction(item workfeed.Item, actionID, comment string) error {
	if s.genesisTracker == nil {
		return errors.New("genesis tracker unavailable")
	}
	id := strings.TrimSpace(item.RefID)
	if id == "" {
		return errors.New("self-correction card carries no candidate id")
	}
	var status string
	switch actionID {
	case workfeedApproveAction:
		status = genesis.SelfCorrectionStatusAccepted
	case workfeedRejectAction:
		status = genesis.SelfCorrectionStatusRejected
	default:
		return fmt.Errorf("unsupported self-correction action %q", actionID)
	}
	if _, err := s.genesisTracker.RecordSelfCorrectionReview(genesis.SelfCorrectionCandidateRecord{
		ID:         id,
		Status:     status,
		Reviewer:   "operator-feed",
		ReviewNote: strings.TrimSpace(comment),
	}); err != nil {
		s.logger.Warn("self-correction review record failed", "id", id, "error", err)
		return fmt.Errorf("record self-correction review %s: %w", id, err)
	}
	s.logger.Info("self-correction reviewed from feed card", "id", id, "status", status)
	return nil
}

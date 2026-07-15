package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/groupware"
)

const (
	groupwareApprovalActionApprove = "approval:approve"
	groupwareApprovalActionReject  = "approval:reject"
)

func groupwareApprovalActions() []workfeed.Action {
	return []workfeed.Action{
		{ID: groupwareApprovalActionApprove, Kind: workfeed.ActionAck, Label: "승인"},
		{ID: groupwareApprovalActionReject, Kind: workfeed.ActionAck, Label: "반려"},
	}
}

// handleGroupwareApprovalAction runs Amaranth 승인/반려 before the card settles.
// Failure leaves the card unread so the operator can retry.
func (s *Server) handleGroupwareApprovalAction(item workfeed.Item, actionID string) error {
	docID := strings.TrimSpace(item.RefID)
	if docID == "" {
		docID = strings.TrimSpace(item.Metadata["docId"])
	}
	if docID == "" {
		return fmt.Errorf("groupware approval card missing docId")
	}
	decision := ""
	switch actionID {
	case groupwareApprovalActionApprove:
		decision = "approve"
	case groupwareApprovalActionReject:
		decision = "reject"
	default:
		return fmt.Errorf("unsupported groupware approval action %q", actionID)
	}
	cfg, ok := groupware.FromEnv()
	if !ok {
		return fmt.Errorf("groupware credentials unset")
	}
	ctx := context.Background()
	if s != nil {
		if sc := s.ShutdownCtx(); sc != nil {
			ctx = sc
		}
	}
	out, err := groupware.ActApproval(ctx, cfg, docID, decision, "")
	if err != nil {
		if s != nil && s.logger != nil {
			s.logger.Warn("groupware approval act failed",
				"docId", docID, "decision", decision, "error", err)
		}
		return err
	}
	if s != nil && s.logger != nil {
		s.logger.Info("groupware approval act ok",
			"docId", docID, "decision", decision, "result", out)
	}
	return nil
}

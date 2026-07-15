package server

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

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

type groupwareApprovalAct func(context.Context, groupware.Config, string, string, string) (string, error)

type groupwareApprovalAudit struct {
	docID    string
	decision string
	comment  string
}

// handleGroupwareApprovalAction runs Amaranth 승인/반려 before the card settles.
// Failure leaves the card unread so the operator can retry.
func (s *Server) handleGroupwareApprovalAction(item workfeed.Item, actionID, comment string) error {
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
	audit, err := runGroupwareApprovalAction(ctx, cfg, item, actionID, comment, groupware.ActApproval)
	if s != nil && s.logger != nil && audit.decision != "" {
		fields := []any{
			"docId", audit.docID,
			"decision", audit.decision,
			"commentPresent", audit.comment != "",
			"commentLen", utf8.RuneCountInString(audit.comment),
		}
		if err != nil {
			s.logger.Error("groupware approval act failed", fields...)
		} else {
			s.logger.Info("groupware approval act ok", fields...)
		}
	}
	return err
}

func runGroupwareApprovalAction(
	ctx context.Context,
	cfg groupware.Config,
	item workfeed.Item,
	actionID, comment string,
	act groupwareApprovalAct,
) (groupwareApprovalAudit, error) {
	audit := groupwareApprovalAudit{}
	docID := strings.TrimSpace(item.RefID)
	if docID == "" {
		docID = strings.TrimSpace(item.Metadata["docId"])
	}
	if docID == "" {
		return audit, fmt.Errorf("groupware approval card missing docId")
	}
	audit.docID = docID
	switch actionID {
	case groupwareApprovalActionApprove:
		audit.decision = "approve"
	case groupwareApprovalActionReject:
		audit.decision = "reject"
		audit.comment = groupware.SanitizeApprovalComment(comment)
	default:
		return audit, fmt.Errorf("unsupported groupware approval action %q", actionID)
	}
	if act == nil {
		return audit, fmt.Errorf("groupware approval actor unavailable")
	}
	_, err := act(ctx, cfg, audit.docID, audit.decision, audit.comment)
	return audit, err
}

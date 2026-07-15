package server

import (
	"context"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/groupware"
)

func TestRunGroupwareApprovalActionForwardsCommentOnlyForReject(t *testing.T) {
	tests := []struct {
		name         string
		actionID     string
		comment      string
		wantDecision string
		wantComment  string
	}{
		{
			name:         "reject forwards sanitized comment",
			actionID:     groupwareApprovalActionReject,
			comment:      "  예산\n\t<script> 재검토  ",
			wantDecision: "reject",
			wantComment:  "예산 script 재검토",
		},
		{
			name:         "approve drops supplied comment",
			actionID:     groupwareApprovalActionApprove,
			comment:      "must not be submitted",
			wantDecision: "approve",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotDocID := ""
			gotDecision := ""
			gotComment := ""
			act := func(_ context.Context, _ groupware.Config, docID, decision, comment string) (string, error) {
				gotDocID = docID
				gotDecision = decision
				gotComment = comment
				return "ok", nil
			}

			audit, err := runGroupwareApprovalAction(
				context.Background(),
				groupware.Config{},
				workfeed.Item{RefID: " 99178 "},
				tt.actionID,
				tt.comment,
				act,
			)
			if err != nil {
				t.Fatal(err)
			}
			if gotDocID != "99178" || gotDecision != tt.wantDecision || gotComment != tt.wantComment {
				t.Fatalf("actor args = %q/%q/%q, want %q/%q/%q", gotDocID, gotDecision, gotComment, "99178", tt.wantDecision, tt.wantComment)
			}
			if audit.docID != gotDocID || audit.decision != gotDecision || audit.comment != gotComment {
				t.Fatalf("audit = %+v, actor args = %q/%q/%q", audit, gotDocID, gotDecision, gotComment)
			}
		})
	}
}

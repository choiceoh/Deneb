package server

import (
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
)

func TestNativeWorkFeedSourceRefHelpers(t *testing.T) {
	store := workfeed.NewStore(filepath.Join(t.TempDir(), "workfeed.jsonl"))
	native := &nativeWorkFeedStore{store: store}
	if _, err := store.Append(workfeed.Item{
		ID: "a", Source: workfeed.SourceGroupwareApproval, RefID: "99178", Body: "phone",
	}); err != nil {
		t.Fatal(err)
	}

	updated, err := native.EscalateApprovalBySourceRef("99178", 1, "4시간째")
	if err != nil || !updated {
		t.Fatalf("escalate updated=%v err=%v", updated, err)
	}
	escalated, ok, err := store.FindActiveBySourceRef(workfeed.SourceGroupwareApproval, "99178")
	if err != nil || !ok || escalated.Priority != workfeed.PriorityHigh {
		t.Fatalf("escalated %+v ok=%v err=%v", escalated, ok, err)
	}

	active, err := native.HasActiveSourceRef(workfeed.SourceGroupwareApproval, "99178")
	if err != nil || !active {
		t.Fatalf("HasActiveSourceRef = %v, %v", active, err)
	}
	if err := native.AckBySourceRef(workfeed.SourceGroupwareApproval, "99178"); err != nil {
		t.Fatal(err)
	}
	active, err = native.HasActiveSourceRef(workfeed.SourceGroupwareApproval, "99178")
	if err != nil || active {
		t.Fatalf("post-ack HasActiveSourceRef = %v, %v", active, err)
	}
	if err := native.AckBySourceRef(workfeed.SourceGroupwareApproval, "99178"); err != nil {
		t.Fatalf("idempotent AckBySourceRef: %v", err)
	}
}

func TestNativeWorkFeedRunActionForwardsCommentOnlyForApprovalReject(t *testing.T) {
	tests := []struct {
		name        string
		source      string
		actionID    string
		comment     string
		wantCalls   int
		wantComment string
	}{
		{
			name:        "reject receives sanitized comment",
			source:      workfeed.SourceGroupwareApproval,
			actionID:    groupwareApprovalActionReject,
			comment:     "  재고\n\t<script> 확인  ",
			wantCalls:   1,
			wantComment: "재고 script 확인",
		},
		{
			name:      "approve drops comment",
			source:    workfeed.SourceGroupwareApproval,
			actionID:  groupwareApprovalActionApprove,
			comment:   "must not forward",
			wantCalls: 1,
		},
		{
			name:     "generic action tolerates and drops comment",
			source:   "mail",
			actionID: "ack",
			comment:  "must not forward",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := workfeed.NewStore(filepath.Join(t.TempDir(), "workfeed.jsonl"))
			if _, err := store.Append(workfeed.Item{
				ID:      "item",
				Source:  tt.source,
				Actions: []workfeed.Action{{ID: tt.actionID, Kind: workfeed.ActionAck}},
			}); err != nil {
				t.Fatal(err)
			}
			calls := 0
			gotComment := ""
			native := &nativeWorkFeedStore{
				store: store,
				onApprovalAct: func(_ workfeed.Item, _ string, comment string) error {
					calls++
					gotComment = comment
					return nil
				},
			}

			if _, err := native.RunAction("item", tt.actionID, tt.comment); err != nil {
				t.Fatal(err)
			}
			if calls != tt.wantCalls || gotComment != tt.wantComment {
				t.Fatalf("approval callback calls/comment = %d/%q, want %d/%q", calls, gotComment, tt.wantCalls, tt.wantComment)
			}
		})
	}
}

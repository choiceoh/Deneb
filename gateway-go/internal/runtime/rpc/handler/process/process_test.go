package process

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/approval"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpctest"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

var call = rpctest.Call

// ---------------------------------------------------------------------------
// ApprovalMethods
// ---------------------------------------------------------------------------



func TestExecApprovalRequest_missingCommand(t *testing.T) {
	store := approval.NewStore()
	m := ApprovalMethods(ApprovalDeps{Store: store})
	resp := call(m, "exec.approval.request", map[string]any{})
	if resp == nil || resp.Error == nil {
		t.Fatal("expected error for missing command")
	}
	if resp.Error.Code != protocol.ErrMissingParam {
		t.Errorf("got error code %v, want ErrMissingParam", resp.Error.Code)
	}
}


func TestExecApprovalWaitDecision_missingID(t *testing.T) {
	store := approval.NewStore()
	m := ApprovalMethods(ApprovalDeps{Store: store})
	resp := call(m, "exec.approval.waitDecision", map[string]any{})
	if resp == nil || resp.Error == nil {
		t.Fatal("expected error for missing id")
	}
}

func TestExecApprovalsGet_returnsSnapshot(t *testing.T) {
	store := approval.NewStore()
	m := ApprovalMethods(ApprovalDeps{Store: store})
	resp := call(m, "exec.approvals.get", map[string]any{})
	if resp == nil || resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp)
	}
}

// ---------------------------------------------------------------------------
// CronAdvancedMethods
// ---------------------------------------------------------------------------


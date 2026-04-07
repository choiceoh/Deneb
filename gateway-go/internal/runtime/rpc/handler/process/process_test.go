package process

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/approval"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpctest"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

var call = rpctest.Call

// ---------------------------------------------------------------------------
// ApprovalMethods
// ---------------------------------------------------------------------------

func TestApprovalMethods_nilStore(t *testing.T) {
	m := ApprovalMethods(ApprovalDeps{})
	if m != nil {
		t.Fatal("expected nil when Store is nil")
	}
}

func TestApprovalMethods_returnsHandlers(t *testing.T) {
	store := approval.NewStore()
	m := ApprovalMethods(ApprovalDeps{Store: store})
	for _, name := range []string{
		"exec.approval.request",
		"exec.approval.waitDecision",
		"exec.approval.resolve",
		"exec.approvals.get",
		"exec.approvals.set",
	} {
		if _, ok := m[name]; !ok {
			t.Errorf("missing handler %q", name)
		}
	}
}

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

func TestExecApprovalRequest_invalidJSON(t *testing.T) {
	store := approval.NewStore()
	m := ApprovalMethods(ApprovalDeps{Store: store})
	req := &protocol.RequestFrame{ID: "t1", Method: "exec.approval.request", Params: json.RawMessage(`not-json`)}
	resp := m["exec.approval.request"](context.Background(), req)
	if resp == nil || resp.Error == nil {
		t.Fatal("expected error for invalid JSON params")
	}
	if resp.Error.Code != protocol.ErrInvalidRequest {
		t.Errorf("got error code %v, want ErrInvalidRequest", resp.Error.Code)
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

func TestCronAdvancedMethods_nilCron(t *testing.T) {
	m := CronAdvancedMethods(CronAdvancedDeps{})
	if m != nil {
		t.Fatal("expected nil when Cron is nil")
	}
}

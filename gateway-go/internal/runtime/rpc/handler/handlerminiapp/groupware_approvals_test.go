package handlerminiapp

import (
	"context"
	"errors"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/groupware"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func TestGroupwareApprovalsList_DefaultsToTotalAndFiltersDate(t *testing.T) {
	var gotFolder string
	var gotLimit int
	docs := []groupware.ApprovalSummary{
		{DocID: "1", Title: "오늘 품의", Date: "2026-07-16", Status: "결재대기", Folder: "pending"},
		{DocID: "2", Title: "어제 품의", Date: "2026-07-15", Status: "결재완료", Folder: "done"},
		{DocID: "3", Title: "오늘 기결", Date: "2026.07.16", Status: "결재완료", Folder: "done"},
	}
	h := GroupwareApprovalsMethods(GroupwareApprovalsDeps{
		List: func(_ context.Context, folder string, limit int) ([]groupware.ApprovalSummary, error) {
			gotFolder = folder
			gotLimit = limit
			return docs, nil
		},
		Act: func(context.Context, string, string, string) (string, error) { return "", nil },
	})["miniapp.groupware.approvals.list"]

	resp := h(authedCtx(), reqWith(t, "miniapp.groupware.approvals.list", map[string]any{
		"date": "2026-07-16",
	}))
	var out GroupwareApprovalsListResponse
	decode(t, resp, &out)
	if gotFolder != "total" {
		t.Fatalf("folder = %q, want total", gotFolder)
	}
	if gotLimit != defaultApprovalsLimit {
		t.Fatalf("limit = %d, want %d", gotLimit, defaultApprovalsLimit)
	}
	if out.Folder != "total" {
		t.Fatalf("response folder = %q, want total", out.Folder)
	}
	if len(out.Approvals) != 2 {
		t.Fatalf("approvals = %d, want 2 (today only)", len(out.Approvals))
	}
	if !out.Approvals[0].CanAct {
		t.Fatal("pending doc should be canAct")
	}
	if out.Approvals[1].CanAct {
		t.Fatal("done doc should not be canAct")
	}
}

func TestGroupwareApprovalsList_RejectsBadFolderAndDate(t *testing.T) {
	h := GroupwareApprovalsMethods(GroupwareApprovalsDeps{
		List: func(context.Context, string, int) ([]groupware.ApprovalSummary, error) {
			return nil, nil
		},
		Act: func(context.Context, string, string, string) (string, error) { return "", nil },
	})["miniapp.groupware.approvals.list"]

	resp := h(authedCtx(), reqWith(t, "miniapp.groupware.approvals.list", map[string]any{"folder": "inbox"}))
	if resp == nil || resp.OK {
		t.Fatal("expected invalid folder error")
	}
	resp = h(authedCtx(), reqWith(t, "miniapp.groupware.approvals.list", map[string]any{"date": "July 16"}))
	if resp == nil || resp.OK {
		t.Fatal("expected invalid date error")
	}
}

func TestGroupwareApprovalsList_UnavailableOnListError(t *testing.T) {
	h := GroupwareApprovalsMethods(GroupwareApprovalsDeps{
		List: func(context.Context, string, int) ([]groupware.ApprovalSummary, error) {
			return nil, errors.New("reader down")
		},
		Act: func(context.Context, string, string, string) (string, error) { return "", nil },
	})["miniapp.groupware.approvals.list"]
	resp := h(authedCtx(), reqWith(t, "miniapp.groupware.approvals.list", map[string]any{}))
	if resp == nil || resp.OK {
		t.Fatal("expected dependency failure")
	}
	if resp.Error.Code != protocol.ErrDependencyFailed {
		t.Fatalf("code = %s, want DEPENDENCY_FAILED", resp.Error.Code)
	}
}

func TestGroupwareApprovalsAct_ApproveAndReject(t *testing.T) {
	var gotDoc, gotDecision, gotComment string
	h := GroupwareApprovalsMethods(GroupwareApprovalsDeps{
		List: func(context.Context, string, int) ([]groupware.ApprovalSummary, error) {
			return nil, nil
		},
		Act: func(_ context.Context, docID, decision, comment string) (string, error) {
			gotDoc, gotDecision, gotComment = docID, decision, comment
			return "ok", nil
		},
	})["miniapp.groupware.approvals.act"]

	resp := h(authedCtx(), reqWith(t, "miniapp.groupware.approvals.act", map[string]any{
		"docId":    "99178",
		"decision": "승인",
		"comment":  "확인",
	}))
	var out GroupwareApprovalActResponse
	decode(t, resp, &out)
	if !out.OK || out.DocID != "99178" || out.Decision != "approve" {
		t.Fatalf("unexpected act response: %+v", out)
	}
	if gotDoc != "99178" || gotDecision != "승인" || gotComment != "확인" {
		t.Fatalf("act args = %q %q %q", gotDoc, gotDecision, gotComment)
	}

	resp = h(authedCtx(), reqWith(t, "miniapp.groupware.approvals.act", map[string]any{
		"docId":    "99178",
		"decision": "maybe",
	}))
	if resp == nil || resp.OK {
		t.Fatal("expected invalid decision")
	}
}

func TestGroupwareApprovalsMethods_NilDeps(t *testing.T) {
	if GroupwareApprovalsMethods(GroupwareApprovalsDeps{}) != nil {
		t.Fatal("nil deps should disable registration")
	}
}

func TestApprovalDayKey(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"2026-07-16", "2026-07-16", true},
		{"2026.07.16", "2026-07-16", true},
		{"20260716", "2026-07-16", true},
		{"2026-07-16 14:30", "2026-07-16", true},
		{"", "", false},
		{"July", "", false},
	}
	for _, tc := range cases {
		got, ok := approvalDayKey(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("approvalDayKey(%q) = %q,%v want %q,%v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

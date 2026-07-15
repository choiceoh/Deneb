package handlerminiapp

import (
	"context"
	"errors"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/groupware"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func TestGroupwareApprovalsList_DefaultsToTotalAndFiltersDate(t *testing.T) {
	var folders []string
	var gotLimit int
	docs := []groupware.ApprovalSummary{
		{DocID: "1", Title: "오늘 품의", Date: "2026-07-16", Status: "결재대기", Folder: "pending"},
		{DocID: "2", Title: "어제 품의", Date: "2026-07-15", Status: "결재완료", Folder: "done"},
		{DocID: "3", Title: "오늘 기결", Date: "2026.07.16", Status: "결재완료", Folder: "done"},
	}
	h := GroupwareApprovalsMethods(GroupwareApprovalsDeps{
		List: func(_ context.Context, folder string, limit int) ([]groupware.ApprovalSummary, error) {
			folders = append(folders, folder)
			gotLimit = limit
			if folder == "pending" {
				return []groupware.ApprovalSummary{docs[0]}, nil
			}
			return docs, nil
		},
		Act: func(context.Context, string, string, string) (string, error) { return "", nil },
	})["miniapp.groupware.approvals.list"]

	resp := h(authedCtx(), reqWith(t, "miniapp.groupware.approvals.list", map[string]any{
		"date": "2026-07-16",
	}))
	var out GroupwareApprovalsListResponse
	decode(t, resp, &out)
	if len(folders) < 1 || folders[0] != "total" {
		t.Fatalf("folders = %v, want total first", folders)
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

func TestGroupwareApprovalsList_TotalMarksPendingCanAct(t *testing.T) {
	h := GroupwareApprovalsMethods(GroupwareApprovalsDeps{
		List: func(_ context.Context, folder string, _ int) ([]groupware.ApprovalSummary, error) {
			switch folder {
			case "pending":
				return []groupware.ApprovalSummary{
					{DocID: "9", Title: "미결 품의", Date: "2026-07-16", Status: "미결", Folder: "pending"},
				}, nil
			case "total":
				return []groupware.ApprovalSummary{
					{DocID: "9", Title: "미결 품의", Date: "2026-07-16", Status: "", Folder: "total"},
					{DocID: "8", Title: "끝난 품의", Date: "2026-07-16", Status: "", Folder: "total"},
				}, nil
			default:
				return nil, nil
			}
		},
		Act: func(context.Context, string, string, string) (string, error) { return "", nil },
	})["miniapp.groupware.approvals.list"]

	resp := h(authedCtx(), reqWith(t, "miniapp.groupware.approvals.list", map[string]any{"folder": "total"}))
	var out GroupwareApprovalsListResponse
	decode(t, resp, &out)
	if len(out.Approvals) != 2 {
		t.Fatalf("approvals = %d, want 2", len(out.Approvals))
	}
	if !out.Approvals[0].CanAct || out.Approvals[0].Status != "미결" {
		t.Fatalf("doc 9 = canAct=%v status=%q, want canAct with 미결", out.Approvals[0].CanAct, out.Approvals[0].Status)
	}
	if out.Approvals[1].CanAct {
		t.Fatal("doc 8 should not be canAct")
	}
}

func TestApprovalCanAct_MigyeolStatus(t *testing.T) {
	if !approvalCanAct(groupware.ApprovalSummary{Status: "미결", Folder: "total"}) {
		t.Fatal("미결 status should be canAct")
	}
	if approvalCanAct(groupware.ApprovalSummary{Status: "", Folder: "total"}) {
		t.Fatal("blank total status should not be canAct alone")
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

func TestGroupwareApprovalsGetAndAnalyzeCache(t *testing.T) {
	dir := t.TempDir()
	cache := groupware.NewApprovalAnalysisStore(dir)
	var analyzeCalls int
	h := GroupwareApprovalsMethods(GroupwareApprovalsDeps{
		List: func(context.Context, string, int) ([]groupware.ApprovalSummary, error) { return nil, nil },
		Act:  func(context.Context, string, string, string) (string, error) { return "", nil },
		Get: func(_ context.Context, docID string) (string, error) {
			if docID != "42" {
				t.Fatalf("docId = %q", docID)
			}
			return "본문 내용\n금액 100", nil
		},
		Cache: cache,
		Analyze: func(_ context.Context, title, body string) (string, string, error) {
			analyzeCalls++
			if title == "" || body == "" {
				t.Fatalf("empty analyze input title=%q body=%q", title, body)
			}
			return "## 요지\nok\nIMPORTANCE: urgent", "urgent", nil
		},
	})

	get := h["miniapp.groupware.approvals.get"]
	resp := get(authedCtx(), reqWith(t, "miniapp.groupware.approvals.get", map[string]any{
		"docId": "42", "title": "품의",
	}))
	var got GroupwareApprovalGetResponse
	decode(t, resp, &got)
	if got.Body == "" || got.DocID != "42" {
		t.Fatalf("get = %+v", got)
	}

	cached := h["miniapp.groupware.approvals.analysis_cached"]
	resp = cached(authedCtx(), reqWith(t, "miniapp.groupware.approvals.analysis_cached", map[string]any{"docId": "42"}))
	var miss GroupwareApprovalAnalysisOut
	decode(t, resp, &miss)
	if miss.Cached || miss.Analysis != "" {
		t.Fatalf("expected cache miss, got %+v", miss)
	}

	analyze := h["miniapp.groupware.approvals.analyze"]
	resp = analyze(authedCtx(), reqWith(t, "miniapp.groupware.approvals.analyze", map[string]any{
		"docId": "42", "title": "품의",
	}))
	var out GroupwareApprovalAnalysisOut
	decode(t, resp, &out)
	if out.Cached || out.Analysis == "" || out.Importance != "urgent" {
		t.Fatalf("analyze = %+v", out)
	}
	if analyzeCalls != 1 {
		t.Fatalf("analyzeCalls = %d", analyzeCalls)
	}

	resp = analyze(authedCtx(), reqWith(t, "miniapp.groupware.approvals.analyze", map[string]any{
		"docId": "42", "title": "품의",
	}))
	decode(t, resp, &out)
	if !out.Cached || analyzeCalls != 1 {
		t.Fatalf("second analyze should hit cache: cached=%v calls=%d", out.Cached, analyzeCalls)
	}
}

func TestGroupwareERPList(t *testing.T) {
	var gotArea, gotFolder, gotQuery string
	var gotLimit int
	h := GroupwareApprovalsMethods(GroupwareApprovalsDeps{
		List: func(context.Context, string, int) ([]groupware.ApprovalSummary, error) { return nil, nil },
		Act:  func(context.Context, string, string, string) (string, error) { return "", nil },
		ListERP: func(_ context.Context, area, folder, query string, limit int) (string, error) {
			gotArea, gotFolder, gotQuery, gotLimit = area, folder, query, limit
			return "재고 2건", nil
		},
	})["miniapp.groupware.erp.list"]
	resp := h(authedCtx(), reqWith(t, "miniapp.groupware.erp.list", map[string]any{
		"area": "stock", "query": "볼트",
	}))
	var out GroupwareERPListResponse
	decode(t, resp, &out)
	if out.Text != "재고 2건" || gotArea != "stock" || gotQuery != "볼트" || gotLimit != defaultERPLimit {
		t.Fatalf("out=%+v area=%s query=%s limit=%d", out, gotArea, gotQuery, gotLimit)
	}
	_ = gotFolder
}

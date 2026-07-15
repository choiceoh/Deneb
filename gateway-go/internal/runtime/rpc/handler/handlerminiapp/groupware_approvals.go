// groupware_approvals.go — miniapp.groupware.approvals.* RPC handlers.
//
//   miniapp.groupware.approvals.list — recent 전체 결재 (folder=total by default),
//                                      optionally filtered to one calendar day
//   miniapp.groupware.approvals.act  — 승인/반려 by docId (operator path only)
//
// The chat groupware tool stays read-only; mutate stays on this RPC (and work-
// feed chips). Clients browse by day (메일/피드 날짜 스테퍼) — the list returns a
// recent snapshot and optional date=YYYY-MM-DD filters to that local day.

package handlerminiapp

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/core/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/groupware"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

const (
	defaultApprovalsFolder = "total"
	defaultApprovalsLimit  = 50
	maxApprovalsLimit      = 100
)

var (
	approvalDayYMD  = regexp.MustCompile(`^(\d{4})[-./]?(\d{2})[-./]?(\d{2})`)
	approvalFolders = map[string]struct{}{"pending": {}, "done": {}, "cc": {}, "total": {}, "all": {}}
)

// GroupwareApprovalsDeps wires the Amaranth list/act calls. Nil List or Act
// disables registration (feature off).
type GroupwareApprovalsDeps struct {
	List func(ctx context.Context, folder string, limit int) ([]groupware.ApprovalSummary, error)
	Act  func(ctx context.Context, docID, decision, comment string) (string, error)
}

// GroupwareApprovalsMethods returns the miniapp.groupware.approvals.* map, or
// nil when List/Act are not wired.
func GroupwareApprovalsMethods(deps GroupwareApprovalsDeps) map[string]rpcutil.HandlerFunc {
	if deps.List == nil || deps.Act == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"miniapp.groupware.approvals.list": groupwareApprovalsList(deps),
		"miniapp.groupware.approvals.act":  groupwareApprovalsAct(deps),
	}
}

func groupwareApprovalsList(deps GroupwareApprovalsDeps) rpcutil.HandlerFunc {
	type params struct {
		Folder string `json:"folder,omitempty"`
		Limit  int    `json:"limit,omitempty"`
		// Date is an optional local calendar day (YYYY-MM-DD). When set, only
		// rows whose Amaranth date falls on that day are returned — so the
		// 메일/피드 day-pager can request one day without client-side filtering.
		Date string `json:"date,omitempty"`
	}
	return bindAuthenticatedOptional[params](func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
		folder := strings.TrimSpace(strings.ToLower(p.Folder))
		if folder == "" {
			folder = defaultApprovalsFolder
		}
		if _, ok := approvalFolders[folder]; !ok {
			return rpcerr.InvalidRequest("folder must be pending|done|cc|total|all").Response(req.ID)
		}
		limit := p.Limit
		if limit <= 0 {
			limit = defaultApprovalsLimit
		}
		if limit > maxApprovalsLimit {
			limit = maxApprovalsLimit
		}
		dayKey := ""
		if d := strings.TrimSpace(p.Date); d != "" {
			key, ok := approvalDayKey(d)
			if !ok {
				return rpcerr.InvalidRequest("date must be YYYY-MM-DD").Response(req.ID)
			}
			dayKey = key
		}
		docs, err := deps.List(ctx, folder, limit)
		if err != nil {
			return rpcerr.WrapDependencyFailed("list groupware approvals", err).Response(req.ID)
		}
		rows := make([]GroupwareApprovalRow, 0, len(docs))
		for _, doc := range docs {
			if dayKey != "" {
				got, ok := approvalDayKey(doc.Date)
				if !ok || got != dayKey {
					continue
				}
			}
			rows = append(rows, GroupwareApprovalRow{
				DocID:   doc.DocID,
				Title:   doc.Title,
				DocNo:   doc.DocNo,
				Drafter: doc.Drafter,
				Date:    doc.Date,
				Status:  doc.Status,
				Folder:  doc.Folder,
				CanAct:  approvalCanAct(doc),
			})
		}
		return rpcutil.RespondOK(req.ID, GroupwareApprovalsListResponse{
			Approvals: rows,
			Folder:    folder,
		})
	})
}

func groupwareApprovalsAct(deps GroupwareApprovalsDeps) rpcutil.HandlerFunc {
	type params struct {
		DocID    string `json:"docId"`
		Decision string `json:"decision"`
		Comment  string `json:"comment,omitempty"`
	}
	return bindAuthenticated[params](func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
		docID := strings.TrimSpace(p.DocID)
		if docID == "" {
			return rpcerr.MissingParam("docId").Response(req.ID)
		}
		decision := strings.TrimSpace(strings.ToLower(p.Decision))
		switch decision {
		case "approve", "reject", "승인", "반려":
		default:
			return rpcerr.InvalidRequest("decision must be approve|reject").Response(req.ID)
		}
		out, err := deps.Act(ctx, docID, decision, strings.TrimSpace(p.Comment))
		if err != nil {
			return rpcerr.WrapDependencyFailed("act groupware approval", err).Response(req.ID)
		}
		normalized := decision
		switch decision {
		case "승인":
			normalized = "approve"
		case "반려":
			normalized = "reject"
		}
		return rpcutil.RespondOK(req.ID, GroupwareApprovalActResponse{
			OK:       true,
			DocID:    docID,
			Decision: normalized,
			Result:   out,
		})
	})
}

func approvalCanAct(doc groupware.ApprovalSummary) bool {
	if strings.EqualFold(strings.TrimSpace(doc.Folder), "pending") {
		return true
	}
	status := strings.TrimSpace(doc.Status)
	return strings.Contains(status, "대기") || strings.EqualFold(status, "pending")
}

// approvalDayKey normalizes Amaranth date stamps to YYYY-MM-DD. Accepts
// 2026-07-16, 2026.07.16, 20260716, and prefixes of longer timestamps.
func approvalDayKey(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", false
	}
	m := approvalDayYMD.FindStringSubmatch(s)
	if m == nil {
		return "", false
	}
	y, mo, d := m[1], m[2], m[3]
	if _, err := time.Parse("2006-01-02", fmt.Sprintf("%s-%s-%s", y, mo, d)); err != nil {
		return "", false
	}
	return fmt.Sprintf("%s-%s-%s", y, mo, d), true
}

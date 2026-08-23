// groupware_approvals.go — miniapp.groupware.approvals.* + erp.list RPC handlers.
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
	defaultERPLimit        = 40
	maxERPLimit            = 80
)

var (
	approvalDayYMD  = regexp.MustCompile(`^(\d{4})[-./]?(\d{2})[-./]?(\d{2})`)
	approvalFolders = map[string]struct{}{"pending": {}, "done": {}, "cc": {}, "total": {}, "all": {}}
)

const approvalAnalyzeSystemPrompt = `당신은 Topsolar Amaranth 전자결재 문서를 요약하는 비서입니다.
한국어로 작성하세요. 마크다운을 쓰되 장황하지 않게.

반드시 포함할 섹션:
1. **요지** — 한 줄 요약
2. **핵심** — 금액·기간·상대·목적 등 결정 사실 불릿
3. **리스크 / 확인 포인트** — 승인 전 볼 것
4. **권고** — 승인·조건부·반려 중 하나와 한 줄 이유

입력에 "## 첨부 내용" 섹션이 있으면 첨부 원문(계약·견적·세금계산서 등)의 수치·조건을
본문보다 우선해 반영하세요. 첨부에서만 보이는 금액·계좌·당사자·특약은 핵심/리스크에 명시하세요.

입력에 "과거 단가·경비 이력" 섹션이 주어지면 **단가 비교** 섹션을 추가하고,
이번 결재의 단가/금액을 과거 이력과 비교해 변동(오름/내림/동일)과 유불리를 짚으세요.
이력에 없는 수치를 지어내지 말고, 이력 섹션이 없으면 이 섹션은 생략하세요.

입력에 "## 과거 유사 결재 (전례)" 섹션이 있으면 **전례 대조** 섹션을 추가하세요:
이번 건이 반복 패턴(정기 발주·동일 거래처)인지, 금액·조건이 전례 대비 어떻게
달라졌는지, 과거 권고와 어긋나는 점이 있는지 1–3불릿. 전례가 이번 판단에
의미 없으면 "특이점 없음" 한 줄로. 전례 섹션이 없으면 이 섹션은 생략하세요.

입력에 "## 프로젝트 후보" 섹션이 있으면 그 프로젝트의 진행 로그/현재 상태에
남길 가치가 있는지 판단하세요. 프로젝트 진행·발주·계약·일정·용량·리스크에
남는 사실이 있으면 yes. 단순 경비·전사 공통·프로젝트와 무관하면 no.
IMPORTANCE(푸시 긴급도)와 무관합니다. 후보 섹션이 없으면 PROJECT_FILE은 no.

마지막에 정확히 다음 두 줄을 적으세요:
IMPORTANCE: urgent|attention|routine
PROJECT_FILE: yes|no`

// GroupwareApprovalsDeps wires Amaranth list/act/get/analyze/Q&A. List+Act required
// for the main method set; Get/Cache/Analyze/Attach/Ask are optional.
type GroupwareApprovalsDeps struct {
	List func(ctx context.Context, folder string, limit int) ([]groupware.ApprovalSummary, error)
	Act  func(ctx context.Context, docID, decision, comment string) (string, error)
	// Get fetches one document body; folder is a best-effort box hint ("" = scan).
	Get func(ctx context.Context, docID, folder string) (body string, err error)
	// Attach fetches one attachment's extracted text (OCR / parser).
	Attach func(ctx context.Context, docID, selector string) (text string, err error)
	// Analyze runs the LLM on title+body. docID/date thread through so the
	// price-memory loop can file the approved cost onto the deal ledger
	// idempotently. Nil → analyze RPC returns UNAVAILABLE.
	Analyze func(ctx context.Context, docID, title, date, body string) (analysis, importance string, err error)
	Cache   *groupware.ApprovalAnalysisStore
	// Ask runs an ephemeral, read-only LLM Q&A grounded in the document body
	// and cached analysis. nil disables miniapp.groupware.approvals.ask.
	Ask func(ctx context.Context, docID, title, body, analysis string, history []QATurn, question string) (answer string, err error)
	// ListERP powers miniapp.groupware.erp.list (stock/po/…/people/board).
	ListERP func(ctx context.Context, area, folder, query string, limit int) (string, error)
	// ReadBoard powers miniapp.groupware.board.get (one post body by id/title).
	ReadBoard func(ctx context.Context, query string) (string, error)
	// LogWiki mirrors a fresh analysis into the project wiki log + 현재 상태
	// (best-effort). body fuels UniqueProjectInText when the title alone is vague.
	LogWiki func(rec *groupware.ApprovalAnalysisRecord, body string)
}

// GroupwareApprovalsMethods returns the miniapp.groupware.* map, or nil when
// List/Act are not wired.
func GroupwareApprovalsMethods(deps GroupwareApprovalsDeps) map[string]rpcutil.HandlerFunc {
	if deps.List == nil || deps.Act == nil {
		return nil
	}
	m := map[string]rpcutil.HandlerFunc{
		"miniapp.groupware.approvals.list": groupwareApprovalsList(deps),
		"miniapp.groupware.approvals.act":  groupwareApprovalsAct(deps),
	}
	if deps.Get != nil {
		m["miniapp.groupware.approvals.get"] = groupwareApprovalsGet(deps)
	}
	if deps.Attach != nil {
		m["miniapp.groupware.approvals.attachment"] = groupwareApprovalsAttachment(deps)
	}
	if deps.Get != nil && deps.Cache != nil {
		m["miniapp.groupware.approvals.analysis_cached"] = groupwareApprovalsAnalysisCached(deps)
		m["miniapp.groupware.approvals.analyze"] = groupwareApprovalsAnalyze(deps)
	}
	for name, handler := range GroupwareApprovalsAskMethods(deps) {
		m[name] = handler
	}
	if deps.ListERP != nil {
		m["miniapp.groupware.erp.list"] = groupwareERPList(deps)
	}
	if deps.ReadBoard != nil {
		m["miniapp.groupware.board.get"] = groupwareBoardGet(deps)
	}
	return m
}

func groupwareApprovalsList(deps GroupwareApprovalsDeps) rpcutil.HandlerFunc {
	type params struct {
		Folder     string `json:"folder,omitempty"`
		Limit      int    `json:"limit,omitempty"`
		Date       string `json:"date,omitempty"`
		AfterDocID string `json:"afterDocId,omitempty"`
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
		afterID := strings.TrimSpace(p.AfterDocID)
		// Amaranth has no cursor — a follow-up page must scan past afterDocId,
		// so pull the max window once and slice. First page stays cheap (limit).
		fetchLimit := limit
		if afterID != "" {
			fetchLimit = maxApprovalsLimit
		}
		dayKey := ""
		if d := strings.TrimSpace(p.Date); d != "" {
			key, ok := approvalDayKey(d)
			if !ok {
				return rpcerr.InvalidRequest("date must be YYYY-MM-DD").Response(req.ID)
			}
			dayKey = key
		}
		docs, err := deps.List(ctx, folder, fetchLimit)
		if err != nil {
			return rpcerr.WrapDependencyFailed("list groupware approvals", err).Response(req.ID)
		}
		// Amaranth "total" rows ship empty status/folder=total, so canAct would
		// always be false without cross-checking the pending box.
		pendingByID := approvalPendingIndex(ctx, deps, folder, fetchLimit)
		rows := make([]GroupwareApprovalRow, 0, len(docs))
		for _, doc := range docs {
			if dayKey != "" {
				got, ok := approvalDayKey(doc.Date)
				if !ok || got != dayKey {
					continue
				}
			}
			status := doc.Status
			canAct := approvalCanAct(doc)
			if pend, ok := pendingByID[strings.TrimSpace(doc.DocID)]; ok {
				canAct = true
				if strings.TrimSpace(status) == "" {
					status = pend.Status
				}
			}
			rows = append(rows, GroupwareApprovalRow{
				DocID:   doc.DocID,
				Title:   doc.Title,
				DocNo:   doc.DocNo,
				Drafter: doc.Drafter,
				Date:    doc.Date,
				Status:  status,
				Folder:  doc.Folder,
				CanAct:  canAct,
			})
		}
		if afterID != "" {
			cut := -1
			for i := range rows {
				if strings.TrimSpace(rows[i].DocID) == afterID {
					cut = i
					break
				}
			}
			if cut < 0 {
				// Stale cursor (doc left the window) — empty page, no further cursor.
				rows = nil
			} else {
				rows = rows[cut+1:]
			}
		}
		nextAfter := ""
		if len(rows) > limit {
			nextAfter = strings.TrimSpace(rows[limit-1].DocID)
			rows = rows[:limit]
		} else if afterID == "" && len(rows) >= limit {
			// First page filled the request — more may exist beyond this fetch.
			nextAfter = strings.TrimSpace(rows[len(rows)-1].DocID)
		}
		return rpcutil.RespondOK(req.ID, GroupwareApprovalsListResponse{
			Approvals:      rows,
			Folder:         folder,
			NextAfterDocID: nextAfter,
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
		comment := strings.TrimSpace(p.Comment)
		if decision == "reject" || decision == "반려" {
			comment = groupware.SanitizeApprovalComment(comment)
		} else {
			comment = ""
		}
		out, err := deps.Act(ctx, docID, decision, comment)
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

func groupwareApprovalsGet(deps GroupwareApprovalsDeps) rpcutil.HandlerFunc {
	type params struct {
		DocID string `json:"docId"`
		Title string `json:"title,omitempty"`
		// Folder hint from the list row (pending|done|cc|total) — lets the
		// reader skip the 4-folder scan on a cold open.
		Folder string `json:"folder,omitempty"`
	}
	return bindAuthenticated[params](func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
		docID := strings.TrimSpace(p.DocID)
		if docID == "" {
			return rpcerr.MissingParam("docId").Response(req.ID)
		}
		body, err := deps.Get(ctx, docID, strings.TrimSpace(p.Folder))
		if err != nil {
			return rpcerr.WrapDependencyFailed("get groupware approval", err).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, GroupwareApprovalGetResponse{
			DocID: docID,
			Title: strings.TrimSpace(p.Title),
			Body:  body,
		})
	})
}

func groupwareApprovalsAttachment(deps GroupwareApprovalsDeps) rpcutil.HandlerFunc {
	type params struct {
		DocID      string `json:"docId"`
		Attachment string `json:"attachment"`
	}
	return bindAuthenticated[params](func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
		docID := strings.TrimSpace(p.DocID)
		if docID == "" {
			return rpcerr.MissingParam("docId").Response(req.ID)
		}
		attachment := strings.TrimSpace(p.Attachment)
		if attachment == "" {
			return rpcerr.MissingParam("attachment").Response(req.ID)
		}
		text, err := deps.Attach(ctx, docID, attachment)
		if err != nil {
			return rpcerr.WrapDependencyFailed("get groupware approval attachment", err).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, GroupwareApprovalAttachmentResponse{
			DocID:      docID,
			Attachment: attachment,
			Text:       text,
		})
	})
}

func groupwareApprovalsAnalysisCached(deps GroupwareApprovalsDeps) rpcutil.HandlerFunc {
	type params struct {
		DocID string `json:"docId"`
	}
	return bindAuthenticated[params](func(_ context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
		docID := strings.TrimSpace(p.DocID)
		if docID == "" {
			return rpcerr.MissingParam("docId").Response(req.ID)
		}
		rec, err := deps.Cache.Load(docID)
		if err != nil {
			return rpcerr.WrapDependencyFailed("load approval analysis cache", err).Response(req.ID)
		}
		if rec == nil {
			return rpcutil.RespondOK(req.ID, GroupwareApprovalAnalysisOut{DocID: docID, Cached: false})
		}
		return rpcutil.RespondOK(req.ID, analysisOutFromRecord(rec, true))
	})
}

func groupwareApprovalsAnalyze(deps GroupwareApprovalsDeps) rpcutil.HandlerFunc {
	type params struct {
		DocID   string `json:"docId"`
		Title   string `json:"title,omitempty"`
		Force   bool   `json:"force,omitempty"`
		Drafter string `json:"drafter,omitempty"`
		Date    string `json:"date,omitempty"`
	}
	return bindAuthenticated[params](func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
		docID := strings.TrimSpace(p.DocID)
		if docID == "" {
			return rpcerr.MissingParam("docId").Response(req.ID)
		}
		if !p.Force && deps.Cache != nil {
			if rec, err := deps.Cache.Load(docID); err == nil && rec != nil {
				return rpcutil.RespondOK(req.ID, analysisOutFromRecord(rec, true))
			}
		}
		if deps.Analyze == nil {
			return rpcerr.Unavailable("approval analysis pipeline unavailable").Response(req.ID)
		}
		body, err := deps.Get(ctx, docID, "")
		if err != nil {
			return rpcerr.WrapDependencyFailed("get groupware approval for analyze", err).Response(req.ID)
		}
		title := strings.TrimSpace(p.Title)
		if title == "" {
			title = firstNonEmptyLine(body)
		}
		start := time.Now()
		analysis, importance, err := deps.Analyze(ctx, docID, title, strings.TrimSpace(p.Date), body)
		dur := time.Since(start)
		if err != nil {
			return rpcerr.WrapDependencyFailed("analyze groupware approval", err).Response(req.ID)
		}
		if strings.TrimSpace(analysis) == "" {
			return rpcerr.Unavailable("analysis returned empty result").Response(req.ID)
		}
		importance = normalizeImportance(importance, analysis)
		now := time.Now().UTC()
		rec := &groupware.ApprovalAnalysisRecord{
			DocID:         docID,
			Title:         title,
			Drafter:       strings.TrimSpace(p.Drafter),
			Date:          strings.TrimSpace(p.Date),
			Analysis:      analysis,
			Importance:    importance,
			ProjectFile:   normalizeProjectFile(analysis),
			DurationMs:    dur.Milliseconds(),
			PromptVersion: groupware.ApprovalAnalysisPromptVersion,
			CreatedAt:     now,
		}
		if deps.Cache != nil {
			_ = deps.Cache.Save(rec)
		}
		if deps.LogWiki != nil {
			deps.LogWiki(rec, body)
		}
		return rpcutil.RespondOK(req.ID, analysisOutFromRecord(rec, false))
	})
}

func groupwareERPList(deps GroupwareApprovalsDeps) rpcutil.HandlerFunc {
	type params struct {
		Area   string `json:"area"`
		Folder string `json:"folder,omitempty"`
		Query  string `json:"query,omitempty"`
		Limit  int    `json:"limit,omitempty"`
	}
	return bindAuthenticated[params](func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
		area := strings.TrimSpace(strings.ToLower(p.Area))
		if area == "" {
			return rpcerr.MissingParam("area").Response(req.ID)
		}
		limit := p.Limit
		if limit <= 0 {
			limit = defaultERPLimit
		}
		if limit > maxERPLimit {
			limit = maxERPLimit
		}
		text, err := deps.ListERP(ctx, area, strings.TrimSpace(p.Folder), strings.TrimSpace(p.Query), limit)
		if err != nil {
			return rpcerr.WrapDependencyFailed("list groupware erp", err).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, GroupwareERPListResponse{
			Area:   area,
			Folder: strings.TrimSpace(p.Folder),
			Query:  strings.TrimSpace(p.Query),
			Text:   text,
		})
	})
}

func groupwareBoardGet(deps GroupwareApprovalsDeps) rpcutil.HandlerFunc {
	type params struct {
		Query string `json:"query"`
	}
	return bindAuthenticated[params](func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
		query := strings.TrimSpace(p.Query)
		if query == "" {
			return rpcerr.MissingParam("query").Response(req.ID)
		}
		text, err := deps.ReadBoard(ctx, query)
		if err != nil {
			return rpcerr.WrapDependencyFailed("read groupware board post", err).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, GroupwareBoardPostResponse{Query: query, Text: text})
	})
}

func analysisOutFromRecord(rec *groupware.ApprovalAnalysisRecord, cached bool) GroupwareApprovalAnalysisOut {
	return GroupwareApprovalAnalysisOut{
		DocID:      rec.DocID,
		Title:      rec.Title,
		Drafter:    rec.Drafter,
		Date:       rec.Date,
		Analysis:   rec.Analysis,
		Importance: rec.Importance,
		DurationMs: rec.DurationMs,
		Cached:     cached,
		CreatedAt:  rec.CreatedAt,
	}
}

func approvalCanAct(doc groupware.ApprovalSummary) bool {
	if strings.EqualFold(strings.TrimSpace(doc.Folder), "pending") {
		return true
	}
	status := strings.TrimSpace(doc.Status)
	if status == "" {
		return false
	}
	if strings.EqualFold(status, "pending") {
		return true
	}
	// Amaranth pending box uses "미결"; some forms say "결재대기".
	return strings.Contains(status, "대기") || strings.Contains(status, "미결")
}

// approvalPendingIndex returns pending-box docs keyed by docId when the list
// folder is total/all (where status is blank). Best-effort — a pending-list
// failure leaves the map empty so rows fall back to approvalCanAct alone.
func approvalPendingIndex(ctx context.Context, deps GroupwareApprovalsDeps, folder string, limit int) map[string]groupware.ApprovalSummary {
	folder = strings.TrimSpace(strings.ToLower(folder))
	if folder != "total" && folder != "all" {
		return nil
	}
	pending, err := deps.List(ctx, "pending", limit)
	if err != nil || len(pending) == 0 {
		return nil
	}
	out := make(map[string]groupware.ApprovalSummary, len(pending))
	for _, doc := range pending {
		id := strings.TrimSpace(doc.DocID)
		if id == "" {
			continue
		}
		out[id] = doc
	}
	return out
}

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

func firstNonEmptyLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			if len(t) > 80 {
				return t[:80]
			}
			return t
		}
	}
	return ""
}

// normalizeProjectFile reports whether the analysis trailer asks to file to the
// project wiki. Only an explicit "yes" counts — missing/other → false (fail-closed).
func normalizeProjectFile(analysis string) bool {
	for _, line := range strings.Split(analysis, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToUpper(line), "PROJECT_FILE:") {
			part := strings.ToLower(strings.TrimSpace(line[len("PROJECT_FILE:"):]))
			return strings.HasPrefix(part, "yes")
		}
	}
	return false
}

func normalizeImportance(explicit, analysis string) string {
	v := strings.ToLower(strings.TrimSpace(explicit))
	switch v {
	case "urgent", "attention", "routine":
		return v
	}
	for _, line := range strings.Split(analysis, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToUpper(line), "IMPORTANCE:") {
			part := strings.TrimSpace(line[len("IMPORTANCE:"):])
			part = strings.ToLower(part)
			switch {
			case strings.HasPrefix(part, "urgent"):
				return "urgent"
			case strings.HasPrefix(part, "attention"):
				return "attention"
			case strings.HasPrefix(part, "routine"):
				return "routine"
			}
		}
	}
	return "attention"
}

// ApprovalAnalyzeSystemPrompt is exported for the server wiring / radar path.
func ApprovalAnalyzeSystemPrompt() string { return approvalAnalyzeSystemPrompt }

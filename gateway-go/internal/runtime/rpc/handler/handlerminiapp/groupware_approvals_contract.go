package handlerminiapp

import "time"

// GroupwareApprovalRow is one Amaranth 전자결재 document on the wire.
// CanAct is true when the operator can still 승인/반려 (미결).
//
//deneb:wire
type GroupwareApprovalRow struct {
	DocID   string `json:"docId"`
	Title   string `json:"title"`
	DocNo   string `json:"docNo,omitempty"`
	Drafter string `json:"drafter,omitempty"`
	Date    string `json:"date,omitempty"`
	Status  string `json:"status,omitempty"`
	Folder  string `json:"folder,omitempty"`
	CanAct  bool   `json:"canAct"`
}

// GroupwareApprovalsListResponse is the miniapp.groupware.approvals.list payload.
//
//deneb:wire
type GroupwareApprovalsListResponse struct {
	Approvals []GroupwareApprovalRow `json:"approvals"`
	Folder    string                 `json:"folder"`
}

// GroupwareApprovalActResponse is the miniapp.groupware.approvals.act payload.
//
//deneb:wire
type GroupwareApprovalActResponse struct {
	OK       bool   `json:"ok"`
	DocID    string `json:"docId"`
	Decision string `json:"decision"`
	Result   string `json:"result,omitempty"`
}

// GroupwareApprovalGetResponse is miniapp.groupware.approvals.get (document body).
//
//deneb:wire
type GroupwareApprovalGetResponse struct {
	DocID string `json:"docId"`
	Title string `json:"title,omitempty"`
	Body  string `json:"body"`
}

// GroupwareApprovalAnalysisOut is miniapp.groupware.approvals.analyze /
// analysis_cached — analysis card leads the detail UI (메일 패리티).
//
//deneb:wire
type GroupwareApprovalAnalysisOut struct {
	DocID      string    `json:"docId"`
	Title      string    `json:"title,omitempty"`
	Drafter    string    `json:"drafter,omitempty"`
	Date       string    `json:"date,omitempty"`
	Analysis   string    `json:"analysis"`
	Importance string    `json:"importance,omitempty"`
	DurationMs int64     `json:"durationMs"`
	Cached     bool      `json:"cached"`
	CreatedAt  time.Time `json:"createdAt"`
}

// GroupwareERPListResponse is miniapp.groupware.erp.list (read-only text snapshot).
//
//deneb:wire
type GroupwareERPListResponse struct {
	Area   string `json:"area"`
	Folder string `json:"folder,omitempty"`
	Query  string `json:"query,omitempty"`
	Text   string `json:"text"`
}

// GroupwareBoardPostResponse is miniapp.groupware.board.get (one 게시판 post body).
//
//deneb:wire
type GroupwareBoardPostResponse struct {
	Query string `json:"query"`
	Text  string `json:"text"`
}

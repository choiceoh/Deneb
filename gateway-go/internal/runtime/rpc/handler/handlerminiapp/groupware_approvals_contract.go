package handlerminiapp

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

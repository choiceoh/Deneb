package groupware

import (
	"strings"
	"testing"
)

func TestParseApprovalAttachmentList(t *testing.T) {
	body := strings.TrimSpace(`
[그룹웨어 전자결재 · 전체결재문서]
제목: 완도 관산포 프로젝트 관련 금전대여의 건
본문
금 액 …

첨부 (2건 · 내용 미열람)
필요한 파일만 groupware action=attachment, doc_id=99291, attachment=<번호 또는 파일명> 으로 읽기
1. 정종호-금전소비대차계약-260715.pdf · 2.0MB
2. 정종호-계약서 1단지.pdf · 1.1MB
`)
	got := ParseApprovalAttachmentList(body)
	if len(got) != 2 {
		t.Fatalf("len=%d want 2: %+v", len(got), got)
	}
	if got[0].Index != 1 || got[0].Name != "정종호-금전소비대차계약-260715.pdf" {
		t.Fatalf("first=%+v", got[0])
	}
	if got[1].Index != 2 || got[1].Name != "정종호-계약서 1단지.pdf" {
		t.Fatalf("second=%+v", got[1])
	}
}

func TestParseApprovalAttachmentList_Empty(t *testing.T) {
	if got := ParseApprovalAttachmentList("본문만 있고 첨부 없음"); got != nil {
		t.Fatalf("want nil, got %+v", got)
	}
}

func TestSelectApprovalAttachmentsForAnalysis_PreferBusiness(t *testing.T) {
	refs := []ApprovalAttachmentRef{
		{Index: 1, Name: "로고.png"},
		{Index: 2, Name: "정종호-계약서.pdf"},
		{Index: 3, Name: "회의메모.docx"},
		{Index: 4, Name: "지출영수증.jpg"},
		{Index: 5, Name: "발주서.xlsx"},
	}
	got := SelectApprovalAttachmentsForAnalysis(refs)
	if len(got) != 3 {
		t.Fatalf("len=%d want 3 (cap): %+v", len(got), got)
	}
	// Business docs first: 계약서, 발주서; then other docs: 회의메모.
	want := []string{"정종호-계약서.pdf", "발주서.xlsx", "회의메모.docx"}
	for i, name := range want {
		if got[i].Name != name {
			t.Fatalf("[%d]=%q want %q (all=%+v)", i, got[i].Name, name, got)
		}
	}
}

func TestSelectApprovalAttachmentsForAnalysis_ImageReceipt(t *testing.T) {
	refs := []ApprovalAttachmentRef{
		{Index: 1, Name: "로고.png"},
		{Index: 2, Name: "다과비_지출영수증.jpg"},
	}
	got := SelectApprovalAttachmentsForAnalysis(refs)
	if len(got) != 1 || got[0].Name != "다과비_지출영수증.jpg" {
		t.Fatalf("got=%+v", got)
	}
}

func TestApprovalAttachmentExtractBody(t *testing.T) {
	raw := "[그룹웨어 전자결재 · 선택 첨부]\ndocId: 1\n파일: a.pdf\n\n추출 본문\n계약금액 100만원"
	if got := approvalAttachmentExtractBody(raw); got != "계약금액 100만원" {
		t.Fatalf("got=%q", got)
	}
	if got := approvalAttachmentExtractBody("헤더\n\n(텍스트 추출 결과 없음)"); got != "" {
		t.Fatalf("empty note should drop, got=%q", got)
	}
}

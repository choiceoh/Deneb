package groupware

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
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

func TestSelectApprovalAttachmentsForQuestion_PrioritizesExplicitFourthFile(t *testing.T) {
	refs := []ApprovalAttachmentRef{
		{Index: 1, Name: "계약서.pdf"},
		{Index: 2, Name: "견적서.xlsx"},
		{Index: 3, Name: "회의록.docx"},
		{Index: 4, Name: "현장사진대지.pdf"},
		{Index: 5, Name: "참고자료.pdf"},
	}
	got := SelectApprovalAttachmentsForQuestion(refs, "4번 첨부 현장사진대지와 본문 수량이 같아?", 4)
	if len(got) != 4 || got[0].Index != 4 {
		t.Fatalf("explicit attachment was not prioritized: %+v", got)
	}
	seen := map[int]bool{}
	for _, ref := range got {
		if seen[ref.Index] {
			t.Fatalf("duplicate attachment: %+v", got)
		}
		seen[ref.Index] = true
	}
}

func TestSelectApprovalAttachmentsForQuestion_GenericAttachmentComparisonFillsListOrder(t *testing.T) {
	refs := []ApprovalAttachmentRef{
		{Index: 1, Name: "계약서.pdf"},
		{Index: 2, Name: "일반메모.txt"},
		{Index: 3, Name: "기타.bin"},
		{Index: 4, Name: "별첨.pdf"},
	}
	got := SelectApprovalAttachmentsForQuestion(refs, "첨부 파일들을 서로 비교해 줘", 4)
	if len(got) != 4 {
		t.Fatalf("generic attachment question omitted listed files: %+v", got)
	}
}

func TestSelectApprovalAttachmentsForQuestion_GeneralClauseQuestionFillsFourthFile(t *testing.T) {
	refs := []ApprovalAttachmentRef{
		{Index: 1, Name: "계약서-1.pdf"},
		{Index: 2, Name: "계약서-2.pdf"},
		{Index: 3, Name: "계약서-3.pdf"},
		{Index: 4, Name: "일반별지.pdf"},
	}
	got := SelectApprovalAttachmentsForQuestion(refs, "지체상금 얼마야?", 4)
	if len(got) != 4 || got[3].Index != 4 {
		t.Fatalf("general question omitted fourth listed attachment: %+v", got)
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

func TestSelectApprovalEvidence_RetainsQuestionMatchedMiddleWithinBound(t *testing.T) {
	text := "문서 머리\n" +
		strings.Repeat("초반", 9000) +
		"\n지체상금은 계약금의 30퍼센트로 한다.\n" +
		strings.Repeat("후반", 9000) +
		"\n문서 꼬리"

	got := SelectApprovalEvidence(text, "지체상금이 얼마야?", approvalAttachInjectRunes)
	if utf8.RuneCountInString(got) > approvalAttachInjectRunes {
		t.Fatalf("selected evidence = %d runes, want <= %d", utf8.RuneCountInString(got), approvalAttachInjectRunes)
	}
	for _, want := range []string{"문서 머리", "지체상금은 계약금의 30퍼센트", "문서 꼬리"} {
		if !strings.Contains(got, want) {
			t.Errorf("selected evidence missing %q", want)
		}
	}
}

func TestSelectApprovalEvidence_ManyTokenQuestionIsBoundedAndKeepsLongestRelevantTerm(t *testing.T) {
	var question strings.Builder
	for i := 0; i < 250; i++ {
		fmt.Fprintf(&question, "토큰%d ", i)
	}
	question.WriteString("초특급지체상금조항 ")
	question.WriteString(strings.Repeat("장", approvalEvidenceMaxQuestionTermRunes+20))

	terms := approvalEvidenceQuestionTerms(question.String())
	if len(terms) > approvalEvidenceMaxQuestionTerms {
		t.Fatalf("question terms = %d, want <= %d", len(terms), approvalEvidenceMaxQuestionTerms)
	}
	for _, term := range terms {
		if got := utf8.RuneCountInString(term); got > approvalEvidenceMaxQuestionTermRunes {
			t.Fatalf("question term = %d runes, want <= %d", got, approvalEvidenceMaxQuestionTermRunes)
		}
	}

	clause := "초특급지체상금조항은 계약금의 45퍼센트다."
	text := strings.Repeat("x", 2*1024*1024) + clause + strings.Repeat("y", 2*1024*1024)
	got := SelectApprovalEvidenceContext(context.Background(), text, question.String(), approvalAttachInjectRunes)
	if !strings.Contains(got, clause) {
		t.Fatalf("many-token selection omitted longest relevant clause")
	}
}

func TestSelectApprovalEvidenceContext_CanceledRequestStopsSelection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got := SelectApprovalEvidenceContext(
		ctx,
		strings.Repeat("x", 4*1024*1024),
		strings.Repeat("토큰 ", 400),
		approvalAttachInjectRunes,
	)
	if got != "" {
		t.Fatalf("canceled selection returned %d runes", utf8.RuneCountInString(got))
	}
}

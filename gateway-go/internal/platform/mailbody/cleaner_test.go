package mailbody

import (
	"strings"
	"testing"
)

func TestCleanForDisplay_KeepsRawSeparateMetadata(t *testing.T) {
	body := strings.Join([]string{
		"안녕하세요. 해남 인버터 배치 수량은 기존 안대로 유지하겠습니다.",
		"OCI 인터뷰 전까지 수정 도면만 다시 공유 부탁드립니다.",
		"",
		"감사합니다",
		"홍길동 드림",
	}, "\n")

	got := CleanForDisplay(body)
	if !strings.Contains(got.Body, "OCI 인터뷰 전까지 수정 도면") {
		t.Fatalf("business body missing:\n%s", got.Body)
	}
	for _, gone := range []string{"감사합니다", "홍길동 드림"} {
		if strings.Contains(got.Body, gone) {
			t.Fatalf("display noise leaked %q:\n%s", gone, got.Body)
		}
	}
	if len(got.HiddenBlocks) == 0 || got.CleanRunes >= got.RawRunes {
		t.Fatalf("expected hidden-block metadata, got %+v", got)
	}
}

func TestCleanForDisplay_DoesNotStripTinyHumanReply(t *testing.T) {
	body := "검토했습니다.\n\n감사합니다"
	got := CleanForDisplay(body)
	if !strings.Contains(got.Body, "감사합니다") {
		t.Fatalf("tiny reply closing should stay visible:\n%s", got.Body)
	}
	if len(got.HiddenBlocks) != 0 {
		t.Fatalf("tiny reply should not report hidden blocks: %+v", got.HiddenBlocks)
	}
}

func TestCleanForDisplay_CutsLongReplyHistory(t *testing.T) {
	body := strings.Join([]string{
		"김대희 과장님,",
		"요청하신 기아 화성 모듈 납기 변경안은 6월 28일 입고분만 96장으로 조정하겠습니다.",
		"수정 발주서는 오늘 중 공유드리겠습니다.",
		"",
		"On Wed, Jun 17, 2026 at 10:31 AM 김대희 <kdh@example.com> wrote:",
		"> 이전 회신입니다.",
		"> From: 김세미 <semi@example.com>",
		"> Sent: Tuesday, June 16, 2026 5:14 PM",
		"> To: 김대희 <kdh@example.com>",
		"> Subject: RE: 기아 화성 모듈 납기",
	}, "\n")

	got := CleanForDisplay(body)
	if !strings.Contains(got.Body, "6월 28일 입고분만 96장") {
		t.Fatalf("latest body missing:\n%s", got.Body)
	}
	for _, gone := range []string{"On Wed", "이전 회신", "김세미"} {
		if strings.Contains(got.Body, gone) {
			t.Fatalf("reply history leaked %q:\n%s", gone, got.Body)
		}
	}
	if len(got.HiddenBlocks) == 0 || got.HiddenBlocks[len(got.HiddenBlocks)-1].Kind != "history" {
		t.Fatalf("expected history hidden block, got %+v", got.HiddenBlocks)
	}
}

func TestCleanForDisplay_CutsChineseOutlookHeaderBlock(t *testing.T) {
	body := strings.Join([]string{
		"Please confirm the accessory installation drawing by Friday.",
		"The latest revision should be used for the Bigeum 154kV package.",
		"",
		"发件人: Christina Gu <christina@example.com>",
		"发送时间: 2026年6月16日 18:42",
		"收件人: Kang Minsoo <minsoo@example.com>",
		"主题: RE: Accessory Installation instructions",
		"",
		"Old thread body that should not be the default reading surface.",
	}, "\n")

	got := CleanForDisplay(body)
	if !strings.Contains(got.Body, "latest revision") {
		t.Fatalf("latest body missing:\n%s", got.Body)
	}
	for _, gone := range []string{"发件人", "Old thread body"} {
		if strings.Contains(got.Body, gone) {
			t.Fatalf("quoted history leaked %q:\n%s", gone, got.Body)
		}
	}
}

func TestCleanForDisplay_DoesNotCutSingleBusinessFromLine(t *testing.T) {
	body := strings.Join([]string{
		"아래 조건으로 검토 부탁드립니다.",
		"From: 현장 계량기 데이터 기준으로 산출했습니다.",
		"6월 발전량 산식과 단가 가정은 첨부 표와 같습니다.",
	}, "\n")

	got := CleanForDisplay(body)
	if got.Body != body {
		t.Fatalf("single business From line should stay intact:\n%s", got.Body)
	}
	if len(got.HiddenBlocks) != 0 {
		t.Fatalf("unexpected hidden blocks: %+v", got.HiddenBlocks)
	}
}

func TestCleanForDisplay_StripsLeadingLargeAttachmentMetadata(t *testing.T) {
	body := strings.Join([]string{
		"대용량 파일첨부 2개",
		"(22.13 MB)",
		"다운로드 기간 : 2026-02-26 ~ 2026-03-28",
		"(대용량 첨부 파일은 30일간 보관)",
		"현장도면.zip",
		"(10.89 MB)",
		"팀장님 안녕하십니까.",
		"수정된 가배치도 송부드립니다.",
		"전기실 및 한전전주 위치를 표기했습니다.",
		"",
		"확인 부탁드립니다.",
		"감사합니다.",
	}, "\n")

	got := CleanForDisplay(body)
	for _, gone := range []string{"대용량 파일첨부", "다운로드 기간", "현장도면.zip"} {
		if strings.Contains(got.Body, gone) {
			t.Fatalf("attachment metadata leaked %q:\n%s", gone, got.Body)
		}
	}
	for _, want := range []string{"수정된 가배치도", "전기실 및 한전전주", "확인 부탁드립니다"} {
		if !strings.Contains(got.Body, want) {
			t.Fatalf("business body missing %q:\n%s", want, got.Body)
		}
	}
	if len(got.HiddenBlocks) == 0 || got.HiddenBlocks[0].Kind != "attachment" {
		t.Fatalf("expected attachment hidden block, got %+v", got.HiddenBlocks)
	}
}

func TestCleanForDisplay_StripsAttachmentThenForwardHeader(t *testing.T) {
	body := strings.Join([]string{
		"대용량 파일첨부 1개",
		"(15.94 MB)",
		"다운로드 기간 : 2026-02-26 ~ 2026-03-28",
		"(대용량 첨부 파일은 30일간 보관)",
		"투자자료.pdf",
		"(15.94 MB)",
		"<hr dze_content_sep=\"\">",
		"보내는사람: 최원철 <choi@example.com>",
		"받는사람 : 본부장 <lead@example.com>",
		"참조 : team@example.com",
		"보낸 날짜 : 2026-02-12 11:37",
		"제목 : [삼성증권] 태양광 펀드 투자 관련 자료 송부 건",
		"<meta http-equiv=\"Content-Type\" content=\"text/html; charset=UTF-8\">",
		"본부장님, 안녕하십니까.",
		"삼성증권 최원철입니다.",
		"유선 상 말씀 드린 블라인드펀드 투자자 모집 관련 자료 송부 드리오니 검토 부탁 드립니다.",
		"자세한 내용은 하기 메일 및 첨부 자료 참고 부탁 드리며,",
		"문의사항 있으시면 언제든지 말씀 부탁 드립니다.",
		"감사합니다.",
		"최원철 드림",
		"--------- Original Message ---------",
		"Sender : 김창경 <kim@example.com>",
		"Date : 2026-02-12 10:27",
		"Title : 재생 에너지 리츠 관련 자료 송부",
		"이전 메일 본문입니다.",
	}, "\n")

	got := CleanForDisplay(body)
	for _, want := range []string{"본부장님", "블라인드펀드", "검토 부탁"} {
		if !strings.Contains(got.Body, want) {
			t.Fatalf("forwarded body missing %q:\n%s", want, got.Body)
		}
	}
	for _, gone := range []string{"대용량 파일첨부", "<hr", "<meta", "보내는사람", "Original Message", "이전 메일 본문"} {
		if strings.Contains(got.Body, gone) {
			t.Fatalf("forward header/history leaked %q:\n%s", gone, got.Body)
		}
	}
}

func TestCleanForDisplay_CutsForwardedHistoryThenSignature(t *testing.T) {
	body := strings.Join([]string{
		"안녕하세요.",
		"현대차그룹향 모듈 공급사 등록에 참여하고자 합니다.",
		"출력보증과 납기 조건은 아래와 같습니다.",
		"",
		"감사합니다.",
		"김성훈 배상",
		"기획조정실/3팀",
		"김성훈 이사",
		"M: 010-3490-9563",
		"E-mail: kim@example.com",
		"<hr dze_content_sep=\"\">",
		"보내는사람: Jay Yu <jay@example.com>",
		"받는사람 : 김성훈 <kim@example.com>",
		"보낸 날짜 : 2026-04-10 18:54",
		"제목 : 이전 회신",
		"이전 회신 본문입니다.",
	}, "\n")

	got := CleanForDisplay(body)
	for _, want := range []string{"모듈 공급사 등록", "출력보증과 납기"} {
		if !strings.Contains(got.Body, want) {
			t.Fatalf("latest body missing %q:\n%s", want, got.Body)
		}
	}
	for _, gone := range []string{"기획조정실", "E-mail", "보내는사람", "이전 회신"} {
		if strings.Contains(got.Body, gone) {
			t.Fatalf("noise leaked %q:\n%s", gone, got.Body)
		}
	}
	var sawHistory, sawSignature bool
	for _, block := range got.HiddenBlocks {
		if block.Kind == "history" {
			sawHistory = true
		}
		if block.Kind == "signature" {
			sawSignature = true
		}
	}
	if !sawHistory || !sawSignature {
		t.Fatalf("expected history and signature hidden blocks, got %+v", got.HiddenBlocks)
	}
}

func TestCleanForDisplay_StripsZeroWidthOnlyTailLines(t *testing.T) {
	body := "확인 부탁드립니다.\n감사합니다. 끝.\n\u200b\n\u200b"
	got := CleanForDisplay(body)
	if strings.Contains(got.Body, "\u200b") {
		t.Fatalf("zero-width tail leaked:\n%q", got.Body)
	}
}

func TestCleanForDisplay_StripsLeadingForwardHeaderBlock(t *testing.T) {
	body := strings.Join([]string{
		"========================================",
		"보낸사람 : 김대희 <kim@example.com>",
		"받는사람 : 임용빈 책임매니저 <lim@example.com>",
		"참조 : 오선택 <ost@example.com>",
		"보낸일시 : 2026-03-16 18:34",
		"제목 : [탑솔라(주)] 기아 AL 화성 별관 태양광 가배치도 송부의 件",
		"책임님 안녕하십니까.",
		"태양광 가배치도 송부드리오니 확인하여 주시기 바랍니다.",
		"현재 공급받으실 수 있는 모듈은 한화 640Wp인 것으로 파악됩니다.",
		"관련 전기도면 요청드리겠습니다.",
		"",
		"기타 문의사항은 연락주시기 바랍니다.",
		"감사합니다.",
	}, "\n")

	got := CleanForDisplay(body)
	for _, gone := range []string{"보낸사람", "받는사람", "보낸일시", "제목 :"} {
		if strings.Contains(got.Body, gone) {
			t.Fatalf("forward header leaked %q:\n%s", gone, got.Body)
		}
	}
	for _, want := range []string{"태양광 가배치도", "한화 640Wp", "전기도면"} {
		if !strings.Contains(got.Body, want) {
			t.Fatalf("forwarded body missing %q:\n%s", want, got.Body)
		}
	}
}

func TestCleanForDisplay_KeepsMeetingJoinDetailsBeforeSignature(t *testing.T) {
	body := strings.Join([]string{
		"Dear All",
		"Kindly join below meeting 2pm Korean time to align with pending points.",
		"Thank you!",
		"Microsoft Teams meeting",
		"Join: https://teams.microsoft.com/meet/example",
		"Meeting ID: 433 544 474 487 50",
		"Passcode: 34G5bb2W",
		"Best Regards,",
		"Christina Gu",
		"Project Execution Manager",
		"Marine Industry Group",
		"Email: christina@example.com Website: www.example.com",
		"This message is confidential and intended only for the recipient.",
		"From: Previous Sender <prev@example.com>",
		"Sent: Monday, June 1, 2026 10:00 AM",
		"To: Team <team@example.com>",
		"Subject: Previous thread",
		"Old message body.",
	}, "\n")

	got := CleanForDisplay(body)
	for _, want := range []string{"Microsoft Teams meeting", "Join: https://teams.microsoft.com", "Meeting ID", "Passcode"} {
		if !strings.Contains(got.Body, want) {
			t.Fatalf("meeting detail missing %q:\n%s", want, got.Body)
		}
	}
	for _, gone := range []string{"Project Execution Manager", "This message is confidential", "Previous thread"} {
		if strings.Contains(got.Body, gone) {
			t.Fatalf("noise leaked %q:\n%s", gone, got.Body)
		}
	}
}

func TestCleanForDisplay_KeepsBusinessLinksInBody(t *testing.T) {
	body := strings.Join([]string{
		"Hi builders,",
		"The old model will be discontinued. Please migrate to the new model.",
		"Action Required:",
		"1. Update the model parameter.",
		"2. Test your integration.",
		"Guide: https://platform.example.com/docs/migration",
		"Affected Model List:",
		"* model-a",
		"* model-b",
		"Feedback & Support",
		"Email: support@example.com",
		"Copyright 2026 Example. All rights reserved.",
		"You can unsubscribe at https://example.com/unsubscribe",
	}, "\n")

	got := CleanForDisplay(body)
	for _, want := range []string{"Action Required", "https://platform.example.com/docs/migration", "* model-a"} {
		if !strings.Contains(got.Body, want) {
			t.Fatalf("business link body missing %q:\n%s", want, got.Body)
		}
	}
	for _, gone := range []string{"support@example.com", "unsubscribe"} {
		if strings.Contains(got.Body, gone) {
			t.Fatalf("footer noise leaked %q:\n%s", gone, got.Body)
		}
	}
}

func TestCleanForDisplay_KeepsForwardedBodyWhenWrapperIsThin(t *testing.T) {
	body := strings.Join([]string{
		"앞서 전달드린 메일에 추가로 현설회의록 들어가있는 메일 송부 드립니다.",
		"감사합니다.",
		"<hr dze_content_sep=\"\">",
		"보내는사람: 정한구 <jung@example.com>",
		"받는사람 : 업체 <vendor@example.com>",
		"보낸 날짜 : 2026-05-15 14:17",
		"제목 : RE: 태양광 모듈 현장설명회",
		"<meta http-equiv=\"Content-Type\" content=\"text/html; charset=ks_c_5601-1987\">",
		"안녕하십니까.",
		"수정 사양본 공유드립니다.",
		"635Wp 이상 모델로 견적 제출 부탁드립니다.",
		"견적 및 회의록 서명본 회신 바랍니다.",
		"",
		"정한구 드림.",
		"화성에너지관리팀 매니저",
		"E jung@example.com",
	}, "\n")

	got := CleanForDisplay(body)
	for _, want := range []string{"수정 사양본", "635Wp", "회의록 서명본"} {
		if !strings.Contains(got.Body, want) {
			t.Fatalf("forwarded body missing %q:\n%s", want, got.Body)
		}
	}
	for _, gone := range []string{"보내는사람", "앞서 전달드린", "jung@example.com"} {
		if strings.Contains(got.Body, gone) {
			t.Fatalf("forward wrapper/header leaked %q:\n%s", gone, got.Body)
		}
	}
}

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
			t.Fatalf("meeting detail missing %q hidden=%v:\n%s", want, got.HiddenBlocks, got.Body)
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

func TestCleanForDisplay_KeepsForwardedBodyAfterSignatureOnlyPrefix(t *testing.T) {
	body := strings.Join([]string{
		"(유)남도에코에너지",
		"이 시 연 주임",
		"광주광역시 북구 첨단연신로29번길 26, 1층",
		"T_062-571-0300 F_062-973-9877",
		"M_010-1111-2222 sara@example.com",
		"Namdoeco Energy Co.,LTD",
		"Lee, Si-Yeon (Sara) / Senior Clerk",
		"#1, 26, Cheomdanyeonsin-ro 29beon-gil, Buk-gu, Gwangju, Korea",
		"<hr dze_content_sep=\"\">",
		"보내는사람: 이시연 <sara@example.com>",
		"받는사람 : Alan Zhang <alan@example.com>",
		"보낸 날짜 : 2026-02-17 14:43",
		"제목 : Re: Project cable request",
		"Dear Alan,",
		"Please find attached the signed contract addendum reflecting the revised quantity.",
		"Kindly ask you sign and send it back to us.",
		"Best,",
		"Sara.",
		"(유)남도에코에너지",
		"이 시 연 주임",
		"T_062-571-0300 F_062-973-9877",
	}, "\n")

	got := CleanForDisplay(body)
	for _, want := range []string{"Dear Alan", "signed contract addendum", "revised quantity"} {
		if !strings.Contains(got.Body, want) {
			t.Fatalf("forwarded body missing %q:\n%s", want, got.Body)
		}
	}
	for _, gone := range []string{"보내는사람", "Cheomdanyeonsin", "T_062"} {
		if strings.Contains(got.Body, gone) {
			t.Fatalf("signature/header leaked %q:\n%s", gone, got.Body)
		}
	}
}

func TestCleanForDisplay_KeepsForwardedBodyAfterThinWrapperSignature(t *testing.T) {
	body := strings.Join([]string{
		"더존 아마란스 API 목록 공유 드립니다.",
		"기획조정실/3팀",
		"김성훈 이사",
		"M: 010-1111-2222",
		"E-mail: kim@example.com",
		"========================================",
		"보낸사람 : 김기성 <kim@example.com>",
		"받은사람 : 김성훈 <director@example.com>",
		"보낸일시 : 2026-04-23 16:00",
		"제목 : [더존비즈온] 더존 아마란스 10 API목록 안내",
		"수신 : 탑솔라(주) 김성훈 이사님",
		"안녕하십니까 이사님",
		"어제 미팅시 요청주신 Amaranth 10 표준 API 목록 첨부와 같이 보내드립니다.",
		"현재 필요하신 표준 API 목록은",
		"1. UC(그룹웨어 기본) 표준 API",
		"2. 물류 공통 표준API",
		"3. 영업관리 표준API",
		"4. 구매자재 표준API",
		"내용 확인 부탁드립니다.",
	}, "\n")

	got := CleanForDisplay(body)
	for _, want := range []string{"Amaranth 10 표준 API", "물류 공통", "구매자재"} {
		if !strings.Contains(got.Body, want) {
			t.Fatalf("forwarded body missing %q:\n%s", want, got.Body)
		}
	}
	for _, gone := range []string{"목록 공유 드립니다", "보낸사람", "기획조정실/3팀", "M: 010"} {
		if strings.Contains(got.Body, gone) {
			t.Fatalf("wrapper/signature/header leaked %q:\n%s", gone, got.Body)
		}
	}
}

func TestCleanForDisplay_StripsLooseLargeAttachmentMetadata(t *testing.T) {
	body := strings.Join([]string{
		"대용량 첨부 5개 48MB",
		"20260420 구조계산서.pdf 5988497 ~ 2026/06/07",
		"건물도면.zip 29420690 ~ 2026/06/07",
		"기한이 있는 파일은 30일 보관 / 100회 다운로드 가능",
		"고건대리님",
		"안녕하세요 제이티에너지 임은진 차장입니다",
		"어제 요청 주셨던 부산8 3개소의 구조검토 보고서 자료 송부 드립니다.",
		"1. 인천은성전기 - 구조계산서, 구조도면 첨부",
		"2. 메탈스타 - 구조계산서, 건물도면",
		"3. 해비코 - 구조계산서, 건물도면",
		"확인을 부탁 드리며, 다음주 실사 일정 주시면 최종 날짜 조율해서 전달 드리겠습니다",
	}, "\n")

	got := CleanForDisplay(body)
	for _, want := range []string{"구조검토 보고서", "인천은성전기", "다음주 실사 일정"} {
		if !strings.Contains(got.Body, want) {
			t.Fatalf("business body missing %q:\n%s", want, got.Body)
		}
	}
	for _, gone := range []string{"대용량 첨부", "구조계산서.pdf", "100회 다운로드"} {
		if strings.Contains(got.Body, gone) {
			t.Fatalf("attachment metadata leaked %q:\n%s", gone, got.Body)
		}
	}
}

func TestCleanForDisplay_DoesNotTreatNumberedAddressAsSignature(t *testing.T) {
	body := strings.Join([]string{
		"대용량 파일첨부 2개",
		"(68.77 MB)",
		"다운로드 기간 : 2026-02-26 ~ 2026-03-28",
		"(대용량 첨부 파일은 30일간 보관)",
		"건축도면.zip",
		"(65.74 MB)",
		"팀장님 안녕하십니까.",
		"탑솔라 기획조정실 김대희과장입니다.",
		"광명역 B환승 주차장 가배치 요청 드립니다.",
		"1. 주소 : 경기 광명시 덕안로 16 광명역B주차장",
		"2. 사용모듈 : 현대 645Wp",
		"3. 특이사항",
		"가. 지붕위에 디자인 구조체 존재. 음영 피해야 될 것으로 판단됨",
		"나. 주차장 측면도 함께 요청",
		"다. 필요시 주차면 감소시킬 수 있으며, 감소면수 표기 요청",
		"기타 문의사항은 연락주시기 바랍니다.",
		"감사합니다.",
	}, "\n")

	got := CleanForDisplay(body)
	for _, want := range []string{"1. 주소", "현대 645Wp", "주차면 감소"} {
		if !strings.Contains(got.Body, want) {
			t.Fatalf("numbered business detail missing %q:\n%s", want, got.Body)
		}
	}
	for _, gone := range []string{"대용량 파일첨부", "건축도면.zip"} {
		if strings.Contains(got.Body, gone) {
			t.Fatalf("attachment metadata leaked %q:\n%s", gone, got.Body)
		}
	}
}

func TestCleanForDisplay_DoesNotTreatRoleProseAsSignature(t *testing.T) {
	body := strings.Join([]string{
		"지난 4월 09일 노스랜드파워에서 계약서 초안을 보내줘 11:00~12:30분까지",
		"미래사업실 김상겸 전무 등 3명과 법무실장 백종관 홍상호 회계사님이 참석하여 영문본과 한글 번역본으로 회의를 진행한바 있습니다",
		"한글 번역본으로 회의를 진행하였으나 우리 탑솔라에서 인식하고 있는 의사 표현이 맞는지 여부에 대해 논쟁이 있어 노스랜드측에",
		"한글 번역본을 요청하였으나 MOA계약서 작성 당시 협의는 영문으로 진행하고 계약서 완결본에 대한 서명은 한글본과 영문본으로 기재 하기로 약속이 되었다고",
		"노스랜드 한국대표인 정세현에게 회신이 왔습니다",
		"결론은 주식양도양수 계약서에 따른 한글본은 제공할수 없다고 회신이 온것입니다",
		"우리가 변호사를 선임하여 영문본 계약 검토를 진행하던가 또는 문구 변경시 마다 변호인에게 의뢰를 해야 하는 상황입니다",
		"최종 완성본에 따른 한글과 영문 계약서를 작성하는것은 상호 동의하고 있읍니다",
		"이와 관련한 의견을 구합니다",
		"탑솔라(주)",
		"미래사업실 / 김상겸 전무",
		"광주광역시 북구 첨단연신로 30번길 41",
		"HP 010-1111-2222",
		"E mail kim@example.com",
	}, "\n")

	got := CleanForDisplay(body)
	for _, want := range []string{"한글 번역본", "제공할수 없다고", "의견을 구합니다"} {
		if !strings.Contains(got.Body, want) {
			t.Fatalf("business prose missing %q:\n%s", want, got.Body)
		}
	}
	for _, gone := range []string{"미래사업실 / 김상겸", "HP 010"} {
		if strings.Contains(got.Body, gone) {
			t.Fatalf("signature leaked %q:\n%s", gone, got.Body)
		}
	}
}

func TestCleanForDisplay_DoesNotTreatIncidentRoleProseAsSignature(t *testing.T) {
	body := strings.Join([]string{
		"수신: 기획조정실 김세미 과장님",
		"발신: 탑솔라(주) / 신안 비금태양광 EPC 전기팀 / 강민수 과장",
		"안녕하십니까.",
		"비금 현장 탑솔라(주) 강민수 과장입니다.",
		"비금 설치 모듈 - 진코635Wp 모듈 관련하여",
		"모듈 - 모듈 간 어레이 공사 진행 중",
		"1직렬 중간 지점 모듈-모듈 MC4 연결부 10곳 이상에서 현재 화재가 발생하여 MC4 화재가 발생한 상황입니다.",
		"현장 내에서는 정확한 문제 분석이 힘들며, 추측으로는 MC4커넥터의 불량으로 1차적 판단 상황입니다.",
		"첨부 사진 자료를 확인 바라며",
		"진코솔라측 기술적 담당자 현장 방문 및 문제 분석이 시급히 필요하오니 진코솔라측에 협조 요청드립니다.",
		"감사합니다.",
		"탑솔라(주) / 신안 비금태양광 / EPC 전기팀 강민수 과장",
		"광주광역시 북구 첨단연신로30번길 41",
		"T: 062-111-2222 F: 062-333-4444",
		"M: 010-1111-2222 E-mail: kang@example.com",
	}, "\n")

	got := CleanForDisplay(body)
	for _, want := range []string{"MC4 화재", "문제 분석", "협조 요청"} {
		if !strings.Contains(got.Body, want) {
			t.Fatalf("incident body missing %q:\n%s", want, got.Body)
		}
	}
	for _, gone := range []string{"E-mail", "첨단연신로30번길"} {
		if strings.Contains(got.Body, gone) {
			t.Fatalf("signature leaked %q:\n%s", gone, got.Body)
		}
	}
}

func TestCleanForDisplay_DoesNotTreatBulletScheduleAsSignature(t *testing.T) {
	body := strings.Join([]string{
		"수신 : 김성훈 이사님께.",
		"안녕하세요, 이사님 트리나솔라 임형철 입니다.",
		"차주 상하이 SNEC 전시회 기간 중 6월 4일 예정으로 진행될 협약식 및 석식 일정 정보 아래와 같이 공유 드리오니",
		"참고하여 주시기 바랍니다.",
		"1. 협약식 일정",
		"1)일정 : 6월 4일 : 15:00~16:00",
		"2)참석자 :",
		"- Todd Li (APAC & MEA 대표이사)",
		"- Lina (Korea,Japan,Israel sales head, APMEA)",
		"- Hank Zhang (Korea regional sales head, APMEA)",
		"- 임형철 부장 (Korea sales manager, APMEA)",
		"3)장소 :",
		"- 협약식 장소는 InterContinental 호텔 회의실(2층)에서 진행될 예정입니다.",
		"2. 저녁 석식일정.",
		"- 일시 : 6월 4일 18:00~",
		"- 주소 :",
		"1) 중문 : 上海宫宴（北京西路1485号，静安区）",
		"2) 영문 : 1485 West Beijing Road.",
		"차주 일정 관련하여 궁금하신 사항에 언제든지 연락 주시기 바랍니다.",
		"감사합니다.",
		"임형철 배상",
	}, "\n")

	got := CleanForDisplay(body)
	for _, want := range []string{"Todd Li", "InterContinental", "저녁 석식일정", "1485 West Beijing Road"} {
		if !strings.Contains(got.Body, want) {
			t.Fatalf("schedule detail missing %q:\n%s", want, got.Body)
		}
	}
	if strings.Contains(got.Body, "임형철 배상") {
		t.Fatalf("closing signature leaked:\n%s", got.Body)
	}
}

func TestCleanForDisplay_KeepsForwardedBodyAfterHTMLSignaturePrefix(t *testing.T) {
	body := strings.Join([]string{
		"화웨이 자동출력제어 기기 공유드립니다!",
		"고 건",
		"<span ng-if=\"showField('title')\">기획조정실 대리",
		"Mobile:<span ng-if=\"showField('mobile')\"> 010-1111-2222",
		"Email:<span ng-if=\"showField('email')\"> go@example.com",
		"<span ng-if=\"showField('address1')\">광주광역시 북구 첨단연신로 30번길 41",
		"<hr dze_content_sep=\"\">",
		"보내는사람: Kim Noh Young <noah@example.com>",
		"받는사람 : 고건 <go@example.com>",
		"보낸 날짜 : 2026-03-13 11:01",
		"제목 : [한국화웨이] Zero export 관련 자료 송부의 건",
		"<meta http-equiv=\"Content-Type\" content=\"text/html; charset=ks_c_5601-1987\">",
		"안녕하세요 탑솔라 고건 대리님.",
		"말씀드린 내용, 하기와 첨부파일 확인 부탁드립니다.",
		"SmartLogger(화웨이 구매) 구매 및 Load 데이터 수집용 파워미터는 반드시 설치가 필요하오니 이 부분 참고 부탁드립니다.",
		"Zero export 구조",
		"대부분 자가소비형 on-site PPA 현장이 많기 때문에 원격감시제어를 포함하는 경우와 역송방지 시스템만 구현하는 경우로 나뉩니다.",
	}, "\n")

	got := CleanForDisplay(body)
	for _, want := range []string{"SmartLogger", "Zero export", "역송방지"} {
		if !strings.Contains(got.Body, want) {
			t.Fatalf("forwarded body missing %q:\n%s", want, got.Body)
		}
	}
	for _, gone := range []string{"화웨이 자동출력제어", "showField", "보내는사람"} {
		if strings.Contains(got.Body, gone) {
			t.Fatalf("wrapper/header leaked %q:\n%s", gone, got.Body)
		}
	}
}

func TestCleanForDisplay_StripsForwardedAttachmentMetadataAfterReplyHeader(t *testing.T) {
	body := strings.Join([]string{
		"이사님",
		"자료 잘 받았습니다",
		"안현철 드림",
		"________________________________",
		"보낸 사람: 김성훈 <kim@example.com>",
		"보낸 날짜: Thursday, February 12, 2026 8:52:04 AM",
		"받는 사람: Hyunchul An <an@example.com>",
		"제목: 보해매실농원 관련 법률자문_실무자료 등_탑솔라",
		"대용량 파일첨부 6개 (32.45 MB) 다운로드 기간 : 2026-02-12 ~ 2026-03-13",
		"(대용량 첨부 파일은 30일간 보관)",
		"*   법률자문요청_20260212_최종본.docx (78.52 KB)<https://example.com/doc>",
		"*   첨부문서 합본.zip (31.29 MB)<https://example.com/zip>",
		"안녕하세요, 안현철 변호사님.",
		"탑솔라 주식회사 김성훈 이사입니다.",
		"당사 법무실에서 보해매실농원 건과 관련하여 법률자문을 요청드린 것으로 알고 있습니다.",
		"자문 검토에 참고하실 수 있도록 관련 계약서, 합의서, 이메일 교신 내역 등 실무 자료를 첨부하여 송부드립니다.",
		"검토 과정에서 실무적으로 확인이 필요하신 사항이 있으시면 아래 연락처로 편하게 연락 주시기 바랍니다.",
		"감사합니다.",
		"김성훈 드림",
	}, "\n")

	got := CleanForDisplay(body)
	for _, want := range []string{"법률자문", "실무 자료", "확인이 필요"} {
		if !strings.Contains(got.Body, want) {
			t.Fatalf("forwarded business body missing %q:\n%s", want, got.Body)
		}
	}
	for _, gone := range []string{"대용량 파일첨부", "첨부문서 합본.zip", "보낸 사람"} {
		if strings.Contains(got.Body, gone) {
			t.Fatalf("forwarded metadata leaked %q:\n%s", gone, got.Body)
		}
	}
}

func TestCleanForDisplay_KeepsFinancialReceiptDetails(t *testing.T) {
	body := strings.Join([]string{
		"Anthropic, PBC  (<https://example.com>)",
		"Anthropic, PBC",
		"Credit note from Anthropic, PBC $25.00 Issued May 2, 2026 (invoice illustration [<url>] Download invoice (<url>) Download credit note (<url>) Credit note number WLGPLVNK-0021-CN-01 Invoice number WLGPLVNK-0021 Credit to astra7471@gmail.com View updated invoice (https://invoice.stripe.com/example)",
		"Credit note # WLGPLVNK-0021-CN-01 Credit - Other $25.00 Refund issued - American Express - 6216 $25.00 Total credit $25.00 Questions? Visit our support site.",
		"Powered by stripe logo",
	}, "\n")

	got := CleanForDisplay(body)
	for _, want := range []string{"Credit note", "$25.00", "Total credit"} {
		if !strings.Contains(got.Body, want) {
			t.Fatalf("receipt detail missing %q:\n%s", want, got.Body)
		}
	}
	if strings.Contains(got.Body, "stripe logo") {
		t.Fatalf("trailing logo leaked:\n%s", got.Body)
	}
	for _, gone := range []string{"Anthropic, PBC  (<https://example.com>)", "Download invoice", "View updated invoice", "Visit our support site"} {
		if strings.Contains(got.Body, gone) {
			t.Fatalf("receipt boilerplate leaked %q:\n%s", gone, got.Body)
		}
	}
}

func TestCleanForDisplay_StripsExpandedEnglishSignature(t *testing.T) {
	body := strings.Join([]string{
		"Nice weekend. Fred again.",
		"ENCLOSED OFFICIAL TUV test report and UV certificate for your info and record.",
		"Hi Sara, Jin Yun and Park,",
		"Could you kindly confirm the enclosed draft TUV test report to issue the official test report.",
		"Looking forward to long-term cooperation with you.",
		"Best Regards & Thanks so much",
		"Fred Lee | Overseas Manager",
		"JOCA Special Cable (Shanghai) Co., Ltd.",
		"Wuxi JOCA Cable Technology Group Co., Ltd.",
		"Mobile: +86 111 2222 3333",
		"E-mail: fred@example.com",
	}, "\n")

	got := CleanForDisplay(body)
	for _, want := range []string{"TUV test report", "confirm the enclosed"} {
		if !strings.Contains(got.Body, want) {
			t.Fatalf("business body missing %q:\n%s", want, got.Body)
		}
	}
	for _, gone := range []string{"Best Regards", "Overseas Manager", "Mobile:"} {
		if strings.Contains(got.Body, gone) {
			t.Fatalf("signature leaked %q:\n%s", gone, got.Body)
		}
	}
}

func TestCleanForDisplay_StripsEnglishProfessionalSignatureAfterShortBody(t *testing.T) {
	body := strings.Join([]string{
		"Hi Sarah:",
		"Please check the draft documents for customs clearance.",
		"Best Regards,",
		"Cherish Xie(Pei)",
		"Logistics Specialist",
		"International Business Division",
		"Marine Industry Group",
		"Factory: Zhongtian Technology Submarine Cable Co., Ltd.",
	}, "\n")

	got := CleanForDisplay(body)
	for _, want := range []string{"Hi Sarah", "draft documents"} {
		if !strings.Contains(got.Body, want) {
			t.Fatalf("short business body missing %q:\n%s", want, got.Body)
		}
	}
	for _, gone := range []string{"Best Regards", "Logistics Specialist", "Factory:"} {
		if strings.Contains(got.Body, gone) {
			t.Fatalf("professional signature leaked %q:\n%s", gone, got.Body)
		}
	}
}

func TestCleanForDisplay_StripsYoursSincerelySignature(t *testing.T) {
	body := strings.Join([]string{
		"Dear Sara",
		"May you please check the invoice statement.",
		"The invoice for customs is slightly different from the payment invoice.",
		"The final amount of the 5 batches will be added to the actual contract value.",
		"Kindly advise the comment on the excelsheet if to be modified.",
		"Yours sincerely.",
		"Best Regards,",
		"Christina Gu (Li Zhen)",
		"Project Execution Manager",
		"International Business Division",
		"Marine Industry Group",
		"Email: christina@example.com Website: www.example.com",
	}, "\n")

	got := CleanForDisplay(body)
	for _, want := range []string{"invoice statement", "final amount", "excelsheet"} {
		if !strings.Contains(got.Body, want) {
			t.Fatalf("business body missing %q:\n%s", want, got.Body)
		}
	}
	for _, gone := range []string{"Yours sincerely", "Best Regards", "Project Execution Manager"} {
		if strings.Contains(got.Body, gone) {
			t.Fatalf("closing signature leaked %q:\n%s", gone, got.Body)
		}
	}
}

func TestCleanForDisplay_StripsKoreanCompanySignatureAfterShortBody(t *testing.T) {
	body := strings.Join([]string{
		"업무에 고생이 많으십니다.",
		"무림 울산풍력 사업 검토안 입니다.",
		"감사합니다.",
		"탑솔라(주)",
		"미래사업실 / 박종원 부장",
		"광주광역시 북구 첨단연신로 30번길 41",
		"HP 010-1111-2222",
		"E mail park@example.com",
	}, "\n")

	got := CleanForDisplay(body)
	for _, want := range []string{"고생이 많으십니다", "검토안"} {
		if !strings.Contains(got.Body, want) {
			t.Fatalf("short Korean business body missing %q:\n%s", want, got.Body)
		}
	}
	for _, gone := range []string{"감사합니다", "탑솔라(주)", "미래사업실", "HP 010"} {
		if strings.Contains(got.Body, gone) {
			t.Fatalf("Korean signature leaked %q:\n%s", gone, got.Body)
		}
	}
}

func TestCleanForDisplay_KeepsBusinessBulletAfterThanks(t *testing.T) {
	body := strings.Join([]string{
		"업무에 고생이 많으십니다.",
		"2월 풍황데이터 입니다.",
		"감사합니다.",
		"- 2월 풍속값 : 6.4m/s (110m기준)",
		"탑솔라(주)",
		"미래사업실 / 박종원 부장",
		"광주광역시 북구 첨단연신로 30번길 41",
		"HP 010-1111-2222",
		"E mail park@example.com",
	}, "\n")

	got := CleanForDisplay(body)
	for _, want := range []string{"2월 풍황데이터", "6.4m/s"} {
		if !strings.Contains(got.Body, want) {
			t.Fatalf("business bullet missing %q:\n%s", want, got.Body)
		}
	}
	for _, gone := range []string{"탑솔라(주)", "미래사업실", "HP 010"} {
		if strings.Contains(got.Body, gone) {
			t.Fatalf("signature leaked %q:\n%s", gone, got.Body)
		}
	}
}

func TestCleanForDisplay_DoesNotTreatSubstantiveShortForwardAsWrapper(t *testing.T) {
	body := strings.Join([]string{
		"대용량 파일첨부 2개",
		"(18.5 MB)",
		"다운로드 기간 : 2026-06-10 ~ 2026-07-09",
		"(대용량 첨부 파일은 30일간 보관)",
		"module.zip",
		"(14.67 MB)",
		"안녕하십니까, 탑솔라(주) 고건 대리입니다.",
		"당사에서 주력으로 사용하고있는 진코 635, 640 모듈 자료 송부드리오니 업무에 참고부탁드립니다.",
		"궁금하신점은 언제든 연락 부탁드립니다.",
		"감사합니다!",
		"고 건",
		"<span ng-if=\"showField('title')\">기획조정실 대리",
		"Mobile:<span ng-if=\"showField('mobile')\"> 010-1111-2222",
		"<hr dze_content_sep=\"\">",
		"보내는사람: 임상훈 <lim@example.com>",
		"받는사람 : 고건 <go@example.com>",
		"보낸 날짜 : 2026-06-08 10:00",
		"제목 : RE: RFP 참고자료 송부",
		"주의 : 이 메일은 조직 외부에서 발송되었습니다.",
		"안녕하십니까, 탑솔라(주) 고건 대리입니다.",
		"참고하실수있는 RFP자료 공유드리오니 업무에 참고부탁드립니다.",
	}, "\n")

	got := CleanForDisplay(body)
	for _, want := range []string{"진코 635, 640", "궁금하신점"} {
		if !strings.Contains(got.Body, want) {
			t.Fatalf("latest substantive body missing %q:\n%s", want, got.Body)
		}
	}
	for _, gone := range []string{"조직 외부", "RFP자료", "보내는사람"} {
		if strings.Contains(got.Body, gone) {
			t.Fatalf("old forwarded body leaked %q:\n%s", gone, got.Body)
		}
	}
}

func TestCleanForDisplay_KeepsForwardedBodyAfterNameAndHTMLSignaturePrefix(t *testing.T) {
	body := strings.Join([]string{
		"양 도 현",
		"<span ng-if=\"showField('title')\">설계실 팀장/부장",
		"Mobile:<span ng-if=\"showField('mobile')\"> 010-1111-2222",
		"Email:<span ng-if=\"showField('email')\"> yang@example.com",
		"<span ng-if=\"showField('address1')\">광주광역시 북구 첨단연신로 30번길 41",
		"<hr dze_content_sep=\"\">",
		"보내는사람: 고건 <go@example.com>",
		"받는사람 : 양도현 <yang@example.com>",
		"보낸 날짜 : 2026-06-01 10:00",
		"제목 : [현대자동차 아산 원동실 태양광]가배치도면 요청의 건",
		"대용량 파일첨부 2개(53.9 MB)다운로드 기간 : 2026-06-01 ~ 2026-06-30",
		"(대용량 첨부 파일은 30일간 보관)",
		"layout.zip (49.78 MB)",
		"안녕하십니까! 탑솔라(주) 고건 대리입니다.",
		"표제와 같이 가배치 도면 요청드리며, 관련 내용은 미팅내용 정리 자료 보시면 확인 가능하십니다.",
		"증축예정인 곳까지 포함하여 가배치도 요청 드립니다.",
		"감사합니다!!!!!",
		"고 건",
	}, "\n")

	got := CleanForDisplay(body)
	for _, want := range []string{"가배치 도면 요청", "증축예정인 곳"} {
		if !strings.Contains(got.Body, want) {
			t.Fatalf("forwarded body missing %q:\n%s", want, got.Body)
		}
	}
	for _, gone := range []string{"양 도 현", "showField", "대용량 파일첨부", "보내는사람"} {
		if strings.Contains(got.Body, gone) {
			t.Fatalf("signature/header leaked %q:\n%s", gone, got.Body)
		}
	}
}

func TestCleanForDisplay_DoesNotTreatRoleMentionBusinessBodyAsSignature(t *testing.T) {
	body := strings.Join([]string{
		"장재일 차장님",
		"안녕하세요 제이티에너지 임은진 입니다.",
		"삼신화학공업 한전설계검토비 외2건 납부 관련하여, 2건은 4/17일까지 1건은 5/9일까지 납부일이였습니다.",
		"고지서를 받고, 김대희과장 및 탑솔라 측에,",
		"저희가 부탁 드린것이, 삼신화학공업으로 미납 안내가 가지 않게 해 달라는 부탁이였습니다.",
		"하지만, 결국 4/17일 미납건에 대해, 삼신화학 담당자에게 한전에서 연락이 갔습니다.",
		"이번에 5/9일까지 납부가 되지 않은 건에 대해서도 한전에서 연락이 간다면, 임대인 쪽에서 보는 시선이 좋지 않을것이라 생각됩니다.",
		"그래서, 당사에서 해당 비용에 대해 금일 납부하고 입금확인증 송부 드립니다.",
		"삼신화학공업 한전선로비 대납 진행 금액에 대해서, 첨부의 당사 계좌로 입금 될 수 있도록 처리해 주시기 바랍니다.",
		"세금계산서 발행을 어떻게 하실껀지 문의하셔서, 알려주시기 바랍니다.",
		"빠른 실행이 될 수 있도록 부탁 드리겠습니다.",
		"감사합니다.",
		"(주)제이티에너지",
		"임은진 차장 / H. 010-1111-2222",
		"T. 02-111-2222 / E. lim@example.com",
	}, "\n")

	got := CleanForDisplay(body)
	for _, want := range []string{"고지서를 받고", "미납건", "입금확인증", "세금계산서"} {
		if !strings.Contains(got.Body, want) {
			t.Fatalf("role-bearing business body missing %q:\n%s", want, got.Body)
		}
	}
	for _, gone := range []string{"(주)제이티에너지", "임은진 차장 / H", "T. 02"} {
		if strings.Contains(got.Body, gone) {
			t.Fatalf("signature leaked %q:\n%s", gone, got.Body)
		}
	}
}

func TestCleanForDisplay_StripsAttachmentTotalWithLinkedFiles(t *testing.T) {
	body := strings.Join([]string{
		"대용량 첨부파일 총 (7개) ※ 다운로드 기간 : 2026-04-29 ~ 2026-05-29",
		"구조계산서_EcoPro AP.pdf<<url> (38.63 MB)",
		"구조계산서_EcoPro BM.pdf<<url> (93.55 MB)",
		"수신 : 수신처제위",
		"발신 : 박지선 책임 / 에코프로 구매혁신팀",
		"안녕하세요.",
		"에코프로 그룹사 주차장 구조계산서 송부드립니다. 견적 산출 시 참고 바랍니다.",
		"감사합니다.",
		"박지선 책임 / 에코프로 구매혁신팀",
		"T. 043-111-2222 / E. park@example.com",
	}, "\n")

	got := CleanForDisplay(body)
	for _, want := range []string{"수신 : 수신처제위", "구조계산서 송부", "견적 산출"} {
		if !strings.Contains(got.Body, want) {
			t.Fatalf("business body missing %q:\n%s", want, got.Body)
		}
	}
	for _, gone := range []string{"대용량 첨부파일", "EcoPro AP.pdf", "38.63 MB", "T. 043"} {
		if strings.Contains(got.Body, gone) {
			t.Fatalf("attachment/signature noise leaked %q:\n%s", gone, got.Body)
		}
	}
}

func TestCleanForDisplay_StripsLooseForwardedAttachmentExpiryRows(t *testing.T) {
	body := strings.Join([]string{
		"자료 공유드립니다.",
		"<hr dze_content_sep=\"\">",
		"보내는사람: 김승우 <kim@example.com>",
		"받는사람 : 김대희 <daehee@example.com>",
		"제목 : 화성산단 태양광 관련",
		"대용량 첨부 5개 11MB",
		"수협 약정식 제출서류 안내_화성산단태양광.xlsx 20283",
		"~ 2026/03/27",
		"1. 컨소시엄 합의서_화성산단태양광_RPS.docx 31735",
		"~ 2026/03/27",
		"기한이 있는 파일은 30일 보관 / 100회 다운로드 가능",
		"안녕하세요, 오늘회계법인 김승우입니다.",
		"어제 화성산단태양광 대출약정은 체결이 되었으며,",
		"추후 공사도급계약/관리운영계약 날인시 시공사 날인 필요한 양식 안내드립니다. 첨부 참고해주시기 바랍니다.",
		"-컨소시엄 합의서 : 한화시스템에 제출",
		"-대출약정서의 별지 양식(별지7. 책임준공확약서, 별첨5. 확약서): 수협 제출",
		"첨부의 제출서류목록 2. 시공사 관련 서류도 참고하여 준비 부탁드립니다.",
		"감사합니다.",
		"김승우 드림",
		"김 승 우 공인회계사(KICPA)",
		"오늘회계법인 서울 강남구 테헤란로 429 원방빌딩 14층",
		"Tel. 02-111-2222",
	}, "\n")

	got := CleanForDisplay(body)
	for _, want := range []string{"오늘회계법인", "대출약정", "컨소시엄 합의서", "책임준공확약서", "제출서류목록"} {
		if !strings.Contains(got.Body, want) {
			t.Fatalf("forwarded body missing %q:\n%s", want, got.Body)
		}
	}
	for _, gone := range []string{"대용량 첨부", "~ 2026/03/27", "xlsx 20283", "김승우 드림", "공인회계사", "Tel."} {
		if strings.Contains(got.Body, gone) {
			t.Fatalf("loose attachment metadata leaked %q:\n%s", gone, got.Body)
		}
	}
}

func TestCleanForDisplay_StripsSpacedSignoffAndTailName(t *testing.T) {
	body := strings.Join([]string{
		"안녕하세요 상무님, 잘 지내시지요?",
		"그룹사 주차장 대상 태양광 PPA 사업을 검토하고 있습니다.",
		"의견 부탁드립니다.",
		"감사합니다.",
		"P.S : 본 내용은 외부 유출되지 않도록 보안 유의 부탁드립니다.",
		"송 기 섭 드림",
		"Yours sincerely,",
		"송 기 섭 팀장",
		"Tel.",
		"010-1111-2222",
	}, "\n")

	got := CleanForDisplay(body)
	for _, want := range []string{"PPA 사업", "보안 유의"} {
		if !strings.Contains(got.Body, want) {
			t.Fatalf("business body missing %q:\n%s", want, got.Body)
		}
	}
	for _, gone := range []string{"송 기 섭 드림", "Yours sincerely", "Tel."} {
		if strings.Contains(got.Body, gone) {
			t.Fatalf("spaced signoff leaked %q:\n%s", gone, got.Body)
		}
	}
}

func TestCleanForDisplay_StripsBilingualTailName(t *testing.T) {
	body := strings.Join([]string{
		"안녕하세요 이사님,",
		"유첨의 내용으로 당사 100MW 견적서 내용을 송부드리오니 참조 부탁드립니다.",
		"감사합니다.",
		"좋은 하루 되세요.",
		"|",
		"|",
		"최종원 / Jongwon Choi",
		"해외영업부 / Overseas Sales Dept.",
		"모바일 (Mob)： +82-10-1111-2222",
	}, "\n")

	got := CleanForDisplay(body)
	if !strings.Contains(got.Body, "100MW 견적서") {
		t.Fatalf("business body missing:\n%s", got.Body)
	}
	for _, gone := range []string{"최종원 / Jongwon Choi", "Overseas Sales", "Mob"} {
		if strings.Contains(got.Body, gone) {
			t.Fatalf("tail name signature leaked %q:\n%s", gone, got.Body)
		}
	}
}

func TestCleanForDisplay_DoesNotTreatRecipientRoleLineBeforeBusinessAsSignature(t *testing.T) {
	body := strings.Join([]string{
		"안녕하십니까. 일진전기 국내영업2팀 은종훈 대리입니다.",
		"업무에 노고가 많으십니다.",
		"이시연 주임님",
		"제 2조 7항 부분 내 지체상금 관련하여, 최대 상금(CAP) 10% 명기 요청드립니다.",
		"기타 문의사항 있으시다면 연락 부탁 드립니다.",
		"감사합니다.",
		"은종훈  Eun Jong Hun",
		"일진전기 ㅣ 전선사업본부 국내영업2팀 ㅣ 대리",
		"서울시 강서구 마곡중앙14로 15 (마곡동) 7층 일진전기",
		"T 02-111-2222   F 02-333-4444   M 010-5555-6666",
		"E-mail  eun@example.com",
	}, "\n")

	got := CleanForDisplay(body)
	for _, want := range []string{"이시연 주임님", "CAP) 10% 명기", "연락 부탁"} {
		if !strings.Contains(got.Body, want) {
			t.Fatalf("recipient/business line missing %q:\n%s", want, got.Body)
		}
	}
	for _, gone := range []string{"은종훈  Eun", "전선사업본부", "T 02"} {
		if strings.Contains(got.Body, gone) {
			t.Fatalf("signature leaked %q:\n%s", gone, got.Body)
		}
	}
}

func TestCleanForDisplay_StripsAttachmentOnlyBody(t *testing.T) {
	body := strings.Join([]string{
		"대용량 파일첨부 1개",
		"(15.5 MB)",
		"다운로드 기간 : 2026-04-11 ~ 2026-05-10",
		"(대용량 첨부 파일은 30일간 보관)",
		"에코프로_가배치.zip",
		"(15.5 MB)",
	}, "\n")

	got := CleanForDisplay(body)
	if got.Body != "" {
		t.Fatalf("attachment-only body should be empty, got:\n%s", got.Body)
	}
	if len(got.HiddenBlocks) == 0 || got.HiddenBlocks[0].Kind != "attachment" {
		t.Fatalf("expected attachment hidden block, got %+v", got.HiddenBlocks)
	}
}

func TestCleanForDisplay_StripsInlineLargeAttachmentPrefix(t *testing.T) {
	body := "대용량 첨부파일 (Timezone : +0900 Asia/Seoul) 공장동 단면도.dwg 41.8 MB 05/24 23:59 지붕 상세.dwg 458.0 KB 05/24 23:59 안녕하세요 MAP허근범 소장입니다. 함평공장 공장도 계획 단면도 및 지붕 상세도 전달 드리니 확인 부탁드립니다."

	got := CleanForDisplay(body)
	for _, want := range []string{"안녕하세요 MAP허근범", "확인 부탁드립니다"} {
		if !strings.Contains(got.Body, want) {
			t.Fatalf("body missing %q:\n%s", want, got.Body)
		}
	}
	for _, gone := range []string{"대용량 첨부파일", "공장동 단면도.dwg", "Timezone"} {
		if strings.Contains(got.Body, gone) {
			t.Fatalf("inline attachment metadata leaked %q:\n%s", gone, got.Body)
		}
	}
}

func TestCleanForDisplay_StripsDecorativeSeparators(t *testing.T) {
	body := strings.Join([]string{
		"안녕하세요. 탑솔라 재경실입니다.",
		"2026년 1월 미처리 세금계산서 내역을 첨부와 같이 전달드립니다.",
		"━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━",
		"-------------------------------------------   아   래   -------------------------------------------",
		"연장된 마감기한 : 2026년 2월 20일",
		"━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━",
		"바쁘신 와중에도 협조해 주셔서 감사합니다.",
	}, "\n")

	got := CleanForDisplay(body)
	for _, want := range []string{"미처리 세금계산서", "연장된 마감기한"} {
		if !strings.Contains(got.Body, want) {
			t.Fatalf("business body missing %q:\n%s", want, got.Body)
		}
	}
	if strings.Contains(got.Body, "━━━━") || strings.Contains(got.Body, "아   래") {
		t.Fatalf("decorative separator leaked:\n%s", got.Body)
	}
}

func TestCleanForDisplay_CutsInlineReplyHistoryAndAttachmentMetadata(t *testing.T) {
	body := "안녕하세요 MAP허근범 소장입니다. 함평공장 공장도 계획 단면도 및 지붕 상세도 전달 드리니 확인 부탁드립니다. 감사합니다. 허근 범 KEUNBEOM HEO 설계부문 / 설계 2 본부 /2 그룹 소장 D 02. 520.9346 M kbheo@example.com 상기 메일은 지정된 수신인만을 위한 것이며 기밀정보를 포함할 수 있습니다. Date: 2026/04/24 08:42:20 From: 김대희 <kdh@example.com> To: kbheo@example.com Cc: team@example.com Subject: [탑솔라(주)] 금호타이어 함평공장 TPO 관련 자료 송부의 件 대용량 파일첨부 4개 (23.64 MB) 다운로드 기간 : 2026-04-24 ~ 2026-05-24 TPO자료.pdf (3.86 MB) 소장님 안녕하십니까. 탑솔라 기획조정실 김대희과장입니다."

	got := CleanForDisplay(body)
	for _, want := range []string{"안녕하세요 MAP허근범", "확인 부탁드립니다"} {
		if !strings.Contains(got.Body, want) {
			t.Fatalf("latest body missing %q:\n%s", want, got.Body)
		}
	}
	for _, gone := range []string{"KEUNBEOM", "상기 메일은", "Date:", "From:", "대용량 파일첨부", "TPO자료.pdf", "소장님 안녕하십니까"} {
		if strings.Contains(got.Body, gone) {
			t.Fatalf("inline history/noise leaked %q:\n%s", gone, got.Body)
		}
	}
}

func TestCleanForDisplay_StripsOpenRouterReceiptSupportFooter(t *testing.T) {
	body := strings.Join([]string{
		"Receipt from OpenRouter, Inc Receipt #1937-7192",
		"Amount paid",
		"$52.75",
		"Date paid",
		"May 15, 2026, 12:53:38 PM",
		"Payment method",
		"- 1880",
		"Summary",
		"- OpenRouter Credits : $52.75",
		"- Amount paid : $52.75",
		"If you have any questions, visit our support site at https://discord.gg/example, contact us at support@example.com.",
		"Something wrong with the email? View in browser: https://dashboard.stripe.com/receipts/payment/example",
		"You're receiving this email because you made a purchase at OpenRouter, Inc, which partners with Stripe.",
	}, "\n")

	got := CleanForDisplay(body)
	for _, want := range []string{"Receipt #1937-7192", "$52.75", "May 15, 2026"} {
		if !strings.Contains(got.Body, want) {
			t.Fatalf("receipt detail missing %q:\n%s", want, got.Body)
		}
	}
	for _, gone := range []string{"support site", "View in browser", "partners with Stripe"} {
		if strings.Contains(got.Body, gone) {
			t.Fatalf("receipt footer leaked %q:\n%s", gone, got.Body)
		}
	}
}

func TestCleanForDisplay_StripsOriginalBoundaryAndMobileSignature(t *testing.T) {
	body := strings.Join([]string{
		"Hi Sara,",
		"Well received the attached stamped PI and PO with thanks.",
		"We will produce the cables accordingly after our CNY holiday.",
		"Fred/JOCA CABLE",
		"�����ҵ�iPhone",
		"------------------ Original ------------------",
		"From: Sara <sara@example.com>",
		"Sent: Monday, March 9, 2026 10:00 AM",
		"Subject: previous message",
		"Old thread body.",
	}, "\n")

	got := CleanForDisplay(body)
	for _, want := range []string{"stamped PI", "CNY holiday"} {
		if !strings.Contains(got.Body, want) {
			t.Fatalf("latest body missing %q:\n%s", want, got.Body)
		}
	}
	for _, gone := range []string{"iPhone", "Original", "Old thread"} {
		if strings.Contains(got.Body, gone) {
			t.Fatalf("original/mobile noise leaked %q:\n%s", gone, got.Body)
		}
	}
}

func TestCleanForDisplay_StripsForwardWrapperContactSignature(t *testing.T) {
	body := strings.Join([]string{
		"전달바랍니다.",
		"이현성 HYUN SUNG LEE",
		"광주시설관리팀 책임매니저",
		"Facilities Management Team - AutoLand Gwangju",
		"E 2555151@kia.com",
		"T +82-62-370-3396",
		"보낸사람 : 이전 담당자 <old@example.com>",
		"제목 : 이전 메일",
		"이전 본문입니다.",
	}, "\n")

	got := CleanForDisplay(body)
	if strings.TrimSpace(got.Body) != "전달바랍니다." {
		t.Fatalf("expected only thin forward wrapper, got:\n%s", got.Body)
	}
}

func TestCleanForDisplay_StripsHTMLWrapperOnlyLine(t *testing.T) {
	body := strings.Join([]string{
		`<mailplughtml xmlns:v="urn:schemas-microsoft-com:vml" xmlns:o="urn:schemas-microsoft-com:office:office">`,
		"안녕하십니까 차장님",
		"내주 미팅관련하여 하기와 같이 회신 드립니다.",
		"일정: 내주 화요일 13시 30분",
		"장소: 여의도 파크원타워2",
	}, "\n")

	got := CleanForDisplay(body)
	for _, want := range []string{"내주 미팅", "파크원타워2"} {
		if !strings.Contains(got.Body, want) {
			t.Fatalf("business body missing %q:\n%s", want, got.Body)
		}
	}
	if strings.Contains(got.Body, "mailplughtml") {
		t.Fatalf("html wrapper leaked:\n%s", got.Body)
	}
}

func TestCleanForDisplay_StripsGitHubNotificationFooter(t *testing.T) {
	body := strings.Join([]string{
		"Exploring acceleration methods for self-hosting",
		"Session completed",
		"choiceoh/osty",
		"View session: https://github.com/choiceoh/osty/tasks/example",
		"Share feedback on Copilot cloud agent: https://gh.io/copilot-coding-agent-survey",
		"Manage notification settings: https://github.com/settings/notifications",
	}, "\n")

	got := CleanForDisplay(body)
	for _, want := range []string{"Session completed", "View session"} {
		if !strings.Contains(got.Body, want) {
			t.Fatalf("notification detail missing %q:\n%s", want, got.Body)
		}
	}
	for _, gone := range []string{"Share feedback", "Manage notification"} {
		if strings.Contains(got.Body, gone) {
			t.Fatalf("notification footer leaked %q:\n%s", gone, got.Body)
		}
	}
}

func TestCleanForDisplay_StripsMojibakeReplyBoundary(t *testing.T) {
	body := strings.Join([]string{
		"Dear Sara and Park",
		"Sorry I'm currently on a business trip in the Philippines, so I'm unable to participate in this reception.",
		"Christina will be there to support you if any issues arise.",
		"----------�ظ����ʼ���Ϣ----------",
		"From: Sara <sara@example.com>",
		"Subject: old message",
		"Old thread body.",
	}, "\n")

	got := CleanForDisplay(body)
	for _, want := range []string{"business trip", "Christina"} {
		if !strings.Contains(got.Body, want) {
			t.Fatalf("latest body missing %q:\n%s", want, got.Body)
		}
	}
	for _, gone := range []string{"�ظ", "From:", "Old thread"} {
		if strings.Contains(got.Body, gone) {
			t.Fatalf("mojibake reply boundary leaked %q:\n%s", gone, got.Body)
		}
	}
}

func TestCleanForDisplay_StripsSplitOriginalMessageBoundary(t *testing.T) {
	body := strings.Join([]string{
		"안녕하세요? 부장님",
		"파인드그린 신정훈 입니다.",
		"주말 잘 보내셨나요?",
		"보내주신 메일에 대해 답신을 드립니다.",
		"첨부된 공문 참고 하십시요",
		"신정훈 드림",
		"---------- Original",
		"message ----------",
		"From: old@example.com",
		"Subject: old message",
		"이전 본문입니다.",
	}, "\n")

	got := CleanForDisplay(body)
	for _, want := range []string{"파인드그린", "공문 참고"} {
		if !strings.Contains(got.Body, want) {
			t.Fatalf("latest body missing %q:\n%s", want, got.Body)
		}
	}
	for _, gone := range []string{"신정훈 드림", "Original", "message ----------", "이전 본문"} {
		if strings.Contains(got.Body, gone) {
			t.Fatalf("split original boundary/signoff leaked %q:\n%s", gone, got.Body)
		}
	}
}

func TestCleanForDisplay_StripsTrailingImageAndBilingualNameAfterShortBusinessLine(t *testing.T) {
	body := strings.Join([]string{
		"견적 제출은 5/26(화) 오전까지 부탁드립니다.",
		"[Image]",
		"정한구 Hangoo Jung",
		"Facilities Management Team",
		"T +82-62-370-0000",
		"From: old@example.com",
		"Subject: old message",
		"이전 본문입니다.",
	}, "\n")

	got := CleanForDisplay(body)
	if strings.TrimSpace(got.Body) != "견적 제출은 5/26(화) 오전까지 부탁드립니다." {
		t.Fatalf("expected only short business line, got:\n%s", got.Body)
	}
}

func TestCleanForDisplay_StripsAuditOfficeSignatureTail(t *testing.T) {
	body := strings.Join([]string{
		"검토의견입니다.",
		"업무에 참고하십시요",
		"감사실",
		"홍 상 호  감사 / 공인회계사",
		"From: old@example.com",
		"Subject: old message",
		"이전 본문입니다.",
	}, "\n")

	got := CleanForDisplay(body)
	if strings.TrimSpace(got.Body) != "검토의견입니다.\n업무에 참고하십시요" {
		t.Fatalf("expected audit signature stripped, got:\n%s", got.Body)
	}
}

func TestCleanForDisplay_CutsRepeatedThreadAfterFirstSignatureBlock(t *testing.T) {
	body := strings.Join([]string{
		"Hi Sara, Jin Yun & Park ,",
		"For lower copper price level, here update you the CNY&USD lower prices Solar PV DC cable H1Z2Z2-K 6mm2 for your purchase decision.",
		"Looking forward to long-term cooperation with you.",
		"Best Regards & Thanks so much",
		"Fred Lee | Overseas Manager",
		"JOCA Special Cable (Shanghai) Co., Ltd.",
		"Wuxi JOCA Cable Technology Group Co., Ltd.",
		"www.joca-cable.com     www.jocagroup.com",
		"Mobile: +86 13651882591 (WhatsApp)",
		"E-mail: fred@jocacable.com,fred@jiukaicable.com,",
		"Add :No 875, Puwei Rd , Fengxian District, Shanghai ,China ,201402.",
		"Hi Sara, Jin Yun & Park ,",
		"Fred again.",
		"Further to our quoted Solar PV DC cable prices, could we have your comments?",
		"Best Regards & Thanks so much",
		"Fred Lee | Overseas Manager",
	}, "\n")

	got := CleanForDisplay(body)
	for _, want := range []string{"lower copper price", "purchase decision"} {
		if !strings.Contains(got.Body, want) {
			t.Fatalf("latest body missing %q:\n%s", want, got.Body)
		}
	}
	for _, gone := range []string{"Fred again", "Mobile:", "E-mail:", "Wuxi JOCA"} {
		if strings.Contains(got.Body, gone) {
			t.Fatalf("repeated thread/signature leaked %q:\n%s", gone, got.Body)
		}
	}
}

func TestCleanForDisplay_StripsInlineHTMLMarketingWrapper(t *testing.T) {
	body := strings.Join([]string{
		`<div style="font-size: 16px; font-family: Verdana, sans-serif; max-width: 600px; margin:auto; background-color: #F8F8F8; padding: 24px;">`,
		`<img src="https://example.com/logo.svg" style="display: block;" />`,
		`<br>Hi 오선택,`,
		`<br>We have activated the free plan for your account so that you can try the API right away.`,
		`Your API Key can be found on the Metals.Dev <a href="https://metals.dev/dashboard" target="_blank">dashboard</a>.`,
		`<br><br>Regards, <br>Team Metals.Dev.</div>`,
	}, "\n")

	got := CleanForDisplay(body)
	for _, want := range []string{"Hi 오선택", "free plan", "dashboard"} {
		if !strings.Contains(got.Body, want) {
			t.Fatalf("html body missing %q:\n%s", want, got.Body)
		}
	}
	for _, gone := range []string{"<div", "font-family", "<img", "<br", "</a>"} {
		if strings.Contains(got.Body, gone) {
			t.Fatalf("html wrapper leaked %q:\n%s", gone, got.Body)
		}
	}
}

func TestCleanForDisplay_StripsZeroWidthInlineFooterAndReplyHistory(t *testing.T) {
	body := strings.Join([]string{
		"대리님 안녕하세요.",
		"요청하신 자료 송부드립니다.",
		"누락된 자료는 PDF 스캔본 참고하셔서 새로 단선결선도 작성 부탁드립니다.",
		"감사합니다.",
		"김 우 종  대리 | 전기팀",
		"대한전선주식회사 31791 충청남도 당진시 고대면 대호만로 870 당진공장",
		"M 010-2940-3086   T 041-360-9823  F 041-360-9299   E woojong0607@example.com",
		"본 e-mail(첨부자료 포함)은 지정된 수신인에 한하여 이용 가능합니다.",
		"This e-mail [including attachment(s)] is solely for the use of intended recipient(s).",
		"From : 윤남열<old@example.com>",
		"Sent : 2026-05-11 09:02",
		"Subject : Fw: [탑솔라(주)] 전기 설계 실사 이후 자료 요청의 건",
		"아래 내용 확인 후 자료 송부 바랍니다.",
	}, "\u200b")

	got := CleanForDisplay(body)
	for _, want := range []string{"요청하신 자료", "단선결선도 작성"} {
		if !strings.Contains(got.Body, want) {
			t.Fatalf("business body missing %q:\n%s", want, got.Body)
		}
	}
	for _, gone := range []string{"본 e-mail", "This e-mail", "From :", "아래 내용", "woojong0607"} {
		if strings.Contains(got.Body, gone) {
			t.Fatalf("inline footer/history leaked %q:\n%s", gone, got.Body)
		}
	}
}

func TestCleanForDisplay_StripsLinkAccountFooter(t *testing.T) {
	body := strings.Join([]string{
		"New login detected",
		"We noticed a login to your Link account from a new device. If this was you, no further action is needed.",
		"- Device: Android (Samsung Internet)",
		"- Location: Buk-gu, Gwangju, South Korea",
		"- When: May 22, 2026 at 12:25:45 AM GMT+9",
		"- How: Verified with one-time passcode sent to phone",
		"If this was not you, log in to your Link account and review your purchases for suspicious activity. If you notice any signs that your account might be compromised, contact us for assistance.",
		"If you believe you are getting this email in error or want to close your Link account, please visit our support site.",
		"One Wilton Park, Wilton Place, Dublin 2 D02 FX04, Ireland",
	}, "\n")

	got := CleanForDisplay(body)
	for _, want := range []string{"New login detected", "Android", "one-time passcode"} {
		if !strings.Contains(got.Body, want) {
			t.Fatalf("security detail missing %q:\n%s", want, got.Body)
		}
	}
	for _, gone := range []string{"close your Link account", "support site", "One Wilton Park"} {
		if strings.Contains(got.Body, gone) {
			t.Fatalf("link footer leaked %q:\n%s", gone, got.Body)
		}
	}
}

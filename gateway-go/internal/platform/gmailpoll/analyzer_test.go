package gmailpoll

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
)

func TestLoadPrompt_Default(t *testing.T) {
	prompt := loadPrompt("")
	if prompt != DefaultPrompt {
		t.Errorf("empty path should return default prompt")
	}
}

func TestLoadPrompt_CustomFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "custom-prompt.md")
	custom := "커스텀 분석 프롬프트입니다."
	if err := os.WriteFile(path, []byte(custom), 0600); err != nil {
		t.Fatal(err)
	}

	prompt := loadPrompt(path)
	if prompt != custom {
		t.Errorf("loadPrompt = %q, want %q", prompt, custom)
	}
}

func TestServiceAnalysisPromptPrefersNativeOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "custom-prompt.md")
	if err := os.WriteFile(path, []byte("file prompt"), 0600); err != nil {
		t.Fatal(err)
	}
	svc := NewService(Config{
		PromptFile: path,
		PromptOverride: func(id string) (string, bool) {
			if id != PromptIDAutoMailAnalysis {
				t.Fatalf("unexpected prompt id: %s", id)
			}
			return " native prompt ", true
		},
	}, nil)
	if got := svc.analysisPrompt(); got != "native prompt" {
		t.Fatalf("analysisPrompt = %q, want native override", got)
	}
}

func TestServiceAnalysisPromptFallsBackToPromptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "custom-prompt.md")
	if err := os.WriteFile(path, []byte("file prompt"), 0600); err != nil {
		t.Fatal(err)
	}
	svc := NewService(Config{
		PromptFile: path,
		PromptOverride: func(string) (string, bool) {
			return "", false
		},
	}, nil)
	if got := svc.analysisPrompt(); got != "file prompt" {
		t.Fatalf("analysisPrompt = %q, want file prompt", got)
	}
}

func TestFormatEmailForAnalysis(t *testing.T) {
	msg := &gmail.MessageDetail{
		From:    "sender@example.com",
		To:      "me@example.com",
		Subject: "Test Subject",
		Date:    "Mon, 1 Jan 2024 00:00:00 +0900",
		Body:    "Hello, this is the email body.",
	}

	result := FormatEmailForAnalysis(msg)

	if !strings.Contains(result, "sender@example.com") {
		t.Error("should contain From address")
	}
	if !strings.Contains(result, "Test Subject") {
		t.Error("should contain Subject")
	}
	if !strings.Contains(result, "Hello, this is the email body.") {
		t.Error("should contain body")
	}
}

func TestFormatEmailForAnalysis_LongBody(t *testing.T) {
	longBody := strings.Repeat("x", 10000)
	msg := &gmail.MessageDetail{
		From:    "a@b.com",
		To:      "c@d.com",
		Subject: "Long",
		Body:    longBody,
	}

	result := FormatEmailForAnalysis(msg)
	if !strings.Contains(result, "본문 생략") {
		t.Error("long body should be truncated with notice")
	}
	// Body in result should be capped.
	if strings.Contains(result, longBody) {
		t.Error("full long body should not appear in result")
	}
}

func TestFormatEmailForAnalysis_StripsSignatureNoiseOnly(t *testing.T) {
	body := strings.Join([]string{
		"안녕하세요. 기아 화성 모듈 관련 견적 검토 부탁드립니다.",
		"6월 20일까지 21MW 기준 531억원 단가와 120장 수량을 확인해 주세요.",
		"첨부 견적서 기준으로 회신 부탁드립니다.",
		"",
		"감사합니다",
		"김대희 부장",
		"M 010-1234-5678",
		"E-mail kim@example.com",
	}, "\n")
	msg := &gmail.MessageDetail{
		From:    "김대희 <kim@example.com>",
		To:      "me@example.com",
		Subject: "기아 화성 모듈 관련 견적",
		Date:    "Wed, 17 Jun 2026 09:32:00 +0900",
		Body:    body,
	}

	result := FormatEmailForAnalysis(msg)

	for _, want := range []string{
		"기아 화성 모듈 관련 견적 검토",
		"6월 20일까지 21MW 기준 531억원 단가와 120장 수량",
	} {
		if !strings.Contains(result, want) {
			t.Fatalf("formatted mail missing %q:\n%s", want, result)
		}
	}
	for _, gone := range []string{"김대희 부장", "010-1234-5678", "E-mail kim@example.com"} {
		if strings.Contains(result, gone) {
			t.Fatalf("signature noise leaked %q:\n%s", gone, result)
		}
	}
}

func TestFormatEmailForAnalysis_StripsHeadSecurityBanner(t *testing.T) {
	body := strings.Join([]string{
		"주의: 외부 발신 메일입니다. 링크나 첨부파일을 열기 전 확인하세요.",
		"---",
		"",
		"안녕하세요. 해남 인버터 배치 수량표 확인 부탁드립니다.",
		"오늘 14시 OCI 인터뷰 전에 최종 수량만 회신 부탁드립니다.",
	}, "\n")
	msg := &gmail.MessageDetail{
		From:    "차남두 <cha@example.com>",
		To:      "me@example.com",
		Subject: "해남 인버터 배치 수량표",
		Body:    body,
	}

	result := FormatEmailForAnalysis(msg)
	if strings.Contains(result, "외부 발신 메일") || strings.Contains(result, "첨부파일을 열기") {
		t.Fatalf("head security banner leaked:\n%s", result)
	}
	if !strings.Contains(result, "해남 인버터 배치 수량표 확인") ||
		!strings.Contains(result, "오늘 14시 OCI 인터뷰") {
		t.Fatalf("business body missing after banner strip:\n%s", result)
	}
}

func TestFormatEmailForAnalysis_StripsLegalFooterNoiseOnly(t *testing.T) {
	body := strings.Join([]string{
		"안녕하세요. 울산 무림 풍력 사업 검토 의견 전달드립니다.",
		"531억 규모 사업비와 SMP 대비 단가 조건을 검토해 주세요.",
		"내일 오전까지 리스크 옵션만 회신 부탁드립니다.",
		"",
		"본 메일은 지정된 수신자만을 위한 기밀 정보이며 무단 복사 및 배포를 금지합니다.",
		"잘못 수신하신 경우 즉시 삭제해 주십시오.",
		"(주)탑솔라 전략기획팀",
		"Tel 02-1234-5678",
		"서울시 강남구 테헤란로 1",
	}, "\n")
	msg := &gmail.MessageDetail{
		From:    "박종원 <park@example.com>",
		To:      "me@example.com",
		Subject: "울산 무림 풍력 사업 검토",
		Body:    body,
	}

	result := FormatEmailForAnalysis(msg)
	for _, want := range []string{"울산 무림 풍력 사업 검토", "531억 규모 사업비", "내일 오전까지"} {
		if !strings.Contains(result, want) {
			t.Fatalf("business body missing %q:\n%s", want, result)
		}
	}
	for _, gone := range []string{"기밀 정보", "무단 복사", "탑솔라 전략기획팀", "02-1234-5678"} {
		if strings.Contains(result, gone) {
			t.Fatalf("footer noise leaked %q:\n%s", gone, result)
		}
	}
}

func TestFormatEmailForAnalysis_StripsCompactContactAndAddressFooter(t *testing.T) {
	body := strings.Join([]string{
		"안녕하세요. 기아 화성 모듈 납기 조정안 확인 부탁드립니다.",
		"6월 24일 입고분은 기존 수량 유지, 6월 28일 입고분만 120장에서 96장으로 조정해 주세요.",
		"변경 가능 여부를 오늘 중 회신 부탁드립니다.",
		"",
		"홍길동 / 영업2팀",
		"T. 02-1234-5678",
		"M. 010-1111-2222",
		"E. sales@example.com",
		"서울특별시 강남구 테헤란로 10",
		"Copyright 2026 Example Corp. All rights reserved.",
	}, "\n")
	msg := &gmail.MessageDetail{
		From:    "sales@example.com",
		To:      "me@example.com",
		Subject: "기아 화성 모듈 납기 조정",
		Body:    body,
	}

	result := FormatEmailForAnalysis(msg)
	for _, want := range []string{"기아 화성 모듈 납기 조정안", "6월 24일 입고분", "오늘 중 회신"} {
		if !strings.Contains(result, want) {
			t.Fatalf("business body missing %q:\n%s", want, result)
		}
	}
	for _, gone := range []string{"영업2팀", "02-1234-5678", "E. sales@example.com", "테헤란로", "All rights reserved"} {
		if strings.Contains(result, gone) {
			t.Fatalf("compact footer noise leaked %q:\n%s", gone, result)
		}
	}
}

func TestFormatEmailForAnalysis_StripsTrailingImageNoiseLine(t *testing.T) {
	body := strings.Join([]string{
		"안녕하세요. OCI 인터뷰 자료는 아래 변경사항만 반영하면 됩니다.",
		"인버터 배치 수량은 32대 기준으로 유지하고, 현장 사진은 별도 전달하겠습니다.",
		"",
		"[image: company-logo.png]",
	}, "\n")
	msg := &gmail.MessageDetail{
		From: "cha@example.com",
		To:   "me@example.com",
		Body: body,
	}

	result := FormatEmailForAnalysis(msg)
	if !strings.Contains(result, "인버터 배치 수량은 32대") {
		t.Fatalf("business body missing after trailing image strip:\n%s", result)
	}
	if strings.Contains(result, "company-logo") {
		t.Fatalf("trailing image noise leaked:\n%s", result)
	}
}

func TestFormatEmailForAnalysis_StripsTrailingLogoResidueLine(t *testing.T) {
	body := strings.Join([]string{
		"안녕하세요. 해남 프로젝트 주간 회의는 금요일 10시로 유지하겠습니다.",
		"자료는 전일 오후까지 공유하겠습니다.",
		"",
		"Company logo image cid:abc123 facebook youtube linkedin",
	}, "\n")
	msg := &gmail.MessageDetail{
		From: "sender@example.com",
		To:   "me@example.com",
		Body: body,
	}

	result := FormatEmailForAnalysis(msg)
	if !strings.Contains(result, "해남 프로젝트 주간 회의는 금요일 10시") {
		t.Fatalf("business body missing after logo residue strip:\n%s", result)
	}
	for _, gone := range []string{"Company logo", "cid:abc123", "facebook"} {
		if strings.Contains(result, gone) {
			t.Fatalf("trailing logo residue leaked %q:\n%s", gone, result)
		}
	}
}

func TestFormatEmailForAnalysis_StripsTrailingClosingOnly(t *testing.T) {
	body := strings.Join([]string{
		"안녕하세요. 기아 화성 모듈 납기 변경안 검토 부탁드립니다.",
		"6월 24일 입고분은 기존 수량 유지, 6월 28일 입고분만 96장으로 조정해 주세요.",
		"",
		"감사합니다.",
	}, "\n")
	msg := &gmail.MessageDetail{
		From: "sender@example.com",
		To:   "me@example.com",
		Body: body,
	}

	result := FormatEmailForAnalysis(msg)
	if !strings.Contains(result, "6월 28일 입고분만 96장") {
		t.Fatalf("business body missing after trailing closing strip:\n%s", result)
	}
	if strings.Contains(result, "감사합니다") {
		t.Fatalf("trailing closing leaked:\n%s", result)
	}
}

func TestFormatEmailForAnalysis_StripsTrailingKoreanSignoff(t *testing.T) {
	body := strings.Join([]string{
		"안녕하세요. 해남 인버터 배치 수량은 기존 안대로 유지하겠습니다.",
		"OCI 인터뷰 전까지 수정 도면만 다시 공유 부탁드립니다.",
		"",
		"감사합니다",
		"홍길동 드림",
	}, "\n")
	msg := &gmail.MessageDetail{
		From: "sender@example.com",
		To:   "me@example.com",
		Body: body,
	}

	result := FormatEmailForAnalysis(msg)
	if !strings.Contains(result, "OCI 인터뷰 전까지 수정 도면") {
		t.Fatalf("business body missing after signoff strip:\n%s", result)
	}
	for _, gone := range []string{"감사합니다", "홍길동 드림"} {
		if strings.Contains(result, gone) {
			t.Fatalf("trailing signoff leaked %q:\n%s", gone, result)
		}
	}
}

func TestFormatEmailForAnalysis_StripsBlankHeavySignature(t *testing.T) {
	body := strings.Join([]string{
		"안녕하세요. 납품 일정은 6월 24일 기준으로 유지하겠습니다.",
		"변경 요청은 오늘 17시 전까지 알려주세요.",
		"",
		"",
		"",
		"감사합니다",
		"",
		"",
		"홍길동 / 영업2팀",
		"",
		"T. 02-1234-5678",
		"",
		"M. 010-1111-2222",
		"",
		"E. sales@example.com",
	}, "\n")
	msg := &gmail.MessageDetail{
		From: "sender@example.com",
		To:   "me@example.com",
		Body: body,
	}

	result := FormatEmailForAnalysis(msg)
	if !strings.Contains(result, "변경 요청은 오늘 17시 전까지") {
		t.Fatalf("business body missing after blank-heavy signature strip:\n%s", result)
	}
	for _, gone := range []string{"홍길동", "02-1234-5678", "010-1111-2222", "E. sales@example.com"} {
		if strings.Contains(result, gone) {
			t.Fatalf("blank-heavy signature leaked %q:\n%s", gone, result)
		}
	}
}

func TestFormatEmailForAnalysis_KeepsStandaloneUsefulURL(t *testing.T) {
	body := strings.Join([]string{
		"견적서 원본은 아래 링크에서 확인해 주세요.",
		"https://example.com/quote/important",
	}, "\n")
	msg := &gmail.MessageDetail{
		From: "sender@example.com",
		To:   "me@example.com",
		Body: body,
	}

	result := FormatEmailForAnalysis(msg)
	if !strings.Contains(result, "https://example.com/quote/important") {
		t.Fatalf("useful standalone URL should remain:\n%s", result)
	}
}

func TestFormatEmailForAnalysis_StripsQuotedReplyHeadersAtTail(t *testing.T) {
	body := strings.Join([]string{
		"확인했습니다. LG 모듈 450W 견적 조건은 이번 주 안으로 정리하겠습니다.",
		"다만 단가 변경 가능성이 있어 최종 견적서는 금요일 오후에 다시 보내겠습니다.",
		"",
		"-----Original Message-----",
		"From: 김세미 <semi@example.com>",
		"Sent: Tuesday, June 16, 2026 5:20 PM",
		"To: 탑솔라",
		"Subject: Re: LG 모듈 견적",
		"이전 메일 본문입니다.",
	}, "\n")
	msg := &gmail.MessageDetail{
		From:    "김세미 <semi@example.com>",
		To:      "me@example.com",
		Subject: "Re: LG 모듈 견적",
		Body:    body,
	}

	result := FormatEmailForAnalysis(msg)
	if !strings.Contains(result, "이번 주 안으로 정리") ||
		!strings.Contains(result, "금요일 오후에 다시") {
		t.Fatalf("current reply missing after quote strip:\n%s", result)
	}
	for _, gone := range []string{"Original Message", "Sent: Tuesday", "이전 메일 본문입니다"} {
		if strings.Contains(result, gone) {
			t.Fatalf("quoted reply noise leaked %q:\n%s", gone, result)
		}
	}
}

func TestFormatEmailForAnalysis_DoesNotStripMidBodyLegalMention(t *testing.T) {
	body := strings.Join([]string{
		"이번 계약서는 법적 검토가 필요합니다.",
		"본 메일은 외부 공유 가능 여부를 판단하기 위한 초안 설명입니다.",
		"무단 배포 금지 문구를 계약서에 넣을지 검토해 주세요.",
		"검토 결과는 금요일까지 회신 부탁드립니다.",
	}, "\n")
	msg := &gmail.MessageDetail{
		From: "legal@example.com",
		To:   "me@example.com",
		Body: body,
	}

	result := FormatEmailForAnalysis(msg)
	for _, want := range []string{"법적 검토가 필요", "무단 배포 금지 문구", "금요일까지"} {
		if !strings.Contains(result, want) {
			t.Fatalf("legitimate body line stripped %q:\n%s", want, result)
		}
	}
}

func TestFormatEmailForAnalysis_DoesNotStripTinyReplySignature(t *testing.T) {
	body := "검토했습니다.\n\n감사합니다\n김대희 부장\n010-1234-5678"
	msg := &gmail.MessageDetail{
		From: "김대희 <kim@example.com>",
		To:   "me@example.com",
		Body: body,
	}

	result := FormatEmailForAnalysis(msg)
	if !strings.Contains(result, "김대희 부장") {
		t.Fatalf("tiny reply signature should remain to avoid over-stripping:\n%s", result)
	}
}

func TestFormatEmailForAnalysis_DoesNotStripTinyClosingOnlyReply(t *testing.T) {
	body := "검토했습니다.\n\n감사합니다"
	msg := &gmail.MessageDetail{
		From: "sender@example.com",
		To:   "me@example.com",
		Body: body,
	}

	result := FormatEmailForAnalysis(msg)
	if !strings.Contains(result, "감사합니다") {
		t.Fatalf("tiny reply closing should remain to avoid over-stripping:\n%s", result)
	}
}

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

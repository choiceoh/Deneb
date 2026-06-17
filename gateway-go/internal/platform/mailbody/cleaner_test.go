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

package wikitool

import (
	"context"
	"strings"
	"testing"

	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func writeWikiPage(t *testing.T, store *wiki.Store, path, title, category, content string) string {
	t.Helper()
	out, err := wikiWrite(context.Background(), store, nil,
		path, title, "", "", category, content,
		nil, nil, nil, "", nil, "", "", nil, nil, 0, "", "", "", true)
	if err != nil {
		t.Fatalf("wikiWrite: %v", err)
	}
	return out
}

// wiki write REPLACES a non-log body, but the result line said "업데이트"
// whether the write extended the page or destroyed it. Overwriting a person
// page with a contradictory one has to be visible in the result.
func TestWikiWriteReportsTheBodyItReplaced(t *testing.T) {
	store := newTestWikiStore(t)
	path := "인물/박지훈.md"

	created := writeWikiPage(t, store, path, "박지훈", "인물",
		"해봄에너지 신규 담당자.\n\n- 직급: 차장\n- 소속: 해봄에너지\n")
	if strings.Contains(created, "⚠️") {
		t.Fatalf("creating a page reported a replacement:\n%s", created)
	}

	replaced := writeWikiPage(t, store, path, "박지훈", "인물", "탑솔라 소속 부장.\n")
	if !strings.Contains(replaced, "⚠️") || !strings.Contains(replaced, "교체") {
		t.Errorf("a full-body replacement was reported as a plain update:\n%s", replaced)
	}
}

// Losing a named section is the actionable case: the model can put it back.
func TestWikiWriteNamesTheSectionsItDropped(t *testing.T) {
	store := newTestWikiStore(t)
	path := "인물/한지수.md"

	writeWikiPage(t, store, path, "한지수", "인물",
		"## 요약\n\n영업팀 과장\n\n## 핵심 사실\n\n- 2026-03 입사\n\n## 변경 이력\n\n- 2026-03: 생성\n")
	out := writeWikiPage(t, store, path, "한지수", "인물",
		"## 요약\n\n영업팀 부장으로 승진\n")

	for _, heading := range []string{"핵심 사실", "변경 이력"} {
		if !strings.Contains(out, heading) {
			t.Errorf("dropped section %q was not named:\n%s", heading, out)
		}
	}
	if strings.Contains(out, "사라진 기존 섹션: 요약") {
		t.Errorf("a section that survived was reported as lost:\n%s", out)
	}
}

// A write that keeps every section says nothing — otherwise the warning fires
// on every ordinary edit and stops meaning anything.
func TestWikiWriteStaysQuietWhenNothingIsLost(t *testing.T) {
	store := newTestWikiStore(t)
	path := "인물/조민서.md"

	writeWikiPage(t, store, path, "조민서", "인물", "## 요약\n\n기획팀\n")
	kept := writeWikiPage(t, store, path, "조민서", "인물", "## 요약\n\n기획팀 팀장\n")
	if strings.Contains(kept, "⚠️") {
		t.Errorf("an edit that kept every section warned anyway:\n%s", kept)
	}

	extended := writeWikiPage(t, store, path, "조민서", "인물",
		"## 요약\n\n기획팀 팀장\n\n## 핵심 사실\n\n- 신규\n")
	if strings.Contains(extended, "⚠️") {
		t.Errorf("adding a section warned:\n%s", extended)
	}
}

// The project-log path appends, so nothing is ever replaced there.
func TestWikiWriteLogAppendNeverReportsAReplacement(t *testing.T) {
	store := newTestWikiStore(t)
	logPage := wiki.NewPage("영산고 진행 로그", "프로젝트", nil)
	logPage.Meta.Type = "log"
	logPage.Body = "## 2026-07-01 계약\n\n계약 체결\n"
	if err := store.WritePage(wiki.LogPagePath("영산고"), logPage); err != nil {
		t.Fatalf("seed log: %v", err)
	}

	out, err := wikiWrite(context.Background(), store, nil,
		wiki.LogPagePath("영산고"), "영산고 진행 로그", "", "", "프로젝트", "자재 입고 완료",
		nil, nil, nil, "", nil, "", "", nil, nil, 0, "log", "", "", false)
	if err != nil {
		t.Fatalf("wikiWrite: %v", err)
	}
	if strings.Contains(out, "⚠️") {
		t.Errorf("the append path reported a replacement:\n%s", out)
	}
	got := testutil.Must(store.ReadPage(wiki.LogPagePath("영산고")))
	if !strings.Contains(got.Body, "계약 체결") {
		t.Fatalf("append lost prior entries:\n%s", got.Body)
	}
}

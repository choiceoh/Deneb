package recall

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

// TestRecallWikiEvidence_ProjectAnchor: a query naming a known project pins
// that project's 대표페이지 (with its 현재 상태) at the top of the wiki evidence,
// above every keyword hit — even when BM25 would prefer detail pages.
func TestRecallWikiEvidence_ProjectAnchor(t *testing.T) {
	dir := t.TempDir()
	store, err := wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	rep := wiki.NewPage("기아 화성", "프로젝트", nil)
	rep.Body = "# 기아 화성\n\n## 현재 상태\n- 모듈 RFQ 회신 완료\n"
	if err := store.WritePage(wiki.RepPagePath("기아-화성"), rep); err != nil {
		t.Fatal(err)
	}
	detail := wiki.NewPage("기아 AL 화성 치장장 태양광 배치", "프로젝트", nil)
	detail.Body = "# 배치\n기아 화성 치장장 배치 검토 내용이 길게 이어진다."
	if err := store.WritePage("프로젝트/기아-화성/치장장-배치.md", detail); err != nil {
		t.Fatal(err)
	}

	evidence := recallWikiEvidence(context.Background(), store, []string{"기아 화성 근황 알려줘"}, "")
	if len(evidence) == 0 {
		t.Fatal("no evidence returned")
	}
	top := evidence[0]
	if top.Source != wiki.RepPagePath("기아-화성") || top.Query != "project-anchor" {
		t.Fatalf("top evidence = %+v, want the project anchor", top)
	}
	if top.Score <= 0.80+1.1 {
		t.Errorf("anchor score %v must outrank any keyword hit", top.Score)
	}
	if !strings.Contains(top.Note, "현재 상태") || !strings.Contains(top.Note, "모듈 RFQ 회신 완료") {
		t.Errorf("anchor note missing 현재 상태: %q", top.Note)
	}

	// The anchor dedupes against keyword hits for the same page: no duplicate
	// rep-page evidence rows.
	seen := 0
	for _, ev := range evidence {
		if ev.Source == wiki.RepPagePath("기아-화성") {
			seen++
		}
	}
	if seen != 1 {
		t.Errorf("rep page appears %d times, want 1", seen)
	}

	// A query naming no project produces no anchor rows.
	for _, ev := range recallWikiEvidence(context.Background(), store, []string{"구리값 어때"}, "") {
		if ev.Query == "project-anchor" {
			t.Errorf("unexpected anchor for a project-less query: %+v", ev)
		}
	}
}

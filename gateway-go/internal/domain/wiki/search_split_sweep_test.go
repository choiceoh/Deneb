package wiki

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// search_split_sweep_test.go — the Deneb-stack instrument for the Infini Memory
// split-threshold finding (arXiv:2606.10677, Table 3): retrieval over topic
// documents peaks near ~5000 tokens and the curve is ASYMMETRIC (too-large costs
// more than too-small). This sweeps MaxPageBytes over a corpus of large
// multi-topic pages, splitting each page at the threshold exactly as the dreamer
// does (Store.SplitPage), and reports recall (hit@k) per threshold — the
// Deneb/Korean counterpart of the paper's Table 3.
//
// What this asserts vs. emits:
//   - ASSERT (de-risks the 50->32 KB default): splitting a large page into H2
//     sub-pages must NOT lose facts — hit@3 stays high as the threshold drops. A
//     fact in a sub-page is still retrievable.
//   - EMIT (WIKI_SPLIT_SWEEP): the hit@1 / hit@3 curve per threshold. The
//     production ranking-precision curve must be measured on the REAL wiki (DGX
//     recall-metric), not this synthetic corpus, so the directional "smaller
//     ranks better" claim is reported, never asserted — that would be the
//     spurious-improvement trap the optimization rules warn against.

// sweepPlant is one fact planted in one H2 section of one page, with the query
// that should retrieve it and a marker substring proving the right fact was hit.
type sweepPlant struct {
	path       string
	title      string
	heading    string
	anchorLine string
	query      string
	marker     string
}

func sweepPlants() []sweepPlant {
	return []sweepPlant{
		{"프로젝트/탑솔라/대표.md", "탑솔라 ESS", "자금 조달", "탑솔라 ESS 프로젝트파이낸싱 약정액은 920억원으로 확정되었다.", "탑솔라 ESS 프로젝트파이낸싱 약정액", "920억원"},
		{"프로젝트/탑솔라/대표.md", "탑솔라 ESS", "인허가", "탑솔라 ESS 발전사업허가는 산업부 조건부 승인을 받았다.", "탑솔라 발전사업허가 산업부", "조건부 승인"},
		{"프로젝트/한빛풍력/대표.md", "한빛 풍력", "공급 계약", "한빛 풍력 터빈 공급계약은 베스타스와 체결되었다.", "한빛 풍력 터빈 공급계약", "베스타스"},
		{"프로젝트/한빛풍력/대표.md", "한빛 풍력", "인력 배치", "한빛 풍력 현장소장은 박정우 부장이 맡는다.", "한빛 풍력 현장소장", "박정우"},
		{"프로젝트/새만금태양광/대표.md", "새만금 태양광", "사업 일정", "새만금 태양광 상업운전 개시 목표는 2027년 3분기다.", "새만금 태양광 상업운전 개시", "2027년 3분기"},
		{"프로젝트/새만금태양광/대표.md", "새만금 태양광", "자금 구조", "새만금 태양광 자기자본 비율은 25퍼센트로 설계되었다.", "새만금 태양광 자기자본 비율", "25퍼센트"},
		{"프로젝트/영암ESS/대표.md", "영암 ESS", "공급사 선정", "영암 ESS 배터리 공급사는 삼성SDI로 선정되었다.", "영암 ESS 배터리 공급사", "삼성SDI"},
		{"프로젝트/영암ESS/대표.md", "영암 ESS", "계통 연계", "영암 ESS 계통연계 심의는 한전 승인 대기중이다.", "영암 ESS 계통연계 한전", "대기중"},
	}
}

// koreanFiller returns generic Korean prose of at least targetBytes, containing
// none of the plant markers so it only dilutes (never falsely satisfies a hit).
func koreanFiller(targetBytes int) string {
	sentences := []string{
		"이 항목은 프로젝트 진행 상황을 기록한 내부 메모이며 추후 갱신될 수 있다.",
		"관련 담당자와 협의한 내용을 정리해 두었고 세부 사항은 별도로 관리한다.",
		"세부 일정과 예산은 분기별 검토 회의에서 다시 확인하기로 했다.",
		"외부 업체와의 일반 조건은 표준 양식을 따르되 예외는 따로 적는다.",
		"현장 점검 결과와 일반적인 후속 조치 사항을 함께 보관한다.",
	}
	var b strings.Builder
	for i := 0; b.Len() < targetBytes; i++ {
		b.WriteString(sentences[i%len(sentences)])
		b.WriteByte('\n')
	}
	return b.String()
}

// buildSweepCorpus assembles large multi-topic pages: each page gets its planted
// sections plus generic filler sections padding it past ~30 KB, so a maxed-out
// page buries each fact among unrelated topics (the dilution the split targets).
func buildSweepCorpus() map[string]*Page {
	const pageTargetBytes = 30 * 1024
	const sectionFillerBytes = 4500

	bodies := map[string]*strings.Builder{}
	titles := map[string]string{}
	for _, p := range sweepPlants() {
		titles[p.path] = p.title
		b := bodies[p.path]
		if b == nil {
			b = &strings.Builder{}
			b.WriteString("# " + p.title + "\n\n")
			bodies[p.path] = b
		}
		b.WriteString("## " + p.heading + "\n\n")
		b.WriteString(p.anchorLine + "\n\n")
		b.WriteString(koreanFiller(sectionFillerBytes) + "\n\n")
	}

	fillerHeads := []string{"개요", "회의록", "현장 점검", "리스크", "후속 조치", "참고 자료", "연락 이력", "예산 메모"}
	pages := map[string]*Page{}
	for path, b := range bodies {
		for i := 0; b.Len() < pageTargetBytes; i++ {
			b.WriteString("## " + fillerHeads[i%len(fillerHeads)] + fmt.Sprintf(" %d\n\n", i))
			b.WriteString(koreanFiller(sectionFillerBytes) + "\n\n")
		}
		pg := NewPage(titles[path], "프로젝트", nil)
		pg.Body = strings.TrimSpace(b.String())
		pages[path] = pg
	}
	return pages
}

func measureSweepRecall(t *testing.T, store *Store, plants []sweepPlant) (hit1, hit3 float64) {
	t.Helper()
	ctx := context.Background()
	var h1, h3 int
	for _, p := range plants {
		results, err := store.Search(ctx, p.query, 3)
		if err != nil {
			t.Fatalf("Search %q: %v", p.query, err)
		}
		for i, r := range results {
			page, err := store.ReadPage(r.Path)
			if err != nil || page == nil {
				continue
			}
			if strings.Contains(page.Body, p.marker) {
				h3++
				if i == 0 {
					h1++
				}
				break
			}
		}
	}
	n := float64(len(plants))
	return float64(h1) / n, float64(h3) / n
}

func countMarkdownPages(t *testing.T, root string) int {
	t.Helper()
	n := 0
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // skip unreadable entries
		}
		base := filepath.Base(p)
		if filepath.Ext(p) == ".md" && base != "index.md" && base != "_index.md" && base != "log.md" {
			n++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return n
}

func TestSplitThresholdSweep(t *testing.T) {
	plants := sweepPlants()

	// 1<<30 = effectively no split (baseline whole pages); then progressively
	// smaller caps so SplitPage fires and fragments each page into more sub-pages.
	thresholds := []int{1 << 30, 24 * 1024, 12 * 1024, 6 * 1024}

	var basePages, smallPages int
	for ti, thr := range thresholds {
		dir := t.TempDir()
		store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
		if err != nil {
			t.Fatalf("NewStore: %v", err)
		}

		// Fresh corpus each pass (WritePage may mutate the page via redaction).
		for path, pg := range buildSweepCorpus() {
			if err := store.WritePage(path, pg); err != nil {
				t.Fatalf("WritePage %s: %v", path, err)
			}
		}
		// Split exactly as the dreamer does after a consolidation write.
		for path := range buildSweepCorpus() {
			if _, err := store.SplitPage(path, thr); err != nil {
				t.Fatalf("SplitPage %s: %v", path, err)
			}
		}
		if err := store.RebuildIndex(); err != nil {
			t.Fatalf("RebuildIndex: %v", err)
		}

		pageCount := countMarkdownPages(t, store.Dir())
		hit1, hit3 := measureSweepRecall(t, store, plants)

		// The Deneb-stack Table 3 row. Read this curve on the REAL wiki (DGX) for
		// the production ranking-precision trend; here it is informational.
		t.Logf("WIKI_SPLIT_SWEEP threshold=%dKB pages=%d hit@1=%.2f hit@3=%.2f",
			thr/1024, pageCount, hit1, hit3)

		// Invariant (de-risks lowering MaxPageBytes): splitting must not LOSE
		// facts. A fact moved into a sub-page must still be retrievable.
		if hit3 < 0.75 {
			t.Errorf("threshold %dKB: hit@3 %.2f too low — splitting lost retrievable facts", thr/1024, hit3)
		}

		switch ti {
		case 0:
			basePages = pageCount
		case len(thresholds) - 1:
			smallPages = pageCount
		}
		store.Close()
	}

	// The instrument must actually exercise splitting, else the sweep is a no-op.
	if smallPages <= basePages {
		t.Errorf("expected the smallest threshold to fragment pages (base=%d small=%d)", basePages, smallPages)
	}
}

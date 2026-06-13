// recall_bench_test.go — measurable recall quality.
//
// Every recall knob (evidence cap, score weights, source mix, query
// normalization) was tuned by feel until now. This bench pins a synthetic
// Korean corpus with known ground truth and scores buildRecallPreflight's
// evidence hit rate, so recall changes are judged by a number instead of
// vibes — the same role the graph bench plays for wiki edges.
//
// Two consumers:
//   - CI: TestRecallQuality asserts the hit-rate floor (regression gate).
//   - Tuning: `scripts/dev/recall-metric.sh` greps the RECALL_METRIC line for
//     the iterate.sh optimization loop (metric_value=<pct>).
//
// The corpus runs without an embedder (CI has no GPU), so it measures the
// lexical+diary+structural path; semantic-only paraphrase recall is exercised
// separately in wiki semantic tests.
package chat

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

// recallBenchCase is one ground-truth question: the recall output must
// contain every `wantAll` substring; `wantNone` must be absent.
type recallBenchCase struct {
	name     string
	question string
	wantAll  []string
	wantNone []string
}

// recallBenchFloorPct is the regression floor. The corpus scores 100% at the
// time of writing; the floor sits lower so legitimate ranking changes don't
// flap CI, while a broken source or query path (the failure modes that
// actually happened) still trips it.
const recallBenchFloorPct = 80

func buildRecallBenchStore(t *testing.T) *wiki.Store {
	t.Helper()
	dir := t.TempDir()
	store, err := wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	pages := []struct {
		path string
		page *wiki.Page
	}{
		{"거래/hyundai-ulsan.md", &wiki.Page{
			Meta: wiki.Frontmatter{ID: "hyundai-ulsan", Title: "현대차 울산공장 모듈 납품", Category: "거래",
				Summary: "현대차 울산공장 태양광 모듈 납품 건, 결제기한 6월 말", Tags: []string{"현대차", "울산", "모듈"}, Importance: 0.9},
			Body: "550W 모듈 2,000장 견적 발송 완료. 결제기한은 6월 말. 담당은 김민준 부장.",
		}},
		{"인물/kim-minjun.md", &wiki.Page{
			Meta: wiki.Frontmatter{ID: "kim-minjun", Title: "김민준 부장", Category: "인물",
				Summary: "현대차 울산공장 구매팀 부장, 모듈 납품 창구", Tags: []string{"현대차", "구매팀"}, Importance: 0.8},
			Body: "현대차 울산공장 구매팀. 연락은 주로 메일, 회신이 빠른 편.",
		}},
		{"프로젝트/dgx-spark.md", &wiki.Page{
			Meta: wiki.Frontmatter{ID: "dgx-spark", Title: "DGX Spark 게이트웨이", Category: "프로젝트",
				Summary: "로컬 추론 게이트웨이 운영, step3p7 메인 모델", Tags: []string{"dgx", "게이트웨이"}, Importance: 0.9},
			Body: "메인 모델은 step3p7. 게이트웨이 포트는 18789. 모든 추론은 로컬에서 수행한다.",
		}},
		{"운영시스템/backup.md", &wiki.Page{
			Meta: wiki.Frontmatter{ID: "memory-backup", Title: "기억 백업 체계", Category: "운영시스템",
				Summary: "spark4tb로 일일 메모리 백업, 보존 30일", Tags: []string{"백업", "spark4tb"}, Importance: 0.7},
			Body: "매일 자정 무렵 spark4tb 스토리지 노드로 tar.gz 전송. 보존 기간은 30일.",
		}},
	}
	for _, p := range pages {
		if err := store.WritePage(p.path, p.page); err != nil {
			t.Fatalf("WritePage %s: %v", p.path, err)
		}
	}
	for _, entry := range []string{
		"탑솔라 루프탑 RE100 건으로 남도에코와 통화. 다음 주 화요일 실사 일정 확정.",
		"주간보고 PDF 자동화는 모닝레터 패턴을 재사용하기로 결정했다.",
	} {
		if err := store.AppendDiary(entry); err != nil {
			t.Fatalf("AppendDiary: %v", err)
		}
	}
	return store
}

func recallBenchCases() []recallBenchCase {
	return []recallBenchCase{
		{
			name:     "deal-deadline-keyword",
			question: "전에 정리했던 현대차 울산 납품 건 결제기한이 언제였지?",
			wantAll:  []string{"거래/hyundai-ulsan.md"},
		},
		{
			name:     "person-lookup",
			question: "지난번 김민준 부장 관련해서 얘기했던 거 계속하자",
			wantAll:  []string{"인물/kim-minjun.md"},
		},
		{
			name:     "project-fact",
			question: "아까 DGX 게이트웨이 포트 뭐라고 했지?",
			wantAll:  []string{"프로젝트/dgx-spark.md"},
		},
		{
			name:     "ops-fact",
			question: "백업 보존 기간 전에 정한 거 기억나?",
			wantAll:  []string{"운영시스템/backup.md"},
		},
		{
			name:     "diary-event",
			question: "저번에 남도에코랑 통화했던 내용 뭐였지?",
			wantAll:  []string{"남도에코"},
		},
		{
			name:     "diary-decision",
			question: "주간보고 자동화 방식 전에 어떻게 결정했더라?",
			wantAll:  []string{"모닝레터"},
		},
		{
			name:     "topicless-recency-fallback",
			question: "아까 뭐였지?",
			wantAll:  []string{"recall-context"}, // recent-diary fallback must produce a fenced block
		},
		{
			name:     "no-evidence-is-honest",
			question: "전에 화성 이주 계획 얘기했던 거 기억나?",
			wantAll:  []string{"근거를 찾지 못"},                                 // explicit cue + nothing found → honest notice
			wantNone: []string{"거래/hyundai-ulsan.md", "프로젝트/dgx-spark.md"}, // must not hallucinate unrelated refs
		},
	}
}

// runRecallBench scores the corpus and returns (hits, total).
func runRecallBench(t *testing.T, verbose bool) (int, int) {
	t.Helper()
	store := buildRecallBenchStore(t)
	cases := recallBenchCases()
	hits := 0
	for _, c := range cases {
		out, _ := buildRecallPreflight(context.Background(),
			RunParams{SessionKey: "client:main", Message: c.question},
			runDeps{wikiStore: store},
			nil,
		)
		ok := true
		for _, want := range c.wantAll {
			if !strings.Contains(out, want) {
				ok = false
				if verbose {
					t.Logf("MISS %-28s want %q\n--- evidence ---\n%s", c.name, want, out)
				}
			}
		}
		for _, bad := range c.wantNone {
			if strings.Contains(out, bad) {
				ok = false
				if verbose {
					t.Logf("LEAK %-28s must not contain %q", c.name, bad)
				}
			}
		}
		if ok {
			hits++
			if verbose {
				t.Logf("HIT  %-28s", c.name)
			}
		}
	}
	return hits, len(cases)
}

// TestRecallQuality is the CI regression gate + the tuning metric emitter.
func TestRecallQuality(t *testing.T) {
	hits, total := runRecallBench(t, true)
	pct := hits * 100 / total
	// Stable, grep-able line for scripts/dev/recall-metric.sh.
	fmt.Printf("RECALL_METRIC hits=%d total=%d pct=%d\n", hits, total, pct)
	if pct < recallBenchFloorPct {
		t.Fatalf("recall quality %d%% below floor %d%% (%d/%d)", pct, recallBenchFloorPct, hits, total)
	}
}

// TestRecallSourceAttribution reports which backend produced the evidence per
// case — the data the backend-consolidation decision needs. Informational
// (no floor): the corpus has no embedder/polaris/hindsight, so it attributes
// the lexical sources; production attribution comes from the per-turn
// "sources" field in the recall preflight Info logs.
func TestRecallSourceAttribution(t *testing.T) {
	store := buildRecallBenchStore(t)
	agg := map[string]int{}
	for _, c := range recallBenchCases() {
		out, _ := buildRecallPreflight(context.Background(),
			RunParams{SessionKey: "client:main", Message: c.question},
			runDeps{wikiStore: store},
			nil,
		)
		for _, src := range []string{"wiki", "diary", "polaris", "transcript", "hindsight"} {
			n := strings.Count(out, "source="+src+" ")
			if n > 0 {
				agg[src] += n
				t.Logf("%-28s %-10s rows=%d", c.name, src, n)
			}
		}
	}
	parts := make([]string, 0, len(agg))
	for _, src := range []string{"wiki", "diary", "polaris", "transcript", "hindsight"} {
		if n := agg[src]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", src, n))
		}
	}
	fmt.Printf("RECALL_SOURCES %s\n", strings.Join(parts, " "))
	if len(agg) == 0 {
		t.Error("no source attribution found in any case output")
	}
}

// TestRecallFactRevisionSupersession exercises the "selective forgetting"
// competency (MemoryAgentBench arXiv:2507.05257): when a stored fact is revised,
// recall must surface the new value and must never present the old value as a
// current fact. Deneb's mechanism is the wiki supersession marker — even if a
// demoted superseded page still surfaces, it carries a staleness marker so the
// model does not cite the old value. This is an incremental, turn-by-turn
// scenario (seed → query → revise → re-query), not a static corpus snapshot.
func TestRecallFactRevisionSupersession(t *testing.T) {
	dir := t.TempDir()
	store, err := wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	const oldPath, newPath = "거래/acme-old.md", "거래/acme-new.md"
	recall := func(msg string) string {
		out, _ := buildRecallPreflight(context.Background(),
			RunParams{SessionKey: "client:main", Message: msg},
			runDeps{wikiStore: store}, nil)
		return out
	}

	// Phase 1 — seed the original fact and confirm it surfaces cleanly.
	if err := store.WritePage(oldPath, &wiki.Page{
		Meta: wiki.Frontmatter{ID: "acme", Title: "에이콘 상사 담당자", Category: "거래",
			Summary: "에이콘 상사 구매 담당자 정보", Tags: []string{"에이콘"}, Importance: 0.8},
		Body: "에이콘 상사 구매 담당자는 김민준 부장이다.",
	}); err != nil {
		t.Fatalf("WritePage old: %v", err)
	}
	before := recall("전에 에이콘 상사 담당자 누구였지?")
	if !strings.Contains(before, "김민준") {
		t.Fatalf("phase 1: original fact must surface, got %q", before)
	}
	if strings.Contains(before, "대체됨") {
		t.Fatalf("phase 1: a current page must carry no staleness marker, got %q", before)
	}

	// Phase 2 — the fact changes: write the new page and supersede the old one
	// (the dreamer's MarkSuperseded path).
	if err := store.WritePage(newPath, &wiki.Page{
		Meta: wiki.Frontmatter{ID: "acme-v2", Title: "에이콘 상사 담당자 (갱신)", Category: "거래",
			Summary: "에이콘 상사 구매 담당자 갱신", Tags: []string{"에이콘"}, Importance: 0.9},
		Body: "에이콘 상사 구매 담당자는 박수진 과장으로 교체되었다.",
	}); err != nil {
		t.Fatalf("WritePage new: %v", err)
	}
	if err := store.MarkSuperseded(oldPath, newPath); err != nil {
		t.Fatalf("MarkSuperseded: %v", err)
	}

	// Phase 3 — re-query. The new fact must surface, and the old page must never
	// appear as an unmarked current fact.
	after := recall("전에 에이콘 상사 담당자 누구였지?")
	if !strings.Contains(after, "박수진") {
		t.Fatalf("phase 3: revised fact must surface, got %q", after)
	}
	if strings.Contains(after, oldPath) && !strings.Contains(after, "대체됨") {
		t.Fatalf("phase 3: superseded old page surfaced without a staleness marker — model could cite the stale value, got %q", after)
	}
}

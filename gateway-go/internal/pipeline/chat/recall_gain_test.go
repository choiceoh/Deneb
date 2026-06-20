// recall_gain_test.go — does the curated wiki layer actually EARN its weight?
//
// CL-Bench (arXiv:2606.05661) makes one demand of any memory layer: measure its
// *gain* — the lift over running the same system without it — not its raw score.
// Its headline finding is that eagerly-curated memory (ACE playbook, notepad
// summaries) rarely beats just keeping raw text and retrieving at query time,
// because curation introduces stale beliefs and spurious generalizations.
//
// Deneb is a hybrid: diary + polaris are raw + query-time retrieval (the pattern
// CL-Bench favors), and the WIKI is the one eagerly-curated layer (the dreamer
// LLM-synthesizes diary into pages, merges, marks supersedes). This bench asks
// the CL-Bench question of that wiki: seed the SAME fact into both the raw diary
// (as it was first observed) AND the curated wiki page (as the dreamer would
// synthesize it), then score recall with the wiki ON vs OFF.
//
//	gain = hit_rate(wiki+diary)  -  hit_rate(diary-only)
//
// A wiki page that only restates a fact the raw diary already surfaces is pure
// redundancy (gain 0) — exactly what retired Hindsight turned out to be. Real
// gain shows up in two places the raw lexical path provably cannot cover:
//  1. terminology normalization — the query paraphrases away from the raw
//     diary's words but matches the curated title/summary/tags;
//  2. stale-belief disambiguation — a fact was revised; raw diary keeps BOTH the
//     old and new entries with no ordering signal, so it can surface the stale
//     value as current. The wiki's supersede marker demotes/flags the old page.
//
// Lexical path only (no embedder on CI) — so this measures the wiki's LEXICAL
// gain (curated metadata as extra match surface) + the supersede guard. The
// semantic-paraphrase gain needs the GPU host and is a separate measurement.
package chat

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

// A fact lives in two forms: the raw diary line(s) it was first jotted as, and
// the curated wiki page the dreamer would synthesize from it. supersededBy != ""
// means this wiki page is the OLD value of a later revision (diary keeps it raw).
type gainFact struct {
	diary        []string // raw diary entries (verbatim observations)
	wikiPath     string   // curated wiki page path ("" = no wiki page)
	wikiPage     *wiki.Page
	supersededBy string // path of the page that replaces this one (revision)
}

type gainQuery struct {
	name     string
	question string
	wantAll  []string // recall output must surface these (the answer)
	staleOld string   // a revised-away value that must NOT appear as a current fact
}

func recallGainCorpus() ([]gainFact, []gainQuery) {
	page := func(id, title, cat, summary, body string, tags ...string) *wiki.Page {
		return &wiki.Page{
			Meta: wiki.Frontmatter{ID: id, Title: title, Category: cat, Summary: summary, Tags: tags, Importance: 0.8},
			Body: body,
		}
	}
	facts := []gainFact{
		// Redundant: query shares keywords with the raw diary, so diary alone hits.
		{
			diary:    []string{"현대차 울산 550W 모듈 2,000장 견적 발송. 결제는 6월 말까지."},
			wikiPath: "거래/hyundai-ulsan.md",
			wikiPage: page("hyundai", "현대차 울산공장 모듈 납품", "거래", "결제기한 6월 말", "550W 2,000장, 결제 6월 말, 담당 김민준 부장.", "현대차", "울산", "모듈", "결제"),
		},
		{
			diary:    []string{"게이트웨이 포트는 18789. 메인 모델은 step3p7."},
			wikiPath: "프로젝트/dgx-spark.md",
			wikiPage: page("dgx", "DGX Spark 게이트웨이", "프로젝트", "포트 18789, 메인 step3p7", "게이트웨이 포트 18789, 추론은 로컬.", "dgx", "게이트웨이", "포트"),
		},
		{
			diary:    []string{"매일 자정 무렵 spark4tb로 백업. 30일 보관."},
			wikiPath: "시스템/backup.md",
			wikiPage: page("backup", "기억 백업 체계", "시스템", "spark4tb 일일 백업, 보존 30일", "매일 자정 spark4tb, 보존 30일.", "백업", "spark4tb"),
		},
		{
			diary:    []string{"에코프로 케이블 1,950m 견적 회신함."},
			wikiPath: "거래/ecopro-cable.md",
			wikiPage: page("ecopro", "에코프로 케이블 견적", "거래", "케이블 1,950m 견적", "에코프로 케이블 1,950m.", "에코프로", "케이블", "견적"),
		},
		// Terminology normalization: raw diary uses colloquial words; the curated
		// wiki carries the normalized terms the query actually uses.
		{
			// raw says "현장 보러" / "통화"; wiki carries RE100·루프탑·실사.
			diary:    []string{"남도에코랑 통화함. 다음 주 화요일에 현장 보러 가기로."},
			wikiPath: "거래/namdo-re100.md",
			wikiPage: page("namdo", "남도에코에너지 RE100 루프탑 실사", "거래", "다음 주 화요일 현장 실사 확정", "RE100 루프탑 건, 화요일 실사.", "남도에코", "RE100", "루프탑", "실사"),
		},
		{
			// raw says "아침편지"; wiki normalizes to "모닝레터".
			diary:    []string{"주간보고 PDF 자동화는 아침편지 만들던 방식 그대로 쓰기로."},
			wikiPath: "업무/weekly-report.md",
			wikiPage: page("weekly", "주간업무보고 자동화", "업무", "모닝레터 패턴 재사용 결정", "주간보고 자동화, 모닝레터 패턴 재사용.", "주간보고", "자동화", "모닝레터"),
		},
		// Stale-belief: the fact was revised. Diary keeps BOTH entries raw (no
		// ordering signal); wiki supersedes the old page.
		{
			diary:        []string{"에이콘 상사 구매 담당자는 이태호 차장."},
			wikiPath:     "거래/acme-old.md",
			wikiPage:     page("acme", "에이콘 상사 담당자", "거래", "구매 담당자 정보", "에이콘 상사 구매 담당자는 이태호 차장.", "에이콘"),
			supersededBy: "거래/acme-new.md",
		},
		{
			diary:    []string{"에이콘 담당자 박수진 과장으로 교체됨."},
			wikiPath: "거래/acme-new.md",
			wikiPage: page("acme2", "에이콘 상사 담당자 (갱신)", "거래", "담당자 갱신", "에이콘 상사 구매 담당자는 박수진 과장.", "에이콘"),
		},
	}
	queries := []gainQuery{
		{"deadline", "전에 현대차 울산 모듈 결제기한 언제까지였지?", []string{"6월"}, ""},
		{"port", "아까 DGX 게이트웨이 포트 번호 뭐였지?", []string{"18789"}, ""},
		{"backup", "백업 보존 기간 전에 며칠로 정했더라?", []string{"30일"}, ""},
		{"ecopro", "에코프로 케이블 견적 몇 미터였지?", []string{"1,950"}, ""},
		// paraphrased away from the raw diary wording — needs the curated wiki.
		{"namdo-paraphrase", "RE100 루프탑 실사 일정 언제였지?", []string{"화요일"}, ""},
		{"weekly-paraphrase", "주간보고 자동화 모닝레터 패턴으로 가기로 했었나?", []string{"모닝레터"}, ""},
		// revised fact — must surface the new value, must not present the old as current.
		{"acme-revision", "에이콘 상사 지금 구매 담당자 누구지?", []string{"박수진"}, "이태호"},
	}
	return facts, queries
}

// buildGainStore seeds the corpus for one mode: "both" (wiki+diary), "diaryonly"
// (raw retrieval only — the CL-Bench "stateless of the wiki" baseline), or
// "wikionly" (curated only). Same facts, different layers present.
func buildGainStore(t *testing.T, mode string) *wiki.Store {
	t.Helper()
	dir := t.TempDir()
	store, err := wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	facts, _ := recallGainCorpus()
	if mode != "diaryonly" {
		for _, f := range facts {
			if f.wikiPath != "" {
				if err := store.WritePage(f.wikiPath, f.wikiPage); err != nil {
					t.Fatalf("WritePage %s: %v", f.wikiPath, err)
				}
			}
		}
		for _, f := range facts {
			if f.supersededBy != "" {
				if err := store.MarkSuperseded(f.wikiPath, f.supersededBy); err != nil {
					t.Fatalf("MarkSuperseded %s: %v", f.wikiPath, err)
				}
			}
		}
	}
	if mode != "wikionly" {
		for _, f := range facts {
			for _, d := range f.diary {
				if err := store.AppendDiary(d); err != nil {
					t.Fatalf("AppendDiary: %v", err)
				}
			}
		}
	}
	return store
}

func recallOnce(store *wiki.Store, question string) string {
	out, _ := buildRecallPreflight(context.Background(),
		RunParams{SessionKey: "client:main", Message: question},
		runDeps{wikiStore: store}, nil)
	return out
}

// gainHits scores how many queries surface their answer under one mode.
func gainHits(t *testing.T, mode string, log bool) (hits int) {
	store := buildGainStore(t, mode)
	_, queries := recallGainCorpus()
	for _, q := range queries {
		out := recallOnce(store, q.question)
		ok := true
		for _, w := range q.wantAll {
			if !strings.Contains(out, w) {
				ok = false
			}
		}
		if ok {
			hits++
		}
		if log {
			mark := "miss"
			if ok {
				mark = "HIT "
			}
			t.Logf("[%-9s] %s %-18s want=%v", mode, mark, q.name, q.wantAll)
		}
	}
	return hits
}

// TestRecallWikiGain is the headline measurement: the wiki's recall gain over a
// diary-only (raw retrieval) baseline, on a corpus where every fact lives in
// both layers. Informational by default (no hard floor) — it answers "does the
// curated layer earn its weight", which is a tuning/architecture question, not a
// pass/fail gate.
func TestRecallWikiGain(t *testing.T) {
	both := gainHits(t, "both", true)
	diaryOnly := gainHits(t, "diaryonly", true)
	wikiOnly := gainHits(t, "wikionly", false)
	_, queries := recallGainCorpus()
	total := len(queries)
	gain := both - diaryOnly
	// Stable, grep-able line for scripts/dev/recall-gain.sh.
	fmt.Printf("RECALL_GAIN both=%d diaryonly=%d wikionly=%d gain=%d total=%d\n",
		both, diaryOnly, wikiOnly, gain, total)
	if gain < 0 {
		t.Errorf("wiki recall gain is negative (%d): the curated layer is hurting recall vs raw diary", gain)
	}
}

// TestRecallStaleBeliefGuard isolates the one thing raw retrieval provably cannot
// do: when a fact is revised, the diary keeps BOTH entries with no ordering
// signal, so diary-only recall can surface the stale value as current. The wiki
// supersede marker should prevent that. This is the clearest, non-redundant
// source of wiki gain (CL-Bench's #1 unsolved failure: discarding stale beliefs).
func TestRecallStaleBeliefGuard(t *testing.T) {
	_, queries := recallGainCorpus()
	var stale gainQuery
	for _, q := range queries {
		if q.staleOld != "" {
			stale = q
		}
	}
	if stale.staleOld == "" {
		t.Fatal("no stale-belief query in corpus")
	}

	// diary-only: the old entry is raw with no supersede signal — measure leak.
	rawOut := recallOnce(buildGainStore(t, "diaryonly"), stale.question)
	rawLeaks := strings.Contains(rawOut, stale.staleOld) && !strings.Contains(rawOut, "대체됨")

	// both (wiki present): supersede marker should demote/flag the old value.
	wikiOut := recallOnce(buildGainStore(t, "both"), stale.question)
	wikiLeaks := strings.Contains(wikiOut, stale.staleOld) && !strings.Contains(wikiOut, "대체됨")
	wikiSurfacesNew := strings.Contains(wikiOut, stale.wantAll[0])

	fmt.Printf("RECALL_STALELEAK diaryonly=%v both=%v new_surfaced=%v\n", rawLeaks, wikiLeaks, wikiSurfacesNew)
	t.Logf("stale-old=%q  diary-only output:\n%s", stale.staleOld, rawOut)
	t.Logf("both output:\n%s", wikiOut)

	if wikiLeaks {
		t.Errorf("wiki path leaked the stale value %q as a current fact (supersede guard failed)", stale.staleOld)
	}
	if !wikiSurfacesNew {
		t.Errorf("wiki path failed to surface the revised value %q", stale.wantAll[0])
	}
}

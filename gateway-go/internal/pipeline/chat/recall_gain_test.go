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
//     old and new entries with no ordering signal (it can even rank the stale one
//     first), so a reader cannot tell which is current. The wiki ranks the
//     corrected value first and flags the superseded page with a 대체됨 marker. It
//     does NOT scrub the raw diary, so the old entry can still surface unmarked —
//     curation adds a disambiguation signal, it does not rewrite history.
//
// Lexical path only (no embedder on CI) — so this measures the wiki's LEXICAL
// gain (curated metadata as extra match surface) + the supersede guard. The
// semantic-paraphrase gain needs the GPU host and is a separate measurement.
package chat

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

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
		// NB: the answer token ("모닝레터") must NOT appear in the question — the
		// recall dump echoes the search query (query="..."), so an echoed token
		// could satisfy the hit from metadata rather than recalled evidence.
		// TestRecallGainCorpusInvariant enforces this for the whole corpus.
		{"weekly-paraphrase", "주간보고 자동화 어떤 방식 재사용하기로 했지?", []string{"모닝레터"}, ""},
		// revised fact — must surface the new value, must not present the old as current.
		{"acme-revision", "에이콘 상사 지금 구매 담당자 누구지?", []string{"박수진"}, "이태호"},
	}
	return facts, queries
}

// TestRecallGainCorpusInvariant guards the measurement's integrity: no wantAll
// token may appear in its own question. buildRecallPreflight echoes the search
// query into the dump (query="..."), so a wanted token present in the question
// could satisfy a hit from that echo rather than from recalled evidence —
// silently inflating the measured wiki gain. Every hit must come from a note or
// source, so answer tokens have to be absent from the prompt.
func TestRecallGainCorpusInvariant(t *testing.T) {
	_, queries := recallGainCorpus()
	for _, q := range queries {
		for _, w := range q.wantAll {
			if strings.Contains(q.question, w) {
				t.Errorf("query %q echoes its wanted token %q in the question — a hit could come from the query echo, not recalled evidence", q.name, w)
			}
		}
	}
}

// buildGainStore seeds the corpus for one mode: "both" (wiki+diary), "diaryonly"
// (raw retrieval only — the CL-Bench "stateless of the wiki" baseline), or
// "wikionly" (curated only). Same facts, different layers present.
func buildGainStore(t *testing.T, mode string) *wiki.Store {
	t.Helper()
	dir := t.TempDir()
	wikiDir := filepath.Join(dir, "wiki")
	diaryDir := filepath.Join(dir, "diary")

	facts, _ := recallGainCorpus()
	// Seed the diary FILES with distinct per-entry timestamps BEFORE NewStore,
	// so its rebuildFromDir indexes each as an independent doc. Appending in a
	// loop via Store.AppendDiary would stamp every entry with the same minute
	// (time.Now HH:MM), and the diary index merges same-(file,header) entries
	// into ONE doc — a single blob where a query matching any fact pulls in all
	// the others, so an unrelated fact could satisfy a query's wantAll and skew
	// the wiki-vs-diary gain. Distinct timestamps keep each fact independently
	// retrievable, matching how real diary entries accrue over time.
	if mode != "wikionly" {
		seedDiaryFiles(t, diaryDir, facts)
	}

	store, err := wiki.NewStore(wikiDir, diaryDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

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
	return store
}

// seedDiaryFiles writes each fact's raw diary line(s) as a distinct dated diary
// file with its own "## HH:MM" section, so wiki.NewStore's rebuildFromDir indexes
// them as independent docs (a unique file+header doc ID each — never merged).
// Earlier facts in the corpus get older dates, so a revised-away fact (acme-old,
// before its replacement) is genuinely older than the value that supersedes it.
func seedDiaryFiles(t *testing.T, diaryDir string, facts []gainFact) {
	t.Helper()
	if err := os.MkdirAll(diaryDir, 0o755); err != nil {
		t.Fatalf("diary mkdir: %v", err)
	}
	base := time.Now()
	for i, f := range facts {
		day := base.AddDate(0, 0, -(len(facts) - i)) // earlier corpus index → older
		file := filepath.Join(diaryDir, "diary-"+day.Format("2006-01-02")+".md")
		for j, d := range f.diary {
			ts := day.Add(time.Duration(j) * time.Minute) // distinct header per line
			section := fmt.Sprintf("\n## %s\n\n%s\n", ts.Format("15:04"), d)
			fh, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
			if err != nil {
				t.Fatalf("diary open: %v", err)
			}
			if _, err := fh.WriteString(section); err != nil {
				_ = fh.Close()
				t.Fatalf("diary write: %v", err)
			}
			_ = fh.Close()
		}
	}
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

const supersedeMarker = "대체됨"

// recallRows splits a <recall-context> dump into per-evidence-row blocks: each
// "- source=..." line plus its indented continuation. The leading preamble
// (before the first source row) is its own block and carries no "- source=".
func recallRows(out string) []string {
	var rows []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			rows = append(rows, cur.String())
			cur.Reset()
		}
	}
	for _, ln := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), "- source=") {
			flush()
		}
		cur.WriteString(ln)
		cur.WriteByte('\n')
	}
	flush()
	return rows
}

// unmarkedStaleRows counts evidence rows that present the stale value WITHOUT a
// supersede marker on that SAME row. A global !contains(marker) check is fooled
// when a separate wiki row carries the marker while a diary row still presents
// the stale value unmarked — so the leak must be judged per row.
func unmarkedStaleRows(out, stale, marker string) int {
	n := 0
	for _, row := range recallRows(out) {
		if strings.Contains(row, "- source=") && strings.Contains(row, stale) && !strings.Contains(row, marker) {
			n++
		}
	}
	return n
}

// markerFlagsStale is true if some evidence row carries BOTH the stale value and
// a supersede marker — i.e. the marker actually flags the stale value rather than
// floating on an unrelated row.
func markerFlagsStale(out, stale, marker string) bool {
	for _, row := range recallRows(out) {
		if strings.Contains(row, stale) && strings.Contains(row, marker) {
			return true
		}
	}
	return false
}

// TestRecallStaleBeliefGuard measures what curation does for a revised fact
// (CL-Bench's #1 unsolved failure: discarding stale beliefs). Honest framing,
// per-row: the raw diary keeps the old observation with NO supersede signal, so
// diary-only recall presents the stale value with nothing marking it outdated.
// The wiki ADDS an explicit supersede marker that flags the stale value and
// surfaces the corrected current one — a disambiguation signal diary-only lacks.
//
// What it does NOT claim: that curation scrubs the raw diary. The old diary entry
// can still surface unmarked in BOTH modes; curation adds a signal to the context,
// it does not rewrite history. So the guard asserts the wiki flags the stale value
// and surfaces the new one, and that it never makes the unmarked-row count worse —
// not that the stale mention disappears.
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

	rawOut := recallOnce(buildGainStore(t, "diaryonly"), stale.question)
	wikiOut := recallOnce(buildGainStore(t, "both"), stale.question)

	rawUnmarked := unmarkedStaleRows(rawOut, stale.staleOld, supersedeMarker)
	wikiUnmarked := unmarkedStaleRows(wikiOut, stale.staleOld, supersedeMarker)
	rawSignal := strings.Contains(rawOut, supersedeMarker)
	wikiMarksOld := markerFlagsStale(wikiOut, stale.staleOld, supersedeMarker)
	wikiSurfacesNew := strings.Contains(wikiOut, stale.wantAll[0])

	fmt.Printf("RECALL_STALELEAK diary_unmarked=%d wiki_unmarked=%d diary_signal=%v wiki_marks_old=%v new_surfaced=%v\n",
		rawUnmarked, wikiUnmarked, rawSignal, wikiMarksOld, wikiSurfacesNew)
	t.Logf("stale-old=%q  diary-only output:\n%s", stale.staleOld, rawOut)
	t.Logf("both output:\n%s", wikiOut)

	// Diary-only provably carries no supersede signal — that is its structural gap.
	if rawSignal {
		t.Errorf("diary-only unexpectedly carried a %q signal (corpus drift?)", supersedeMarker)
	}
	// The wiki's value: it flags the stale value and surfaces the corrected one.
	if !wikiMarksOld {
		t.Errorf("wiki path did not flag the stale value %q with a %q marker on its own row", stale.staleOld, supersedeMarker)
	}
	if !wikiSurfacesNew {
		t.Errorf("wiki path failed to surface the revised value %q", stale.wantAll[0])
	}
	// Honest limitation: curation adds a signal, it does not scrub the raw diary,
	// so an unmarked stale row may persist — but the wiki must never make it worse.
	if wikiUnmarked > rawUnmarked {
		t.Errorf("wiki path increased unmarked stale rows (%d > %d): curation regressed the raw baseline", wikiUnmarked, rawUnmarked)
	}
}

// rowScoreContaining returns the score= of the first evidence row whose content
// contains val (0 if none). Lets a test compare the recall ranking of two values
// that live in different sources.
func rowScoreContaining(out, val string) float64 {
	for _, row := range recallRows(out) {
		if !strings.Contains(row, "- source=") || !strings.Contains(row, val) {
			continue
		}
		i := strings.Index(row, "score=")
		if i < 0 {
			return 0
		}
		field := strings.TrimSpace(strings.SplitN(row[i+len("score="):], " ", 2)[0])
		v, _ := strconv.ParseFloat(field, 64)
		return v
	}
	return 0
}

// TestRecallSynthesisDrift measures CL-Bench's central curation hazard —
// "spurious generalizations" — head on. The gain corpus only seeds wiki==diary
// AGREEMENT, so it cannot see the failure that matters most: when the dreamer
// mis-synthesizes a fact, the curated wiki value DRIFTS from the faithful raw
// diary, and the wiki's higher prior can rank the WRONG value above the right one.
//
// One fact, two conflicting values: the diary holds the correct figure (as first
// observed), the wiki holds a drifted one (the dreamer fat-fingered the same
// deal). A query asks the figure; we measure which value recall ranks higher.
//
// Informational, not a hard gate on which wins — it reports the ranking so a
// drift that overrides raw ground truth is visible and can inform the wiki↔diary
// prior. The ONE hard failure is the drift completely suppressing the correct
// value: then the user can never recover ground truth from recall at all.
func TestRecallSynthesisDrift(t *testing.T) {
	const (
		correct = "1,950" // faithful raw diary value (as first observed)
		drift   = "2,950" // dreamer mis-summarized the SAME deal into the wiki
		query   = "에코프로 케이블 견적 미터 수 얼마였지?"
	)
	dir := t.TempDir()
	diaryDir := filepath.Join(dir, "diary")
	if err := os.MkdirAll(diaryDir, 0o755); err != nil {
		t.Fatalf("diary mkdir: %v", err)
	}
	day := time.Now().AddDate(0, 0, -1)
	section := fmt.Sprintf("\n## %s\n\n%s\n", day.Format("15:04"), "에코프로 케이블 "+correct+"m 견적 회신함.")
	if err := os.WriteFile(filepath.Join(diaryDir, "diary-"+day.Format("2006-01-02")+".md"), []byte(section), 0o644); err != nil {
		t.Fatalf("write diary: %v", err)
	}

	store, err := wiki.NewStore(filepath.Join(dir, "wiki"), diaryDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	// The curated wiki carries the DRIFTED figure — title/summary/tags give it
	// more match surface than the one raw diary line, plus an importance prior.
	if err := store.WritePage("거래/ecopro-cable.md", &wiki.Page{
		Meta: wiki.Frontmatter{ID: "ecopro", Title: "에코프로 케이블 견적", Category: "거래",
			Summary: "에코프로 케이블 " + drift + "m 견적 회신", Tags: []string{"에코프로", "케이블", "견적"}, Importance: 0.8},
		Body: "에코프로 케이블 " + drift + "m 견적 회신.",
	}); err != nil {
		t.Fatalf("WritePage: %v", err)
	}

	out := recallOnce(store, query)
	driftScore := rowScoreContaining(out, drift)
	correctScore := rowScoreContaining(out, correct)
	driftPresent := strings.Contains(out, drift)
	correctPresent := strings.Contains(out, correct)
	driftOnTop := driftPresent && (!correctPresent || driftScore > correctScore)

	top := "correct"
	if driftOnTop {
		top = "drift"
	}
	fmt.Printf("RECALL_DRIFT drift_score=%.2f correct_score=%.2f drift_present=%v correct_present=%v top=%s\n",
		driftScore, correctScore, driftPresent, correctPresent, top)
	t.Logf("query=%q  output:\n%s", query, out)

	// Hard failure: the drifted wiki value entirely suppressed the correct raw one
	// — ground truth is then unrecoverable from recall.
	if !correctPresent {
		t.Errorf("the correct raw diary value %q was absent from recall — the drifted wiki value suppressed ground truth", correct)
	}
	// Honest observation (logged, not gated): the curated drift outranking the raw
	// truth is CL-Bench's spurious-generalization failure made concrete — evidence
	// to weigh when tuning the wiki↔diary prior (the deferred rebalance question).
	if driftOnTop {
		t.Logf("SPURIOUS-GENERALIZATION OBSERVED: curated wiki drift %q (score %.2f) outranks faithful raw diary %q (score %.2f). Both surface, so the conflict is visible — but recall presents the wrong figure first. Evidence for capping the wiki>diary prior.",
			drift, driftScore, correct, correctScore)
	}
}

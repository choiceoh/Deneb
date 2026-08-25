// wikirepair applies the corpus repairs identified by the 2026-08-25 full-read
// audit — every wiki page read sentence-by-sentence (1,186 pages / 3.77MB).
//
// It fixes what the audit found in the STORED corpus. The causes are fixed
// separately in the gateway (AnalysisUsable, relatedDomainCompatible,
// sameThemeSignal); without those, these repairs would decay again.
//
// Passes, in dependency order:
//
//	jamo        — canonical-spelling sweep for syllable-corrupted proper nouns
//	              (핵봄→해봄 …). Recall-critical: a corrupted name is unfindable.
//	related     — strip off-domain related links (personal/system pages that
//	              embedding proximity attached to project mail).
//	numbers     — literal factual corrections (a 10x total, an Excel serial
//	              left as a date, four two-year-off ledger dates).
//	unusable    — delete pages whose body is narration/error rather than
//	              analysis, so the mail re-analyzes cleanly under the new gate.
//	themes      — drop the unearned history from repeat-signal rows whose key
//	              was reused for an unrelated signal.
//	stubedges   — cut related[] edges to and from pages with no prose, which
//	              matched each other on their shared empty template.
//
// Deliberately NOT here: relocating non-business mail out of 프로젝트/. A
// 메일분석 page's path is derived from its message ID, so a later re-analysis
// recreates the original path and the move leaves a duplicate — which is why
// IsLayoutManagedPath already refuses topic-driven moves of these slots
// (measured 2026-08-23: 21 such auto-moves, none of them correct). Newsletters
// sitting in the unlinked bucket are staging, and the fix for those belongs at
// intake, not in the corpus.
//
// Dry-run by default. --apply writes.
//
// ⚠️ Stop the gateway before --apply: Store locking is in-process only, and a
// live gateway holds in-memory search/index state. Restart afterwards.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailanalysis"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "wikirepair:", err)
		os.Exit(1)
	}
}

func run() error {
	home, _ := os.UserHomeDir()
	wikiDir := flag.String("wiki-dir", filepath.Join(home, ".deneb", "wiki"), "wiki root")
	diaryDir := flag.String("diary-dir", filepath.Join(home, ".deneb", "memory", "diary"), "diary root")
	apply := flag.Bool("apply", false, "execute (default: dry-run)")
	only := flag.String("only", "", "run a single pass (jamo|related|numbers|unusable)")
	flag.Parse()

	store, err := wiki.NewStore(*wikiDir, *diaryDir)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer store.Close()

	rep := &report{}
	if *apply {
		if h := store.SnapshotGit(context.Background(), "wikirepair: pre full-read audit repairs"); h != "" {
			fmt.Println("snapshot", h)
		}
	}

	passes := []struct {
		name string
		fn   func(*wiki.Store, bool, *report) error
	}{
		{"jamo", repairJamo},
		{"related", repairRelated},
		{"numbers", repairNumbers},
		{"unusable", deleteUnusableAnalyses},
		{"themes", repairThemeHistory},
		{"stubedges", stripStubRelatedEdges},
	}
	for _, p := range passes {
		if *only != "" && *only != p.name {
			continue
		}
		rep.section(p.name)
		if err := p.fn(store, *apply, rep); err != nil {
			return fmt.Errorf("%s: %w", p.name, err)
		}
	}

	rep.print()
	if !*apply {
		fmt.Println("\n(dry-run — nothing written; stop the gateway and re-run with --apply)")
	}
	return nil
}

// --- report -----------------------------------------------------------------

type report struct {
	lines []string
	errs  []string
	count map[string]int
	cur   string
}

func (r *report) section(name string) {
	if r.count == nil {
		r.count = map[string]int{}
	}
	r.cur = name
	r.lines = append(r.lines, "\n== "+name)
}

func (r *report) add(format string, args ...any) {
	r.lines = append(r.lines, "   "+fmt.Sprintf(format, args...))
	r.count[r.cur]++
}

func (r *report) fail(format string, args ...any) {
	r.errs = append(r.errs, fmt.Sprintf(format, args...))
}

func (r *report) print() {
	for _, l := range r.lines {
		fmt.Println(l)
	}
	fmt.Println("\n== totals")
	keys := make([]string, 0, len(r.count))
	for k := range r.count {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("   %-10s %d\n", k, r.count[k])
	}
	for _, e := range r.errs {
		fmt.Fprintln(os.Stderr, "ERROR", e)
	}
}

// --- pass: jamo -------------------------------------------------------------

// jamoRepairs maps a syllable-corrupted form to its canonical spelling. Every
// entry was observed in the corpus; the corruption is a generation-side defect
// from 2026-06~07 that mangles a single syllable inside a proper noun
// (해봄에너지 → 핵봄에너지). Recall matches on the name, so a corrupted page is
// unreachable by the name it is about — which is why this runs first.
//
// Ordering matters: longer keys first so 핵말고흥 is repaired before a shorter
// key could bite into it.
var jamoRepairs = []struct{ bad, good string }{
	{"핵말고흥", "해밀고흥"},
	{"설치조걘부", "설치조건부"},
	{"정적화엽시험", "정적연소시험"},
	{"남도에코에코", "남도에코"},
	{"요청핵둔", "요청해둔"},
	{"이경개시졌", "개시됐"},
	{"손핵배상", "손해배상"},
	{"손핼배상", "손해배상"},
	{"보핵매실", "보필매실"},
	{"직묵대행", "직무대행"},
	{"업묵보고", "업무보고"},
	{"나엘되지", "나열되지"},
	{"해상풉력", "해상풍력"},
	{"소규modal", "소규모"},
	{"탑솔라(쭈)", "탑솔라(주)"},
	{"핫남군청", "하남군청"},
	{"의신탕력", "의신풍력"},
	{"오리지인", "오리진"},
	{"주첳별", "주차별"},
	{"핵바람", "해바람"},
	{"뉴그렌", "뉴글렌"},
	{"파트ner", "파트너"},
	{"미엔랑", "미얀마"},
	{"통볐다", "통했다"},
	{"측멸도", "측정도"},
	{"지철된", "지체된"},
	{"휴드폰", "휴대폰"},
	{"남려와", "남겨와"},
	{"근종할", "추종할"},
	{"미봉채", "미봉책"},
	{"태양꿕", "태양광"},
	{"조걸부", "조건부"},
	{"핵봄", "해봄"},
	{"걸물", "건물"},
	{"낶부", "내부"},
	{"핵밀", "해밀"},
	{"법묵", "법무"},
	{"법묲", "법무"},
	{"유옄", "유예"},
	{"즉닉", "즉시"},
	{"면살", "명분"},
}

// derivedPages are regenerated from the pages they summarize, so repairing them
// in place would be undone on the next rebuild — and repairing the sources is
// what actually fixes them.
var derivedPages = map[string]bool{"index.md": true, "log.md": true}

func repairJamo(store *wiki.Store, apply bool, rep *report) error {
	paths, err := store.ListPages("")
	if err != nil {
		return err
	}
	for _, rp := range paths {
		if derivedPages[filepath.ToSlash(rp)] {
			continue
		}
		page, err := store.ReadPage(rp)
		if err != nil || page == nil {
			continue
		}
		hits := jamoHits(page.Body) + jamoHits(page.Meta.Title) + jamoHits(page.Meta.Summary)
		if hits == 0 {
			continue
		}
		if !apply {
			rep.add("would repair %d occurrence(s) in %s", hits, rp)
			continue
		}
		if err := store.UpdatePage(rp, func(cur *wiki.Page) (*wiki.Page, error) {
			if cur == nil {
				return nil, nil
			}
			cur.Body = applyJamo(cur.Body)
			cur.Meta.Title = applyJamo(cur.Meta.Title)
			cur.Meta.Summary = applyJamo(cur.Meta.Summary)
			return cur, nil
		}); err != nil {
			rep.fail("jamo %s: %v", rp, err)
			continue
		}
		rep.add("repaired %d occurrence(s) in %s", hits, rp)
	}
	return nil
}

func jamoHits(s string) int {
	n := 0
	for _, r := range jamoRepairs {
		n += strings.Count(s, r.bad)
	}
	return n
}

func applyJamo(s string) string {
	for _, r := range jamoRepairs {
		s = strings.ReplaceAll(s, r.bad, r.good)
	}
	return s
}

// --- pass: related ----------------------------------------------------------

// offDomainPrefixes are page namespaces that carry no business relationship to
// a project page. Embedding proximity linked 기타/차량-핫스팟 and 기타/집-와이파이
// into EPC contract mail, and 시스템/톤-규칙 into a 가배치 mail — short pages
// crowd together in vector space regardless of subject. Those edges become
// graph edges the ranker trusts, so a contract query surfaces the operator's
// home wifi page.
var offDomainPrefixes = []string{"시스템/", "기타/"}

func repairRelated(store *wiki.Store, apply bool, rep *report) error {
	paths, err := store.ListPages("프로젝트")
	if err != nil {
		return err
	}
	for _, rp := range paths {
		page, err := store.ReadPage(rp)
		if err != nil || page == nil || len(page.Meta.Related) == 0 {
			continue
		}
		kept, dropped := partitionRelated(page.Meta.Related)
		if len(dropped) == 0 {
			continue
		}
		if !apply {
			rep.add("would drop %v from %s", dropped, rp)
			continue
		}
		if err := store.UpdatePageMetaOnly(rp, func(cur *wiki.Page) (*wiki.Page, error) {
			if cur == nil {
				return nil, nil
			}
			cur.Meta.Related = kept
			return cur, nil
		}); err != nil {
			rep.fail("related %s: %v", rp, err)
			continue
		}
		rep.add("dropped %v from %s", dropped, rp)
	}
	return nil
}

func partitionRelated(related []string) (kept, dropped []string) {
	for _, r := range related {
		t := strings.TrimSpace(r)
		off := false
		for _, p := range offDomainPrefixes {
			if strings.HasPrefix(t, p) {
				off = true
				break
			}
		}
		if off {
			dropped = append(dropped, t)
			continue
		}
		kept = append(kept, r)
	}
	return kept, dropped
}

// --- pass: numbers ----------------------------------------------------------

// numberFixes are factual corrections, each verified against the source mail
// during the full-read audit. Unlike the jamo sweep these are not a pattern:
// every entry is a single page whose stated figure contradicts its own
// evidence, so they are listed literally and applied only on an exact match —
// a miss reports rather than guesses.
var numberFixes = []struct {
	path, old, new, why string
}{{
	path: "프로젝트/거래/ja-solar.md",
	old:  "130MW × 167원/Wp = **약 2,171억원**",
	new:  "130MW × 167원/Wp = **약 217.1억원**",
	why:  "130,000,000W × 167원 = 217.1억. The page's own unit price and volume give a tenth of the stated total.",
}, {
	path: "프로젝트/거래/기아오토랜드-광주공장.md",
	old:  "- 46254.543659143521 · 견적서",
	new:  "- 2026-08-20 · 견적서",
	why:  "Excel serial 46254 = 2026-08-20, matching the cited mail 1fc028bb2 (sent 2026-08-20).",
}, {
	path: "프로젝트/거래/ztt.md",
	old:  "- 2024-06-23 · 기타 · $1,078,476.23",
	new:  "- 2026-06-23 · 기타 · $1,078,476.23",
	why:  "Alan Zhang's payment reminder is 2026-06-23; the ledger is two years off.",
}, {
	path: "프로젝트/거래/ztt.md",
	old:  "- 2025-06-25 · 기타 · $401,540.58",
	new:  "- 2026-06-25 · 기타 · $401,540.58",
	why:  "Invoice 7 due date is 2026-06-25.",
}, {
	path: "프로젝트/거래/ztt.md",
	old:  "- 기한 해제: 2025-06-25 (424일 경과",
	new:  "- 기한 해제: 2026-06-25 (61일 경과",
	why:  "Derived from the corrected due date — the 424-day figure was produced by the wrong year.",
}, {
	path: "프로젝트/거래/xpanner.md",
	old:  "- 2024-05-29 · 기타",
	new:  "- 2026-05-29 · 기타",
	why:  "The cited mail 176955990 is 2026-05-29.",
}, {
	path: "프로젝트/거래/헥사리뉴어블코리아-hexa-renewables.md",
	old:  "- 2024-06-24 · 계약서",
	new:  "- 2026-06-24 · 계약서",
	why:  "The cited 강진 90MW EPC draft mail 157687b48 is 2026-06-24.",
}}

func repairNumbers(store *wiki.Store, apply bool, rep *report) error {
	for _, f := range numberFixes {
		page, err := store.ReadPage(f.path)
		if err != nil || page == nil {
			rep.fail("numbers: %s unreadable", f.path)
			continue
		}
		if !strings.Contains(page.Body, f.old) {
			if strings.Contains(page.Body, f.new) {
				rep.add("already fixed %s (%q)", f.path, f.new)
				continue
			}
			rep.fail("numbers: %s no longer contains %q — verify by hand", f.path, f.old)
			continue
		}
		if !apply {
			rep.add("would fix %s: %q → %q", f.path, f.old, f.new)
			continue
		}
		if err := store.UpdatePage(f.path, func(cur *wiki.Page) (*wiki.Page, error) {
			if cur == nil {
				return nil, nil
			}
			cur.Body = strings.ReplaceAll(cur.Body, f.old, f.new)
			return cur, nil
		}); err != nil {
			rep.fail("numbers %s: %v", f.path, err)
			continue
		}
		rep.add("fixed %s: %q → %q", f.path, f.old, f.new)
	}
	return nil
}

// --- pass: unusable ---------------------------------------------------------

// deleteUnusableAnalyses removes 메일분석 pages whose body is not an analysis —
// process narration, a bare heading, or a delivery-layer error string. The
// judgement is mailanalysis.AnalysisUsable, the same gate that now runs at
// write time, so the corpus and the intake path cannot disagree about what
// counts as a usable analysis.
//
// Deleting is the repair: the mail stays in the archive, and the page's absence
// is what lets a later pass re-analyze it. Leaving a placeholder would instead
// keep asserting that this mail was analyzed.
func deleteUnusableAnalyses(store *wiki.Store, apply bool, rep *report) error {
	paths, err := store.ListPages("")
	if err != nil {
		return err
	}
	for _, rp := range paths {
		slash := filepath.ToSlash(rp)
		if !strings.Contains(slash, "/메일분석/") {
			continue
		}
		page, err := store.ReadPage(rp)
		if err != nil || page == nil {
			continue
		}
		reason := mailanalysis.AnalysisUsable(analysisBodyOf(page.Body))
		if reason == nil {
			continue
		}
		if !apply {
			rep.add("would delete %s (%v)", rp, reason)
			continue
		}
		if err := store.DeletePage(rp); err != nil {
			rep.fail("delete %s: %v", rp, err)
			continue
		}
		rep.add("deleted %s (%v)", rp, reason)
	}
	return nil
}

// analysisBodyOf drops the leading provenance blockquote (From/Date/Message ID)
// that buildMailAnalysisPage prepends, leaving what the model actually wrote.
func analysisBodyOf(body string) string {
	var out []string
	started := false
	for _, ln := range strings.Split(body, "\n") {
		if !started && strings.HasPrefix(ln, ">") {
			continue
		}
		if !started && strings.TrimSpace(ln) == "" {
			continue
		}
		started = true
		out = append(out, ln)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// --- pass: themes -----------------------------------------------------------

// Repeat-signal rows whose key was reused for an unrelated signal. Until
// sameThemeSignal shipped, mergeThemeRows overwrote a row's Signal in place and
// kept its First date and Count, so the new signal inherited a recurrence it
// never had: "srv2 vLLM 엔드포인트 설정" had been seen 4 times since 07-25, and a
// first-ever observation about a 현대차 EPC 텀시트 landed on that row reading as a
// 정착 pattern.
//
// Each of these was read against its key and its 최근 근거 by hand — a Korean
// signal and an English slug share no tokens, so nothing here can be derived.
// The lost signal text is not recoverable, so the repair is to stop the row
// asserting a history that is not its own: First becomes Last, Count becomes 1,
// stage returns to 관찰. The key is left alone. It is only a slug, and once the
// dreamer coins a correct key for this content the stale row simply goes dormant.
var themeKeyReuse = []struct {
	key    string
	signal string // prefix of the current (unrelated) signal, to confirm the row
}{
	{"srv2-vllm-endpoint-setup", "현대차 출고센터 EPC 텀시트"},
	{"solar-monitoring-dashboard-widget-development", "SKN 이천 모듈 바이패스"},
	{"gaon-electronics-payment-delay", "결제 실패로 인한 인프라 구독"},
	{"ja-solar-financial-review", "중견 기업 관련 사용자 문의"},
	{"goheung-permit-renewal-risk", "옹진 해상풍력"},
	{"sindasan-equity-injunction", "빛가람이엔씨-제이에스파워"},
}

const themeLedgerPath = "업무/반복신호.md"

func repairThemeHistory(store *wiki.Store, apply bool, rep *report) error {
	// Unlike the corpus-wide passes, this one has exactly one target. A skip
	// here would let --apply report success having repaired nothing, so an
	// unreadable ledger stops the run instead.
	page, err := store.ReadPage(themeLedgerPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", themeLedgerPath, err)
	}
	if page == nil {
		return fmt.Errorf("%s not found", themeLedgerPath)
	}
	body, changed := rewriteThemeRows(page.Body, rep, apply)
	if !apply || changed == 0 {
		return nil
	}
	if err := store.UpdatePage(themeLedgerPath, func(cur *wiki.Page) (*wiki.Page, error) {
		if cur == nil {
			return nil, nil
		}
		cur.Body = body
		return cur, nil
	}); err != nil {
		rep.fail("themes: %v", err)
	}
	return nil
}

// rewriteThemeRows resets the inherited history on each listed row. Columns are
// 신호키|신호|최초|최근|근거수|단계|상태|최근 근거 between leading and trailing pipes.
func rewriteThemeRows(body string, rep *report, apply bool) (string, int) {
	lines := strings.Split(body, "\n")
	changed := 0
	for _, want := range themeKeyReuse {
		found := false
		for i, ln := range lines {
			cols := strings.Split(ln, "|")
			if len(cols) < 10 || strings.TrimSpace(cols[1]) != want.key {
				continue
			}
			found = true
			if !strings.HasPrefix(strings.TrimSpace(cols[2]), want.signal) {
				rep.fail("themes: %s no longer carries %q — verify by hand", want.key, want.signal)
				break
			}
			first, last := strings.TrimSpace(cols[3]), strings.TrimSpace(cols[4])
			count, stage := strings.TrimSpace(cols[5]), strings.TrimSpace(cols[6])
			if count == "1" && first == last {
				rep.add("already reset %s", want.key)
				break
			}
			rep.add("reset %s: 최초 %s→%s · 근거수 %s→1 · 단계 %s→관찰", want.key, first, last, count, stage)
			changed++
			if !apply {
				break
			}
			cols[3] = " " + last + " "
			cols[5] = " 1 "
			cols[6] = " 관찰 "
			lines[i] = strings.Join(cols, "|")
			break
		}
		if !found {
			rep.fail("themes: row %s not found", want.key)
		}
	}
	return strings.Join(lines, "\n"), changed
}

// --- pass: stubedges --------------------------------------------------------

// stripStubRelatedEdges removes related[] edges that involve a page carrying no
// prose. Those pages are byte-identical scaffolding apart from a place name, so
// they are each other's nearest neighbours and the cosine floor cannot separate
// them — 광명시 was linked to 광주, 울주군 to 완도군, across unrelated projects.
//
// Both directions go: a prose-less page's own related[] is cleared (it has
// nothing to be related about), and every other page drops the entries pointing
// at one. wiki.HasOwnProse is the same predicate suggestRelated now applies at
// write time, so the corpus and the enricher cannot disagree about what counts
// as an empty page.
func stripStubRelatedEdges(store *wiki.Store, apply bool, rep *report) error {
	paths, err := store.ListPages("")
	if err != nil {
		return err
	}
	stub := make(map[string]bool, len(paths))
	for _, p := range paths {
		page, err := store.ReadPage(p)
		if err != nil || page == nil {
			continue
		}
		if !wiki.HasOwnProse(page.Body) {
			stub[strings.TrimSuffix(p, ".md")] = true
		}
	}
	rep.add("prose 없는 페이지 %d장 / 전체 %d장", len(stub), len(paths))

	for _, p := range paths {
		page, err := store.ReadPage(p)
		if err != nil || page == nil {
			continue
		}
		selfStub := stub[strings.TrimSuffix(p, ".md")]
		kept := make([]string, 0, len(page.Meta.Related))
		var dropped []string
		for _, r := range page.Meta.Related {
			t := strings.TrimSuffix(strings.TrimSpace(r), ".md")
			if selfStub || stub[t] {
				dropped = append(dropped, t)
				continue
			}
			kept = append(kept, r)
		}
		if len(dropped) == 0 {
			continue
		}
		if !apply {
			rep.add("would cut %d edge(s) from %s → %s", len(dropped), p, strings.Join(dropped, ", "))
			continue
		}
		if err := store.UpdatePageMetaOnly(p, func(cur *wiki.Page) (*wiki.Page, error) {
			if cur == nil {
				return nil, nil
			}
			cur.Meta.Related = kept
			return cur, nil
		}); err != nil {
			rep.fail("stubedges %s: %v", p, err)
			continue
		}
		rep.add("cut %d edge(s) from %s", len(dropped), p)
	}
	return nil
}

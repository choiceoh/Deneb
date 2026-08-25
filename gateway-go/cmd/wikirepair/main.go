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
//	spammail    — delete analyses whose own opening line classifies the mail as
//	              an ad / newsletter / marketing blast (operator-directed).
//	foldsites   — fold 현장 pages back into their project page and delete them,
//	              now that the 현장 지도 they fed is gone.
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
		{"spammail", deleteSelfDeclaredSpamMail},
		{"foldsites", foldSitePagesIntoProject},
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

// note prints a line WITHOUT counting it. Context lines — a pass summarising how
// many pages it scanned, or breaking its own result down by sender — are not
// actions, and folding them into the total made this tool misreport its own work
// (spammail read 83 when it would delete 59, the difference being 24 per-sender
// lines). A repair tool that cannot count its own repairs is worse than none.
func (r *report) note(format string, args ...any) {
	r.lines = append(r.lines, "   "+fmt.Sprintf(format, args...))
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
// jamoUnresolved holds corruptions this tool reports and never rewrites.
//
// It is empty right now, and the mechanism stays because of what filled it. The
// jamo corruptions are NOT transcription errors over some source document —
// every page carrying one is written by the pipeline itself (the analysis body
// below the provenance blockquote, the dream-maintained 로그.md). The corruption
// happens at GENERATION. There is no original to restore.
//
// That splits the table into three kinds of entry, and only the first two can be
// settled by anything but a person:
//
//	names       — no original either, but the REFERENT is real. 해봄, 해밀, 해남군청,
//	              보해매실. Someone who knows the business can rule, it drives
//	              recall, and getting it wrong writes a false name into a ledger.
//	vocabulary  — context fixes it. 걸물측 can only be 건물측, 14일 유옄 only 유예,
//	              whatever the model meant to type.
//	neither     — the model's own commentary, where context does not narrow it.
//	              근종할 sat here: "진행 상황을근종할 필요가 있습니다" reads as 추종/추적/
//	              주시 equally. Not undecidable — there was nothing to decide. The
//	              operator ruled 주시할 on MEANING, which is a call about their own
//	              wiki, not a recovery of a lost word.
//
// So an entry lands here when it is that third kind: report it, leave the text
// alone, and let a person decide whether the sentence is worth rewriting at all.
var jamoUnresolved = []struct{ bad, note string }{}

// name marks a proper noun — a company, place, or project. Only those get the
// curated-attestation audit: a wrong NAME is unverifiable from context and gets
// written into ledgers, while a wrong ordinary word (휴대폰, 직무대행) is obvious
// on sight and never appears in curated metadata anyway. Encoding the
// distinction here is what keeps the audit's findings worth reading.
var jamoRepairs = []struct {
	bad, good string
	name      bool
	// confirmed records that a person signed off on this target. The audit skips
	// those — not because it trusts the table, but because a finding that keeps
	// reappearing after it has been answered is how real findings get ignored.
	// Set it only for a target an operator actually stated.
	confirmed bool
}{
	{"핵말고흥", "해밀고흥", true, false},
	{"설치조걘부", "설치조건부", false, false},
	{"정적화엽시험", "정적연소시험", false, false},
	{"남도에코에코", "남도에코", true, false},
	{"요청핵둔", "요청해둔", false, false},
	{"이경개시졌", "개시됐", false, false},
	{"손핵배상", "손해배상", false, false},
	{"손핼배상", "손해배상", false, false},
	// 운영자 확정 2026-08-25 — "곧 진행 상황을근종할 필요가 있습니다"에서 주시할.
	// 띄어쓰기까지 삼켜져 있어 함께 복원한다.
	{"상황을근종할", "상황을 주시할", false, true},
	// 운영자 확정 2026-08-25. 코퍼스만으로는 3표기(보해/보필/보핵) 중 어느 것도
	// 정할 수 없었다 — 대표.md cues는 보해, 로그.md 대표이사 기록과 합의서 PDF
	// 파일명은 보필이었다. 지상진실이 코퍼스 밖에 있는 경우다.
	{"보핵매실", "보해매실", true, true},
	{"보필매실", "보해매실", true, true},
	// 운영자 확정 2026-08-25. 구조물 도면이라 측면도. 측정도로도 읽혀서 문맥이
	// 못 정했다.
	{"측멸도", "측면도", false, true},
	// 운영자 확정 2026-08-25. "SK가 직접 내려와 이야기하라" — 현장으로 내려오라는
	// 뜻. 남겨와/나와서 둘 다 틀린 추측이었다.
	{"남려와", "내려와", false, true},
	{"직묵대행", "직무대행", false, false},
	{"업묵보고", "업무보고", false, false},
	{"나엘되지", "나열되지", false, false},
	{"해상풉력", "해상풍력", false, false},
	{"소규modal", "소규모", false, false},
	{"탑솔라(쭈)", "탑솔라(주)", true, false},
	{"핫남군청", "해남군청", true, true},
	{"의신탕력", "의신풍력", true, false},
	{"오리지인", "오리진", true, false},
	{"주첳별", "주차별", false, false},
	{"핵바람", "해바람", true, false},
	{"뉴그렌", "뉴글렌", true, false},
	{"파트ner", "파트너", false, false},
	{"미엔랑", "미열람", false, false},
	{"통볐다", "통했다", false, false},
	{"지철된", "지체된", false, false},
	{"휴드폰", "휴대폰", false, false},
	{"미봉채", "미봉책", false, false},
	{"태양꿕", "태양광", false, false},
	{"조걸부", "조건부", false, false},
	{"핵봄", "해봄", true, false},
	{"걸물", "건물", false, false},
	{"낶부", "내부", false, false},
	{"핵밀", "해밀", true, false},
	{"법묵", "법무", false, false},
	{"법묲", "법무", false, false},
	{"유옄", "유예", false, false},
	{"즉닉", "즉시", false, false},
	{"면살", "명분", false, false},
}

// derivedPages are regenerated from the pages they summarize, so repairing them
// in place would be undone on the next rebuild — and repairing the sources is
// what actually fixes them.
var derivedPages = map[string]bool{"index.md": true, "log.md": true}

// auditJamoTable checks each substitution's TARGET against the corpus before any
// of them run, and reports the ones a human still has to look at.
//
// This exists because of a bug this tool shipped with: {"보핵매실", "보필매실"} — a
// mapping from one corruption to ANOTHER, which would have written a wrong
// counterparty name into a financial ledger. The check that missed it was
// frequency: 보필매실 occurred 5 times, so it looked attested. It was not
// canonical, it was just the more common typo.
//
// What separates them is WHERE the form lives. The real name is in curated
// metadata — 대표.md's body, and its cues/title/summary, which a person wrote and
// maintains. The typo lived only in 로그.md body text, which is appended
// mechanically. So attestation means "appears somewhere curated", not "appears".
//
// Three outcomes, and only the middle one is a finding:
//
//	curated            — the target is a name the wiki actually uses. Silent.
//	body-only          — ★ the 보필매실 shape: common enough to look right, never
//	                     used anywhere a person curated.
//	absent entirely    — nothing in the corpus vouches for this name at all.
//
// Both non-curated outcomes are findings, and the second one is why. This audit
// first ran with "absent" as a quiet note — a one-off place name seemed benign —
// and it printed 핫남군청 → 하남군청. The operator read that line and corrected it:
// the corpus has 해남 404 times and 하남 zero, the project code is pl1-hnm-epc-001,
// and the page is a 영암 ledger, where a Gyeonggi city cannot appear. 하남 and 해남
// are BOTH real Korean place names, which is exactly why nothing but attestation
// could separate them — and exactly why "absent" must not be filed as expected.
// A name the corpus cannot vouch for is the unverifiable case, not the harmless
// one. The tool surfaces it; a person decides.
func auditJamoTable(store *wiki.Store, paths []string, rep *report) {
	var curated, body strings.Builder
	for _, rp := range paths {
		page, err := store.ReadPage(rp)
		if err != nil || page == nil {
			continue
		}
		curated.WriteString(page.Meta.Title + "\n" + page.Meta.Summary + "\n")
		curated.WriteString(strings.Join(page.Meta.Cues, "\n") + "\n")
		curated.WriteString(strings.Join(page.Meta.Tags, "\n") + "\n")
		// A 대표 page IS the curated statement of what a thing is called.
		if strings.HasSuffix(filepath.ToSlash(rp), "/대표.md") {
			curated.WriteString(page.Body + "\n")
			continue
		}
		body.WriteString(page.Body + "\n")
	}
	curatedText, bodyText := curated.String(), body.String()
	for _, r := range jamoRepairs {
		if !r.name || r.confirmed {
			continue
		}
		if !strings.Contains(curatedText, r.bad) && !strings.Contains(bodyText, r.bad) {
			continue // rule does not fire on this corpus; nothing to vouch for
		}
		if strings.Contains(curatedText, r.good) {
			continue
		}
		if strings.Contains(bodyText, r.good) {
			rep.fail("jamo: %q → %q — 대상이 본문에만 있고 큐레이션된 곳엔 없음. 오타→오타일 수 있으니 대표/cues로 정본 확인", r.bad, r.good)
			continue
		}
		rep.fail("jamo: %q → %q — 코퍼스 어디에도 이 이름이 없음. 문맥으로 검증 불가하니 사람이 확인할 것", r.bad, r.good)
	}
}

func repairJamo(store *wiki.Store, apply bool, rep *report) error {
	paths, err := store.ListPages("")
	if err != nil {
		return err
	}
	for _, u := range jamoUnresolved {
		rep.note("미결(적용 안 함) %s — %s", u.bad, u.note)
	}
	auditJamoTable(store, paths, rep)
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
				rep.note("already fixed %s (%q)", f.path, f.new)
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
// Deleting is the repair when the mail can be analyzed again: the source stays
// in the archive, and the page's absence is what lets a later pass redo it. A
// placeholder would instead keep asserting that this mail WAS analyzed.
//
// Two of the seventeen cannot be redone. They predate the archive and carry no
// `resource: mail:<id>` pointer, so nothing can fetch the source again — the
// page's title (the mail's subject line) is the only surviving trace that the
// mail existed. Deleting those would be a permanent loss to remove a body that
// is merely worthless, so they keep the page and lose only the body: it is
// replaced with a line saying the analysis failed and the source is gone. That
// tells the truth in both directions — the mail happened, the analysis did not.
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
		// The pipeline re-fetches by `resource: mail:<id>`; without it the page
		// is the last trace of the mail and must not be deleted.
		if strings.TrimSpace(page.Meta.Resource) == "" {
			if !apply {
				rep.add("would blank (source unrecoverable, no resource) %s (%v)", rp, reason)
				continue
			}
			if err := store.UpdatePage(rp, func(cur *wiki.Page) (*wiki.Page, error) {
				if cur == nil {
					return nil, nil
				}
				cur.Body = unrecoverableBody(cur.Body)
				return cur, nil
			}); err != nil {
				rep.fail("blank %s: %v", rp, err)
				continue
			}
			rep.add("blanked %s (%v)", rp, reason)
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

// unrecoverableBody keeps the provenance blockquote (who sent it, when) and
// replaces the failed analysis with a statement of what happened.
func unrecoverableBody(body string) string {
	var quote []string
	for _, ln := range strings.Split(body, "\n") {
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(t, ">") || strings.HasPrefix(t, "#") || t == "" {
			quote = append(quote, ln)
			continue
		}
		break
	}
	head := strings.TrimRight(strings.Join(quote, "\n"), "\n")
	return head + "\n\n분석 실패 — 저장된 본문이 분석이 아니라 오류 문구였고, 원본 메일이 아카이브에 없어 재분석할 수 없다. 이 페이지는 메일이 존재했다는 기록으로만 남는다. (wikirepair, 2026-08-25)\n"
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
				rep.note("already reset %s", want.key)
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
	rep.note("prose 없는 페이지 %d장 / 전체 %d장", len(stub), len(paths))

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

// --- 철회: noisemail -------------------------------------------------------
//
// A pass here deleted 메일분석 pages whose SENDER matched mailpriority.IsBulkNoise
// and that sat in a project folder. It ran once, on 2026-08-25, and deleted 25
// pages. 23 of them were real business meeting records — 주간회의, 계약 협의, PF
// 인출, 인허가, 자재 조달 — and they were restored from the pre-apply snapshot.
//
// The premise was false. "Machine sender" is a proxy for "machine SENT", not for
// "worthless": no-reply@plaud.ai is a recorder that mails transcripts of the
// operator's OWN meetings. One of the restored pages opens "오형석 회장 주재 회의
// 녹취 전사 … 화웨이가 빨리 안 되면 썬그로우로 간다는 방향을 확정했다".
//
// The mistake was not the idea, it was the verification: the pass was justified
// by reading ONE sample, which happened to be a 17-second lunch memo, and
// generalising from it. Sizes alone would have shown it — the deleted meeting
// records run 3.6–7.5KB against ~1.2KB for the newsletters that genuinely go.
//
// There is nothing to replace it with, because spammail already covers the real
// case from the other direction: it reads what the ANALYSIS concluded, and it
// caught both genuinely worthless Plaud pages (the lunch memo and a "cannot
// fully transcribe" notice) while leaving every meeting record alone. The pass
// built to AVOID trusting model output is the one that was wrong; the content
// the analyst wrote knew what the sender address could not.

// --- pass: spammail ---------------------------------------------------------

// deleteSelfDeclaredSpamMail removes 메일분석 pages whose own opening line
// classifies the mail as bulk — advertising, a newsletter, a marketing blast, a
// subscription/billing notice.
//
// This pass trusts the analysis body's conclusion, which the other passes
// deliberately do not: noisemail keys on the sender address precisely because
// trusting model output as evidence is the failure mode this whole campaign has
// been closing. It is used here because the operator directed it ("스팸 메일성
// 메일분석 페이지는 다 지워버려"), and because the judgement is cheap to check —
// the verdict sits in the first line, in the analyst's own words, and a human
// reading the list can see whether each one is right.
//
// The judgement itself is mailanalysis.AnalysisNonBusiness — the same predicate
// the autonomous write path now applies, so the corpus and the intake gate
// cannot disagree about what counts as bulk mail (the discipline the unusable
// pass follows with AnalysisUsable). Its two guards — the verdict must LEAD the
// opening line, and the line must not be drawing a contrast with other mail —
// live there with the corpus evidence for each.
func deleteSelfDeclaredSpamMail(store *wiki.Store, apply bool, rep *report) error {
	paths, err := store.ListPages("프로젝트")
	if err != nil {
		return err
	}
	bySender := map[string]int{}
	for _, rp := range paths {
		if !strings.Contains(filepath.ToSlash(rp), "/메일분석/") {
			continue
		}
		page, err := store.ReadPage(rp)
		if err != nil || page == nil {
			continue
		}
		if mailanalysis.AnalysisNonBusiness(page.Body) == nil {
			continue
		}
		line := firstProseLine(page.Body)
		from := provenanceFrom(page.Body)
		if !apply {
			rep.add("would delete %s — %s", rp, truncRunes(line, 70))
			bySender[senderAddr(from)]++
			continue
		}
		if err := store.DeletePage(rp); err != nil {
			rep.fail("spammail %s: %v", rp, err)
			continue
		}
		bySender[senderAddr(from)]++
		rep.add("deleted %s — %s", rp, truncRunes(line, 70))
	}
	senders := make([]string, 0, len(bySender))
	for k := range bySender {
		senders = append(senders, k)
	}
	sort.Slice(senders, func(a, b int) bool {
		if bySender[senders[a]] != bySender[senders[b]] {
			return bySender[senders[a]] > bySender[senders[b]]
		}
		return senders[a] < senders[b]
	})
	for _, sdr := range senders {
		rep.note("  발신자별: %-46s %d장", sdr, bySender[sdr])
	}
	return nil
}

// firstProseLine returns the first line of actual analysis — past the
// frontmatter, the provenance blockquote, and any heading.
func firstProseLine(body string) string {
	for _, ln := range strings.Split(body, "\n") {
		t := strings.TrimSpace(ln)
		if t == "" || strings.HasPrefix(t, ">") || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "---") {
			continue
		}
		return t
	}
	return ""
}

func senderAddr(from string) string {
	if i := strings.IndexByte(from, '<'); i >= 0 {
		if j := strings.IndexByte(from[i:], '>'); j > 0 {
			return strings.ToLower(from[i+1 : i+j])
		}
	}
	if from == "" {
		return "(unknown)"
	}
	return strings.ToLower(from)
}

func truncRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// provenanceFrom pulls the sender out of the "> From: …" line that
// buildMailAnalysisPage writes above every analysis. spammail reports it so the
// deletion list can be read by sender, which is how a person spots a sender that
// should not be in the list at all.
func provenanceFrom(body string) string {
	for _, ln := range strings.Split(body, "\n") {
		t := strings.TrimSpace(ln)
		if !strings.HasPrefix(t, ">") {
			if t != "" && !strings.HasPrefix(t, "#") {
				return ""
			}
			continue
		}
		t = strings.TrimSpace(strings.TrimPrefix(t, ">"))
		if rest, ok := strings.CutPrefix(t, "From:"); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// --- pass: foldsites --------------------------------------------------------

// foldSitePagesIntoProject folds each 현장 page into its project's 대표 page and
// deletes it.
//
// 현장 pages were never a documentation slot — they were the data model for the
// 현장 지도, one page per map pin (address=position, capacity=size, kinds=colour
// and shape, and five milestone dates=timeline). The client dropped that map in
// #4517 and the gateway read path went with it, so a page per pin now costs a
// page and buys nothing. The corpus shows it: of 30 pages the five schedule
// fields are filled 0/30 — nobody fills a field with nowhere to render.
//
// What actually has to survive is narrow. 29 of 30 addresses are ALREADY in the
// project page's sites[] (that is where seed-sites bootstrapped them from), and
// the project page carries client and kinds of its own. Unique to the site pages
// are capacity (19) and status (13), which 대표 has no field for at all — so they
// go into a body table rather than new frontmatter, and the fold stays a
// presentation change instead of a schema change.
//
// A project that already has a hand-written "## 현장" section keeps it untouched
// and is only reported: those tables (기아 충주 8,100kW · 이천덕평 5,252kW …) are
// richer than anything derivable from a seeded stub, and overwriting curated
// prose with generated rows is how a repair becomes a regression.
func foldSitePagesIntoProject(store *wiki.Store, apply bool, rep *report) error {
	paths, err := store.ListPages("프로젝트")
	if err != nil {
		return err
	}
	byProject := map[string][]*wiki.Page{}
	pagePath := map[*wiki.Page]string{}
	for _, rp := range paths {
		slash := filepath.ToSlash(rp)
		if !strings.Contains(slash, "/현장/") {
			continue
		}
		page, err := store.ReadPage(rp)
		if err != nil || page == nil {
			continue
		}
		proj, ok := wiki.ProjectNameOf(slash)
		if !ok || proj == "" {
			rep.fail("foldsites: %s — 소유 프로젝트를 못 찾음", rp)
			continue
		}
		byProject[proj] = append(byProject[proj], page)
		pagePath[page] = rp
	}

	projects := make([]string, 0, len(byProject))
	for k := range byProject {
		projects = append(projects, k)
	}
	sort.Strings(projects)

	for _, proj := range projects {
		sites := byProject[proj]
		sort.Slice(sites, func(a, b int) bool { return sites[a].Meta.Title < sites[b].Meta.Title })
		repPath := wiki.RepPagePath(proj)
		repPage, err := store.ReadPage(repPath)
		if err != nil || repPage == nil {
			rep.fail("foldsites: %s 대표 페이지 없음 — %d장 보류", proj, len(sites))
			continue
		}
		if strings.Contains(repPage.Body, "\n## 현장") {
			rep.note("건너뜀(대표에 손으로 쓴 ## 현장 있음) %s — 현장 %d장 유지", proj, len(sites))
			continue
		}
		section, addrs := siteSection(sites)
		if !apply {
			rep.add("would fold %d site(s) into %s (+주소 %d개)", len(sites), repPath, len(addrs))
			continue
		}
		if err := store.UpdatePage(repPath, func(cur *wiki.Page) (*wiki.Page, error) {
			if cur == nil {
				return nil, nil
			}
			cur.Body = strings.TrimRight(cur.Body, "\n") + "\n\n" + section
			cur.Meta.Sites = mergeStrings(cur.Meta.Sites, addrs)
			return cur, nil
		}); err != nil {
			rep.fail("foldsites %s: %v", repPath, err)
			continue
		}
		folded := 0
		for _, sp := range sites {
			if err := store.DeletePage(pagePath[sp]); err != nil {
				rep.fail("foldsites delete %s: %v", pagePath[sp], err)
				continue
			}
			folded++
		}
		rep.add("folded %d site(s) into %s", folded, repPath)
	}
	return nil
}

// siteSection renders the fold target: the fields the map used to carry, as a
// table a person reads. Columns with nothing in them are dropped, so a project
// whose sites only ever had an address gets two columns, not six empty ones.
func siteSection(sites []*wiki.Page) (string, []string) {
	type col struct {
		head string
		get  func(*wiki.Page) string
	}
	cols := []col{
		{"현장", func(p *wiki.Page) string { return p.Meta.Title }},
		{"주소", func(p *wiki.Page) string { return p.Meta.Address }},
		{"거래처", func(p *wiki.Page) string { return p.Meta.Client }},
		{"특성", func(p *wiki.Page) string { return strings.Join(p.Meta.Kinds, "·") }},
		{"상태", func(p *wiki.Page) string { return p.Meta.Status }},
		{"용량", func(p *wiki.Page) string {
			if p.Meta.Capacity == 0 {
				return ""
			}
			return strings.TrimSuffix(strings.TrimRight(fmt.Sprintf("%.2f", p.Meta.Capacity), "0"), ".") + "MW"
		}},
	}
	used := make([]col, 0, len(cols))
	for _, c := range cols {
		for _, sp := range sites {
			if strings.TrimSpace(c.get(sp)) != "" {
				used = append(used, c)
				break
			}
		}
	}
	var b strings.Builder
	b.WriteString("## 현장\n\n|")
	for _, c := range used {
		b.WriteString(" " + c.head + " |")
	}
	b.WriteString("\n|")
	for range used {
		b.WriteString("---|")
	}
	b.WriteString("\n")
	var addrs []string
	for _, sp := range sites {
		b.WriteString("|")
		for _, c := range used {
			b.WriteString(" " + strings.TrimSpace(c.get(sp)) + " |")
		}
		b.WriteString("\n")
		if a := strings.TrimSpace(sp.Meta.Address); a != "" {
			addrs = append(addrs, a)
		}
	}
	return b.String(), addrs
}

// mergeStrings appends the values of b that a does not already hold.
func mergeStrings(a, b []string) []string {
	have := make(map[string]bool, len(a))
	for _, v := range a {
		have[strings.TrimSpace(v)] = true
	}
	for _, v := range b {
		if v = strings.TrimSpace(v); v != "" && !have[v] {
			a = append(a, v)
			have[v] = true
		}
	}
	return a
}

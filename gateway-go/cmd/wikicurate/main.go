// wikicurate applies the 2026-08-23 P0/P1 wiki-ledger cleanup (split
// contaminated pages, fold duplicate counterparties, file 4 known orphan
// mails, restore 보배 SPA, cap related/tags, expire stale open questions).
//
// Dry-run by default. --apply writes.
//
// ⚠️ Stop the gateway before --apply: Store locking is in-process only, and a
// live gateway holds in-memory search/index state. Restart afterwards.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "wikicurate:", err)
		os.Exit(1)
	}
}

func run() error {
	home, _ := os.UserHomeDir()
	wikiDir := flag.String("wiki-dir", filepath.Join(home, ".deneb", "wiki"), "wiki root")
	diaryDir := flag.String("diary-dir", filepath.Join(home, ".deneb", "memory", "diary"), "diary root")
	reviewState := flag.String("review-state", filepath.Join(home, ".deneb", "wiki-review-state.json"), "wiki-review observe trail")
	apply := flag.Bool("apply", false, "execute (default: dry-run)")
	flag.Parse()

	store, err := wiki.NewStore(*wikiDir, *diaryDir)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer store.Close()

	now := time.Now()
	rep := &report{}

	if *apply {
		if h := store.SnapshotGit(context.Background(), "wikicurate: pre P0/P1"); h != "" {
			fmt.Println("snapshot", h)
		}
	}

	if err := splitContamination(store, now, *apply, rep); err != nil {
		return err
	}
	if err := fileKnownOrphans(store, now, *apply, rep); err != nil {
		return err
	}
	if err := foldDeals(store, now, *apply, rep); err != nil {
		return err
	}
	if err := emptyMiscDeals(store, *apply, rep); err != nil {
		return err
	}
	if err := restoreBobaeSPA(store, *apply, rep); err != nil {
		return err
	}
	if err := cleanVaultwarden(store, now, *apply, rep); err != nil {
		return err
	}

	if *apply {
		n, err := store.CapPageRelations()
		if err != nil {
			return fmt.Errorf("cap relations: %w", err)
		}
		rep.add("cap-related-tags %d pages", n)
		n = store.ExpireStaleOpenQuestions(now, wiki.OpenQuestionExpireAfterDays)
		rep.add("expire-questions %d pages", n)
		if err := dedupeReviewState(*reviewState); err != nil {
			rep.add("review-state: %v", err)
		} else {
			rep.add("review-state deduped")
		}
		if h := store.SnapshotGit(context.Background(), "wikicurate: P0/P1 applied"); h != "" {
			fmt.Println("snapshot", h)
		}
	} else {
		rep.add("dry-run: skip CapPageRelations / ExpireStaleOpenQuestions / review-state")
	}

	mode := "DRY-RUN"
	if *apply {
		mode = "APPLIED"
	}
	fmt.Printf("== wikicurate %s ==\n", mode)
	for _, line := range rep.lines {
		fmt.Println("  " + line)
	}
	if len(rep.errs) > 0 {
		fmt.Println("-- errors --")
		for _, e := range rep.errs {
			fmt.Println("  ERROR " + e)
		}
		return fmt.Errorf("%d actions failed", len(rep.errs))
	}
	if !*apply {
		fmt.Println("(dry-run — nothing written; stop the gateway and re-run with --apply)")
	}
	return nil
}

type report struct {
	lines []string
	errs  []string
}

func (r *report) add(format string, args ...any) {
	r.lines = append(r.lines, fmt.Sprintf(format, args...))
}

func (r *report) fail(format string, args ...any) {
	r.errs = append(r.errs, fmt.Sprintf(format, args...))
}

func exists(store *wiki.Store, path string) bool {
	p, err := store.ReadPage(path)
	return err == nil && p != nil
}

func moveOrFold(store *wiki.Store, from, to string, apply bool, rep *report) {
	if !exists(store, from) {
		rep.add("skip missing %s", from)
		return
	}
	if !apply {
		if exists(store, to) {
			rep.add("would fold %s → %s", from, to)
			return
		}
		rep.add("would move %s → %s", from, to)
		return
	}
	if exists(store, to) {
		if err := store.FoldDuplicate(to, from); err != nil {
			rep.fail("fold %s → %s: %v", from, to, err)
			return
		}
		rep.add("folded %s → %s", from, to)
		return
	}
	if err := store.MovePage(from, to); err != nil {
		rep.fail("move %s → %s: %v", from, to, err)
		return
	}
	rep.add("moved %s → %s", from, to)
}

func splitContamination(store *wiki.Store, now time.Time, apply bool, rep *report) error {
	const g2 = "기타/even-realities-g2-smart-glasses.md"
	const scoPath = "기타/데이팅앱-스코폴라민-콜롬비아-경고.md"
	const mcPath = "기타/맥코넬-의원-건강-이슈-2026.md"
	const feinPath = "기타/페인스타인-사건.md"

	g2page, err := store.ReadPage(g2)
	if err != nil {
		rep.fail("read G2: %v", err)
	} else if g2page != nil {
		marker := "# 데이팅 앱 기반 범죄"
		idx := strings.Index(g2page.Body, marker)
		if idx < 0 {
			rep.add("G2: no scopolamine section (already split?)")
		} else if !apply {
			rep.add("would split G2 scopolamine → %s", scoPath)
		} else {
			scoBody := strings.TrimSpace(g2page.Body[idx:])
			g2keep := strings.TrimSpace(g2page.Body[:idx]) + "\n"
			sco := wiki.NewPage("데이팅 앱 기반 범죄: 스코폴라민·패스포트 브로스", "기타",
				[]string{"데이팅앱", "콜롬비아", "스코폴라민", "범죄수법", "패스포트브로스", "미국대사관"})
			sco.Meta.ID = "dating-app-scopolamine-threat"
			sco.Meta.Summary = "미 대사관 메데인 여행 경고: 데이팅 앱 기반 패스포트 브로스와 스코폴라민 금융 사기"
			sco.Meta.Type = "concept"
			sco.Meta.Confidence = "high"
			sco.Meta.Importance = 0.55
			sco.Body = scoBody + "\n"
			if err := store.WritePage(scoPath, sco); err != nil {
				rep.fail("write scopolamine: %v", err)
			} else {
				rep.add("wrote %s", scoPath)
			}
			if err := store.UpdatePage(g2, func(cur *wiki.Page) (*wiki.Page, error) {
				if cur == nil {
					return nil, nil
				}
				cur.Meta.ID = "even-realities-g2"
				cur.Meta.Summary = "Even Realities G2 스마트 안경 — 번역·AI, 카메라 없음. MemoMind One·INMO GO3와 비교."
				cur.Meta.Tags = []string{"스마트안경", "번역", "AI", "G2", "Even Realities", "MemoMind", "INMO", "웨어러블"}
				cur.Meta.Related = []string{"기타/xr-글라스-정의.md", "사용자/오선택-사용자모델.md"}
				cur.Meta.Updated = now.Format("2006-01-02")
				cur.Body = g2keep
				return cur, nil
			}); err != nil {
				rep.fail("update G2: %v", err)
			} else {
				rep.add("cleaned %s", g2)
			}
		}
	}

	if exists(store, feinPath) {
		rep.add("페인스타인 page already exists")
	} else if !apply {
		rep.add("would write %s and delete %s", feinPath, mcPath)
	} else {
		fein := wiki.NewPage("페인스타인 사건", "기타",
			[]string{"페인스타인", "미 상원", "건강", "치매", "의무 부재"})
		fein.Meta.ID = "feinstein-casualty"
		fein.Meta.Summary = "다이앤 페인스타인(89세) — 2023년 뇌염 입원·기억상실, 의원실 3개월 결석 보도, 연방판사 인준 마비, 사망 후 정치적 후폭풍"
		fein.Meta.Type = "entity"
		fein.Meta.Confidence = "high"
		fein.Meta.Importance = 0.50
		fein.Meta.Cues = []string{"Dianne Feinstein", "상원 의장", "연방판사 인준"}
		fein.Body = `# 페인스타인 사건

다이앤 페인스타인(Dianne Feinstein) 미 상원 의원(당시 89세)의 건강 악화와 의정 공백, 사망 이후 정치적 후폭풍.

## 핵심 사실

- 2023년 뇌염으로 입원, 기억상실 증상 보도
- 의원실 '3개월 결석' 보도, 연방판사 인준 마비
- 사망 후 공석·후임 임명을 둘러싼 정치적 파급

## 변경 이력

- 2026-08-23: 맥코넬 페이지(id=feinstein-casualty, 본문 공란)에서 분리
`
		if err := store.WritePage(feinPath, fein); err != nil {
			rep.fail("write 페인스타인: %v", err)
		} else {
			rep.add("wrote %s", feinPath)
		}
		if exists(store, mcPath) {
			if err := store.DeletePage(mcPath); err != nil {
				rep.fail("delete 맥코넬: %v", err)
			} else {
				rep.add("deleted %s", mcPath)
			}
		}
	}
	return nil
}

func fileKnownOrphans(store *wiki.Store, now time.Time, apply bool, rep *report) error {
	moves := []struct{ msgID, project string }{
		{"15653621c-ebe2-478c-8ab8-9a70e62d2b6c@barocorp.com", "pl1-sin-bes-001"},
		{"2026070917302539227723@sunkean.com", "nde-sun-cbl-001"},
		{"2026071311003692883827@sunkean.com", "nde-sun-cbl-001"},
		{"2026072315482244896445@sunkean.com", "nde-sun-cbl-001"},
	}
	for _, m := range moves {
		from := wiki.MailAnalysisPagePath("", m.msgID)
		to := wiki.MailAnalysisPagePath(m.project, m.msgID)
		if !exists(store, from) {
			rep.add("orphan already filed or missing: %s", from)
			continue
		}
		if !apply {
			rep.add("would file %s → %s", from, to)
			continue
		}
		if err := store.MovePage(from, to); err != nil {
			rep.fail("file %s: %v", from, err)
			continue
		}
		repPath := wiki.RepPagePath(m.project)
		_ = store.UpdatePage(to, func(cur *wiki.Page) (*wiki.Page, error) {
			if cur == nil {
				return nil, nil
			}
			for _, r := range cur.Meta.Related {
				if r == repPath {
					return nil, nil
				}
			}
			cur.Meta.Related = append(cur.Meta.Related, repPath)
			cur.Meta.Updated = now.Format("2006-01-02")
			return cur, nil
		})
		rep.add("filed %s → %s", from, to)
	}
	return nil
}

func foldDeals(store *wiki.Store, now time.Time, apply bool, rep *report) error {
	if apply {
		kiaDup := "프로젝트/거래/기아-광주2공장-기아오토랜드-광주공장.md"
		if exists(store, kiaDup) {
			_ = store.UpdatePage(kiaDup, func(cur *wiki.Page) (*wiki.Page, error) {
				if cur == nil {
					return nil, nil
				}
				cur.Meta.Summary = "기아오토랜드 광주2공장 캐노피 태양광 견적서 제출, 공급가액 858,000,000원"
				cur.Body = strings.ReplaceAll(cur.Body, "공급가액 858,000,000원, 합계 943,800,000원", "공급가액 858,000,000원")
				cur.Body = strings.ReplaceAll(cur.Body, "943,800,000원", "858,000,000원")
				cur.Meta.Updated = now.Format("2006-01-02")
				return cur, nil
			})
		}
	}

	folds := []struct{ keep, fold string }{
		{"프로젝트/거래/joca-cable-group-co-ltd.md", "프로젝트/거래/joca.md"},
		{"프로젝트/거래/joca-cable-group-co-ltd.md", "프로젝트/거래/joca-cable-group.md"},
		{"프로젝트/거래/선그로우파워코리아.md", "프로젝트/거래/선그로우.md"},
		{"프로젝트/거래/선그로우파워코리아.md", "프로젝트/거래/썬그로우.md"},
		{"프로젝트/거래/헥사리뉴어블코리아-hexa-renewables.md", "프로젝트/거래/hexa.md"},
		{"프로젝트/거래/헥사리뉴어블코리아-hexa-renewables.md", "프로젝트/거래/헥사-hexa.md"},
		{"프로젝트/거래/기아오토랜드-광주공장.md", "프로젝트/거래/기아-광주2공장-기아오토랜드-광주공장.md"},
		{"프로젝트/거래/바로-주.md", "기타/거래/바로-주식회사.md"},
		{"기타/거래/주-광주일보사.md", "기타/거래/광주일보사.md"},
	}
	for _, f := range folds {
		if !exists(store, f.fold) {
			rep.add("fold skip missing %s", f.fold)
			continue
		}
		if !apply {
			rep.add("would fold %s → %s", f.fold, f.keep)
			continue
		}
		if err := store.FoldDuplicate(f.keep, f.fold); err != nil {
			rep.fail("fold %s → %s: %v", f.fold, f.keep, err)
			continue
		}
		rep.add("folded %s → %s", f.fold, f.keep)
	}

	// 썬그로우 alias on the keeper.
	if apply && exists(store, "프로젝트/거래/선그로우파워코리아.md") {
		_ = store.UpdatePage("프로젝트/거래/선그로우파워코리아.md", func(cur *wiki.Page) (*wiki.Page, error) {
			if cur == nil {
				return nil, nil
			}
			cur.Meta.Cues = wikiMergeCue(cur.Meta.Cues, "썬그로우")
			return cur, nil
		})
	}

	moveOrFold(store, "기타/거래/주-광주일보사.md", "인물/광주일보사.md", apply, rep)
	return nil
}

func wikiMergeCue(existing []string, add string) []string {
	for _, c := range existing {
		if c == add {
			return existing
		}
	}
	return append(append([]string{}, existing...), add)
}

func emptyMiscDeals(store *wiki.Store, apply bool, rep *report) error {
	// 01:07 warehouse: deals back to 프로젝트/거래, orgs to 인물/. Keep
	// 업무/거래 (경비) and 인물/거래 (사람) alone.
	backToDeals := []string{"대한전선-당진공장.md", "현대.md"}
	for _, name := range backToDeals {
		moveOrFold(store, "기타/거래/"+name, "프로젝트/거래/"+name, apply, rep)
	}

	pages, err := store.ListPages("기타/거래")
	if err != nil {
		return err
	}
	skip := map[string]bool{
		"기타/거래/대한전선-당진공장.md": true,
		"기타/거래/현대.md":        true,
		"기타/거래/바로-주식회사.md":   true, // folded above
		"기타/거래/광주일보사.md":     true, // folded above
		"기타/거래/주-광주일보사.md":   true, // moved above
	}
	for _, rp := range pages {
		rp = filepath.ToSlash(rp)
		if skip[rp] {
			continue
		}
		base := filepath.Base(rp)
		moveOrFold(store, rp, "인물/"+base, apply, rep)
	}
	return nil
}

func restoreBobaeSPA(store *wiki.Store, apply bool, rep *report) error {
	from := "기타/pl1-bbw-wnd-001/spa.md"
	to := "프로젝트/pl1-bbw-wnd-001/spa.md"
	moveOrFold(store, from, to, apply, rep)
	return nil
}

func cleanVaultwarden(store *wiki.Store, now time.Time, apply bool, rep *report) error {
	const path = "시스템/vaultwarden.md"
	if !exists(store, path) {
		rep.add("vaultwarden missing")
		return nil
	}
	if !apply {
		rep.add("would clean vaultwarden tags/related + drop ISW merge stub")
		return nil
	}
	err := store.UpdatePage(path, func(cur *wiki.Page) (*wiki.Page, error) {
		if cur == nil {
			return nil, nil
		}
		cur.Meta.ID = "vaultwarden"
		cur.Meta.Tags = []string{"vaultwarden", "bitwarden", "비밀번호", "셀프호스트", "docker", "tailscale"}
		cur.Meta.Related = []string{"시스템/mikrotik-crs812-라우터.md", "시스템/rollback-drill-20260816.md"}
		cur.Meta.Updated = now.Format("2006-01-02")
		if i := strings.Index(cur.Body, "## 병합된 중복 문서 (기타/isw-가족-소유.md)"); i >= 0 {
			cur.Body = strings.TrimRight(cur.Body[:i], "\n") + "\n"
		}
		return cur, nil
	})
	if err != nil {
		rep.fail("vaultwarden: %v", err)
		return nil
	}
	rep.add("cleaned vaultwarden")
	return nil
}

type reviewStateFile struct {
	Version      int      `json:"version"`
	LastReviewMs int64    `json:"lastReviewMs"`
	Observed     []string `json:"observed,omitempty"`
}

func dedupeReviewState(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var st reviewStateFile
	if err := json.Unmarshal(data, &st); err != nil {
		return err
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(st.Observed))
	for _, line := range st.Observed {
		key := line
		if i := strings.Index(line, " | "); i >= 0 {
			key = line[i+3:]
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, line)
	}
	st.Observed = out
	next, err := json.MarshalIndent(&st, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.WriteFile(path, next, &atomicfile.Options{Perm: 0o600})
}

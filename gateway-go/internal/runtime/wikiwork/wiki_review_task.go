// wiki_review_task.go — 위키 리뷰어: post-write duplicate review of recently
// touched wiki pages (the skill-reviewer pattern applied to memory writes).
//
// Every write path can miss: the wiki tool's pre-write guard and the dreamer's
// create-dedup check before writing, but an agent can force, an RPC client can
// write blind, and near-duplicates slip past slug/FTS heuristics. This task is
// the safety net BEHIND all of them: every couple of hours it reads the wiki
// audit log (log.md) for pages created/updated since its last pass, finds
// near-match candidates with the same FindSimilarPages primitive the guards
// use, and asks a small local LLM for a duplicate verdict. High-confidence
// duplicates are folded together with the same reversible merge machinery the
// dream cycle's verify pass uses (git snapshot first, capped per cycle).
//
// Deliberately NOT an agent turn: the skill-review lesson (#3006 area) is that
// text-role models never make tool calls, so this is a bounded pipeline —
// deterministic gather → one JSON verdict call → deterministic apply.
// Model role: main (operator's explicit 2026-07-02 quality call — a wrong "high"
// verdict merges two real pages, so judgment quality beats the local-first
// default; the analysis role was retired 2026-07-07, so this quality intent now
// lands on main, the strongest role. Volume is tiny: at most one call per 2h
// cycle, zero when no candidates). Fail-open: any error logs and skips the cycle.
//
// Rollout safety: auto-merge starts OFF (observe mode) — verdicts are logged
// and recorded in the state file so the operator can audit judgment quality,
// then flip DENEB_WIKI_REVIEW_AUTOMERGE=1 to arm the merges.
package wikiwork

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/monitoring"
	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// Compile-time interface compliance.
var _ autonomous.PeriodicTask = (*wikiReviewTask)(nil)

const (
	// wikiReviewInterval is the review cadence. Long enough to batch a work
	// session's writes, short enough that a duplicate never survives a day.
	wikiReviewInterval = 2 * time.Hour
	// wikiReviewMaxPages bounds how many touched pages one cycle examines.
	wikiReviewMaxPages = 12
	// wikiReviewMaxMerges bounds auto-merges per cycle (blast radius; the rest
	// waits for the next cycle or the dream verify pass).
	wikiReviewMaxMerges = 3
	// wikiReviewLLMTimeout bounds the single verdict call.
	wikiReviewLLMTimeout = 90 * time.Second
	// wikiReviewStateFile persists the last-review high-water mark.
	wikiReviewStateFile = "wiki-review-state.json"
)

const (
	// ReviewStateFile is the durable duplicate-review cursor filename.
	ReviewStateFile = wikiReviewStateFile
	// ReviewInterval is the autonomous review cadence.
	ReviewInterval = wikiReviewInterval
)

type wikiReviewState struct {
	Version      int   `json:"version"`
	LastReviewMs int64 `json:"lastReviewMs"`
	// Observed accumulates would-merge verdicts made while auto-merge is off
	// (observe mode), newest last, capped — the operator's audit trail for
	// deciding when to arm DENEB_WIKI_REVIEW_AUTOMERGE=1.
	Observed []string `json:"observed,omitempty"`
	// DeadLinks tracks when each still-dead body wikilink ("page\x00target")
	// was first seen by the prune sweep. A dead target can self-heal — a purged
	// person re-seeded, a page recreated — so a link is only unwrapped to prose
	// after staying dead across the whole grace window. Healed or unwrapped
	// entries are dropped, so the map tracks current rot, not history.
	DeadLinks map[string]int64 `json:"deadLinks,omitempty"`
}

// wikiDeadLinkGraceDays is how long a dead body wikilink may wait for its
// target to come back before being unwrapped to prose.
const wikiDeadLinkGraceDays = 14

// reconcileDeadLinks updates the observed-dead ledger against the current
// sweep's findings and returns the links whose grace has run out, grouped by
// page. Pure — the task's Run wires the store calls around it.
func reconcileDeadLinks(ledger map[string]int64, current []wiki.DeadWikiLink, now time.Time) (map[string]int64, map[string]map[string]bool) {
	next := make(map[string]int64, len(current))
	condemned := map[string]map[string]bool{}
	cutoff := now.AddDate(0, 0, -wikiDeadLinkGraceDays).UnixMilli()
	for _, d := range current {
		key := d.Page + "\x00" + d.Target
		first, seen := ledger[key]
		if !seen {
			first = now.UnixMilli()
		}
		if first <= cutoff {
			if condemned[d.Page] == nil {
				condemned[d.Page] = map[string]bool{}
			}
			condemned[d.Page][d.Target] = true
			continue // unwrapped this cycle — do not carry forward
		}
		next[key] = first
	}
	return next, condemned
}

// wikiReviewObservedCap bounds the observe-mode audit trail in the state file.
const wikiReviewObservedCap = 100

// wikiReviewTask implements autonomous.PeriodicTask.
type wikiReviewTask struct {
	wikiStore *wiki.Store
	activity  *monitoring.ActivityTracker
	logger    *slog.Logger
	statePath string
	// autoMerge arms the destructive step. Default false = observe mode:
	// verdicts are logged + recorded, nothing is merged.
	autoMerge bool
	// llm is the verdict call, injectable for tests. Defaults to the main
	// role (pilot.CallRoleLLM(RoleMain)) — bounded JSON judgment, no tool calls.
	llm func(ctx context.Context, system, user string, maxTokens int) (string, error)
}

// ReviewTask reviews recent wiki writes and performs deterministic maintenance.
type ReviewTask = wikiReviewTask

// NewReviewTask constructs the post-write wiki reviewer.
func NewReviewTask(
	wikiStore *wiki.Store,
	activity *monitoring.ActivityTracker,
	logger *slog.Logger,
	statePath string,
	autoMerge bool,
	llm func(context.Context, string, string, int) (string, error),
) *ReviewTask {
	return &wikiReviewTask{
		wikiStore: wikiStore,
		activity:  activity,
		logger:    logger,
		statePath: statePath,
		autoMerge: autoMerge,
		llm:       llm,
	}
}

// Name returns the component's stable scheduler name.
func (t *wikiReviewTask) Name() string { return "wiki-review" }

// Interval returns the component's scheduling cadence.
func (t *wikiReviewTask) Interval() time.Duration { return wikiReviewInterval }

// wikiReviewSuspect pairs one recently-written page with its near-match candidates.
type wikiReviewSuspect struct {
	path       string
	title      string
	summary    string
	candidates []wiki.SimilarHit
}

// wikiReviewVerdict is the LLM's per-page judgment.
type wikiReviewVerdict struct {
	Page        string `json:"page"`
	DuplicateOf string `json:"duplicate_of"` // "" = distinct
	Confidence  string `json:"confidence"`   // high | medium | low
}

// Run executes one scheduled task cycle.
func (t *wikiReviewTask) Run(ctx context.Context) error {
	if t.wikiStore == nil {
		return fmt.Errorf("wiki-review: wiki store not available")
	}
	// Defer to the user: even a lightweight call competes for the local GPU.
	if t.activity != nil {
		idle := time.Duration(time.Now().UnixMilli()-t.activity.LastActivityAt()) * time.Millisecond
		if idle < 2*time.Minute {
			t.logger.Info("wiki-review: skipped, user active", "idle", idle.Round(time.Second))
			return nil
		}
	}

	state := t.loadState()
	since := time.UnixMilli(state.LastReviewMs)
	scanStart := time.Now()

	touched := t.recentlyTouchedPages(since)
	// Advance the high-water mark regardless of the outcome below — a failing
	// page or LLM hiccup must not re-queue the same batch forever.
	state.LastReviewMs = scanStart.UnixMilli()
	if err := t.saveState(state); err != nil {
		t.logger.Warn("wiki-review: failed to persist state", "error", err)
	}

	// The LLM-backed duplicate review runs only when pages were recently
	// written, but it is fully self-contained: every early exit inside it (no
	// touched pages, no candidates, verdict error) must NOT skip the
	// deterministic maintenance sweep below. That sweep is the wiki's real
	// upkeep — log rotation, dormancy nudge, dead-link pruning, mail refiling —
	// and has to run on quiet cycles too, the very cycles where nothing was
	// touched. (It used to sit after the duplicate-review early-returns and so
	// almost never ran.)
	merged, suspectCount := t.reviewDuplicates(ctx, touched, state)

	// Deterministic maintenance — always runs, no LLM, independent of the review.
	// Dormancy nudge FIRST: log rotation stamps 로그.md/로그-보관.md Updated with
	// today, which would reset the whole folder's activity clock and push a
	// genuinely dormant project's detection out another ~120 days.
	// Long-inactive ACTIVE projects get one 종결-검토 bullet on their 대표페이지
	// (surfaces in the 모아보기; quarter-idempotent; never auto-closes). Capped
	// so a backlog can't flood the digest view.
	dormant := t.wikiStore.FlagDormantProjects(time.Now(), 2)
	rotated := t.rotateProjectLogs()
	// Graph hygiene: repair/drop dead Related references (idempotent; the first
	// sweep clears the historical rot, later ones only touch fresh drift).
	prune, perr := t.wikiStore.PruneDeadRelatedLinks()
	if perr != nil {
		t.logger.Warn("wiki-review: dead-link prune failed", "error", perr)
	} else if prune.PagesChanged > 0 {
		t.logger.Info("wiki-review: dead links pruned",
			"pages", prune.PagesChanged, "repointed", prune.Repointed, "removed", prune.Removed)
	}
	// Same ladder over body [[links]]: an unambiguous rename is rewritten, an
	// ambiguous or vanished target stays as prose and is only counted.
	bodyLinks, deadNow, blerr := t.wikiStore.PruneDeadWikiLinks()
	if blerr != nil {
		t.logger.Warn("wiki-review: wikilink prune failed", "error", blerr)
	} else {
		if bodyLinks.PagesChanged > 0 || bodyLinks.Removed > 0 {
			t.logger.Info("wiki-review: body wikilinks repaired",
				"pages", bodyLinks.PagesChanged, "repointed", bodyLinks.Repointed,
				"stillDead", bodyLinks.Removed)
		}
		// Terminal rung of the ladder: a link that stayed dead across the whole
		// grace window has proven its target is not coming back — unwrap it to
		// prose so it stops rendering as a broken destination.
		nextLedger, condemned := reconcileDeadLinks(state.DeadLinks, deadNow, time.Now())
		unwrapped := 0
		for page, targets := range condemned {
			n, uerr := t.wikiStore.UnwrapWikiLinks(page, targets)
			if uerr != nil {
				t.logger.Warn("wiki-review: dead-link unwrap failed", "page", page, "error", uerr)
				// Keep the ledger entries so the next cycle retries instead of
				// restarting their grace from zero.
				for target := range targets {
					nextLedger[page+"\x00"+target] = time.Now().AddDate(0, 0, -wikiDeadLinkGraceDays).UnixMilli()
				}
				continue
			}
			unwrapped += n
		}
		if unwrapped > 0 {
			t.logger.Info("wiki-review: dead body wikilinks unwrapped to prose",
				"links", unwrapped, "pages", len(condemned))
		}
		state.DeadLinks = nextLedger
		if err := t.saveState(state); err != nil {
			t.logger.Warn("wiki-review: failed to persist dead-link ledger", "error", err)
		}
	}
	// Retroactive mail filing: unlinked analyses whose project has since become
	// known move into that project's 메일분석 slot (deterministic signals only).
	// Domain-signal proposals (observe mode, until DENEB_MAIL_RECLASS_DOMAIN=1)
	// go to the state file's audit trail instead of moving — the same
	// observe→arm pattern as duplicate merges.
	refiled := 0
	moved, proposals := t.wikiStore.ReclassifyUnlinkedMailAnalyses(time.Now(), 10)
	for _, m := range moved {
		t.logger.Info("wiki-review: unlinked mail re-filed",
			"from", m.From, "project", m.Project, "signal", m.Signal)
		refiled++
	}
	if len(proposals) > 0 {
		state := t.loadState()
		for _, p := range proposals {
			t.logger.Info("wiki-review: domain-signal refile proposal (observe mode — no move)",
				"from", p.From, "project", p.Project)
			appendObserved(state, fmt.Sprintf("%s | mail-domain %s → %s",
				time.Now().Format("2006-01-02 15:04"), p.From, p.Project))
		}
		if over := len(state.Observed) - wikiReviewObservedCap; over > 0 {
			state.Observed = state.Observed[over:]
		}
		if err := t.saveState(state); err != nil {
			t.logger.Warn("wiki-review: failed to persist domain proposals", "error", err)
		}
	}
	expiredQs := t.wikiStore.ExpireStaleOpenQuestions(time.Now(), wiki.OpenQuestionExpireAfterDays)
	t.logger.Info("wiki-review cycle completed",
		"touched", len(touched), "suspects", suspectCount, "merged", merged,
		"autoMerge", t.autoMerge, "logSectionsRotated", rotated,
		"dormantFlagged", len(dormant), "mailRefiled", refiled,
		"questionsExpired", expiredQs)
	return nil
}

// reviewDuplicates runs the LLM-backed near-duplicate pass over the recently
// touched pages. It is the ONLY LLM-using part of the cycle and is deliberately
// self-contained: every early exit (no touched pages, no candidates, verdict
// error) returns quietly so the caller's deterministic maintenance sweep still
// runs. Returns (merges applied, suspect count) for the cycle summary. The flat
// layout repair inside gatherSuspects still happens whenever pages were touched.
func (t *wikiReviewTask) reviewDuplicates(ctx context.Context, touched []string, state *wikiReviewState) (merged, suspects int) {
	if len(touched) == 0 {
		return 0, 0
	}
	// gatherSuspects also does the deterministic flat-layout repair (a blind RPC
	// write of 프로젝트/<name>.md routes onto its 대표.md slot) — no LLM involved.
	found := t.gatherSuspects(ctx, touched)
	if len(found) == 0 {
		t.logger.Info("wiki-review: no duplicate candidates among touched pages", "touched", len(touched))
		return 0, 0
	}
	verdicts, err := t.judge(ctx, found)
	if err != nil {
		t.logger.Warn("wiki-review: verdict call failed (skipping duplicate review)", "error", err)
		return 0, len(found) // fail-open — maintenance still runs
	}
	return t.applyVerdicts(ctx, found, verdicts, state), len(found)
}

// rotateProjectLogs keeps every project's 로그.md bounded: sections beyond the
// newest wiki.LogKeepSections move to the project's 로그-보관.md (archived, so
// search demotes it). Deterministic, no LLM. Returns sections moved.
func (t *wikiReviewTask) rotateProjectLogs() int {
	moved := 0
	for _, ref := range t.wikiStore.KnownProjects() {
		name, ok := wiki.ProjectNameOf(ref.Path)
		if !ok {
			continue
		}
		n, err := t.wikiStore.RotateProjectLog(name)
		if err != nil {
			t.logger.Warn("wiki-review: log rotation failed", "project", name, "error", err)
			continue
		}
		moved += n
	}
	return moved
}

// wikiLogEntryRe matches an audit-log section header: "## [2026-07-02 15:04] op".
var wikiLogEntryRe = regexp.MustCompile(`^## \[(\d{4}-\d{2}-\d{2} \d{2}:\d{2})\] (\S+)$`)

// recentlyTouchedPages parses the wiki audit log for pages created/updated
// since the given time, newest first, deduped, capped at wikiReviewMaxPages.
// Raw-data pages (메일분석/거래 — deterministic writers) are excluded.
func (t *wikiReviewTask) recentlyTouchedPages(since time.Time) []string {
	data, err := os.ReadFile(filepath.Join(t.wikiStore.Dir(), "log.md"))
	if err != nil {
		if !os.IsNotExist(err) { // a fresh wiki has no audit log yet — that's not a failure
			t.logger.Warn("wiki-review: audit log unreadable, skipping duplicate review", "error", err)
		}
		return nil
	}
	// Minute-precision timestamps: pull the window back one minute so an entry
	// sharing the last pass's minute isn't lost (re-review is idempotent).
	since = since.Add(-time.Minute)

	type entry struct {
		ts   time.Time
		path string
	}
	var entries []entry
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		m := wikiLogEntryRe.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil || (m[2] != "create" && m[2] != "update") {
			continue
		}
		ts, perr := time.ParseInLocation("2006-01-02 15:04", m[1], time.Local)
		if perr != nil || ts.Before(since) || i+1 >= len(lines) {
			continue
		}
		detail := strings.TrimSpace(lines[i+1])
		path := detail
		if cut := strings.Index(detail, " — "); cut >= 0 {
			path = detail[:cut]
		}
		path = strings.TrimSpace(path)
		if path == "" || !strings.HasSuffix(path, ".md") {
			continue
		}
		if wiki.IsProjectRawDataPath(path) || wiki.IsMailAnalysisPath(path) || wiki.IsProjectLogPage(path) {
			continue
		}
		entries = append(entries, entry{ts: ts, path: path})
	}
	// Newest first, dedup by path, cap. The high-water mark advances past the
	// whole window regardless, so pages beyond the cap are dropped from review
	// (accepted tradeoff: re-queueing them forever would starve fresh writes) —
	// but that overflow must at least be visible.
	seen := make(map[string]bool, len(entries))
	var out []string
	overflow := 0
	for i := len(entries) - 1; i >= 0; i-- {
		p := entries[i].path
		if seen[p] {
			continue
		}
		seen[p] = true
		if len(out) >= wikiReviewMaxPages {
			overflow++
			continue
		}
		out = append(out, p)
	}
	if overflow > 0 {
		t.logger.Info("wiki-review: touched pages exceed the per-cycle cap; oldest skipped unreviewed",
			"cap", wikiReviewMaxPages, "skipped", overflow)
	}
	return out
}

// gatherSuspects reads each touched page, repairs flat-layout strays, and
// collects near-match candidates. Same-project pages are never candidates —
// 대표.md/로그.md/detail pages of one project are intentional slots, not dups.
func (t *wikiReviewTask) gatherSuspects(ctx context.Context, touched []string) []wikiReviewSuspect {
	var suspects []wikiReviewSuspect
	for _, p := range touched {
		// Layout repair: a flat project page routes onto its 대표.md slot. When
		// the slot ALREADY exists the move fails — and the flat remnant would
		// then be invisible forever (the same-folder filter below hides its own
		// rep from the duplicate candidates), so fold it into the slot instead:
		// both are BY CONSTRUCTION the same project's rep, making this a
		// deterministic layout repair (content preserved under a merge marker),
		// not an LLM judgment gated by autoMerge.
		if np := wiki.NormalizeProjectPagePath(p); np != p {
			if _, perr := t.wikiStore.ReadPage(p); perr != nil {
				p = np // flat form already gone (moved/folded earlier) — review the slot
			} else if _, rerr := t.wikiStore.ReadPage(np); rerr == nil {
				if err := t.wikiStore.FoldDuplicate(np, p); err != nil {
					t.logger.Warn("wiki-review: flat project remnant fold failed",
						"flat", p, "slot", np, "error", err)
					continue // unreadable pair — retried next time it's touched
				}
				t.logger.Info("wiki-review: flat project remnant folded into layout slot",
					"from", p, "to", np)
				p = np
			} else if err := t.wikiStore.MovePage(p, np); err == nil {
				t.logger.Info("wiki-review: flat project page moved to layout slot", "from", p, "to", np)
				p = np
			} else {
				t.logger.Warn("wiki-review: flat project page move failed",
					"from", p, "to", np, "error", err)
			}
		}
		page, err := t.wikiStore.ReadPage(p)
		if err != nil || page == nil {
			continue // deleted/moved since the log entry
		}
		category, _, _ := strings.Cut(p, "/")
		hits := t.wikiStore.FindSimilarPages(ctx, wiki.SimilarQuery{
			Path:     p,
			ID:       page.Meta.ID,
			Code:     page.Meta.Code,
			Title:    page.Meta.Title,
			Category: category,
		}, 3)
		selfFolder, selfIsProject := wiki.ProjectFolderOf(p)
		kept := hits[:0]
		for _, h := range hits {
			if selfIsProject {
				if f, ok := wiki.ProjectFolderOf(h.Path); ok && f == selfFolder {
					continue // same project's own slots/details
				}
			}
			kept = append(kept, h)
		}
		if len(kept) == 0 {
			continue
		}
		suspects = append(suspects, wikiReviewSuspect{
			path:       p,
			title:      strings.TrimSpace(page.Meta.Title),
			summary:    strings.TrimSpace(page.Meta.Summary),
			candidates: kept,
		})
	}
	return suspects
}

// judge runs the single lightweight JSON verdict call over all suspects.
func (t *wikiReviewTask) judge(ctx context.Context, suspects []wikiReviewSuspect) ([]wikiReviewVerdict, error) {
	var b strings.Builder
	b.WriteString(`다음은 위키에 최근 기록된 문서와, 같은 주제일 가능성이 있는 기존 문서 후보입니다.
각 문서가 후보 중 하나와 **같은 대상/주제를 다루는 중복**인지 판정하세요.

주의:
- 한 프로젝트 폴더의 대표.md(개요)·로그.md(진행 이력)·상세 문서는 역할이 다른 문서이지 중복이 아닙니다.
- 같은 거래처의 서로 다른 프로젝트/거래 건은 중복이 아닙니다.
- confidence가 high면 자동 병합되므로, 정말 같은 대상일 때만 high를 쓰세요.

`)
	for i, s := range suspects {
		fmt.Fprintf(&b, "[%d] 문서: %s | %s | %s\n", i+1, s.path, s.title, s.summary)
		for _, c := range s.candidates {
			fmt.Fprintf(&b, "    후보: %s | %s | %s\n", c.Path, c.Title, c.Summary)
		}
	}
	b.WriteString(`
출력 (JSON 배열만, 다른 텍스트 없이):
[{"page":"<문서 경로>","duplicate_of":"<중복인 후보 경로, 아니면 빈 문자열>","confidence":"high|medium|low"}]`)

	jctx, cancel := context.WithTimeout(ctx, wikiReviewLLMTimeout)
	defer cancel()
	resp, err := t.llm(jctx, "You deduplicate wiki pages. Respond only with a JSON array.", b.String(), 1024)
	if err != nil {
		return nil, err
	}
	verdicts, err := jsonutil.UnmarshalLLMArray[wikiReviewVerdict](resp)
	if err != nil {
		return nil, fmt.Errorf("parse verdicts: %w (raw: %.200s)", err, resp)
	}
	return verdicts, nil
}

// applyVerdicts acts on high-confidence duplicate verdicts. In observe mode
// (autoMerge off — the rollout default) each would-merge is logged and recorded
// in the state file's audit trail; when armed, duplicates are folded, capped,
// with a git snapshot before the first destructive action. A verdict may only
// name a candidate the gather step actually offered — anything else is ignored
// (LLM hallucination).
func (t *wikiReviewTask) applyVerdicts(ctx context.Context, suspects []wikiReviewSuspect, verdicts []wikiReviewVerdict, state *wikiReviewState) int {
	offered := make(map[string]map[string]bool, len(suspects))
	for _, s := range suspects {
		set := make(map[string]bool, len(s.candidates))
		for _, c := range s.candidates {
			set[c.Path] = true
		}
		offered[s.path] = set
	}

	merged := 0
	observed := 0
	snapshotted := false
	for _, v := range verdicts {
		if merged >= wikiReviewMaxMerges {
			t.logger.Info("wiki-review: merge cap reached, deferring the rest", "cap", wikiReviewMaxMerges)
			break
		}
		page := strings.TrimSpace(v.Page)
		dup := strings.TrimSpace(v.DuplicateOf)
		if page == "" || dup == "" || !strings.EqualFold(strings.TrimSpace(v.Confidence), "high") {
			continue
		}
		if set, ok := offered[page]; !ok || !set[dup] {
			continue // not a pair we offered — never act on invented paths
		}
		if !t.autoMerge {
			// Observe mode: record the would-merge for the operator's audit.
			t.logger.Info("wiki-review: duplicate confirmed (observe mode — no merge)",
				"page", page, "duplicateOf", dup)
			appendObserved(state, fmt.Sprintf("%s | %s ≒ %s", time.Now().Format("2006-01-02 15:04"), page, dup))
			observed++
			continue
		}
		if !snapshotted {
			t.wikiStore.SnapshotGit(ctx, "wiki-review: pre-merge snapshot")
			snapshotted = true
		}
		keep, fold := t.wikiStore.ChooseDuplicateKeeper(dup, page)
		if err := t.wikiStore.FoldDuplicate(keep, fold); err != nil {
			t.logger.Warn("wiki-review: merge failed", "keep", keep, "fold", fold, "error", err)
			continue
		}
		t.logger.Info("wiki-review: duplicate merged", "keep", keep, "fold", fold)
		merged++
	}
	if merged > 0 {
		t.wikiStore.SnapshotGit(ctx, fmt.Sprintf("wiki-review: %d duplicate(s) merged", merged))
	}
	if observed > 0 {
		if over := len(state.Observed) - wikiReviewObservedCap; over > 0 {
			state.Observed = state.Observed[over:]
		}
		if err := t.saveState(state); err != nil {
			t.logger.Warn("wiki-review: failed to persist observed verdicts", "error", err)
		}
	}
	return merged
}

// appendObserved records an observe-mode line once. The payload after " | "
// is the identity (mail-domain PATH → PROJECT, or PAGE ≒ DUP) so the same
// four SunKean/Barocorp proposals cannot refill the 100-line cap every 2h.
func appendObserved(state *wikiReviewState, line string) {
	if state == nil {
		return
	}
	key := observedPayload(line)
	for _, existing := range state.Observed {
		if observedPayload(existing) == key {
			return
		}
	}
	state.Observed = append(state.Observed, line)
}

func observedPayload(line string) string {
	if i := strings.Index(line, " | "); i >= 0 {
		return line[i+3:]
	}
	return line
}

func (t *wikiReviewTask) loadState() *wikiReviewState {
	st := &wikiReviewState{Version: 1}
	data, err := os.ReadFile(t.statePath)
	if err != nil {
		return st
	}
	if err := json.Unmarshal(data, st); err != nil {
		t.logger.Warn("wiki-review: corrupt state, starting fresh", "error", err)
		return &wikiReviewState{Version: 1}
	}
	return st
}

func (t *wikiReviewTask) saveState(st *wikiReviewState) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.WriteFile(t.statePath, data, &atomicfile.Options{Perm: 0o600})
}

// registerWikiReviewTask wires the post-write duplicate reviewer into the
// autonomous service. Production state dir only — a dev/live-test gateway must
// not mutate the shared curated wiki (mirrors registerWikiResearchTask's gate).

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
// deterministic gather → one lightweight JSON verdict → deterministic apply.
// Model role: lightweight (internal background judgment; see
// .claude/rules/model-roles.md). Fail-open: any error logs and skips the cycle.
package server

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

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/monitoring"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/pilot"
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

type wikiReviewState struct {
	Version      int   `json:"version"`
	LastReviewMs int64 `json:"lastReviewMs"`
}

// wikiReviewTask implements autonomous.PeriodicTask.
type wikiReviewTask struct {
	wikiStore *wiki.Store
	activity  *monitoring.ActivityTracker
	logger    *slog.Logger
	statePath string
	// llm is the verdict call, injectable for tests. Defaults to the lightweight
	// local role (pilot.CallLocalLLM) — bounded JSON judgment, no tool calls.
	llm func(ctx context.Context, system, user string, maxTokens int) (string, error)
}

func (t *wikiReviewTask) Name() string            { return "wiki-review" }
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
	if len(touched) == 0 {
		return nil
	}

	// Deterministic layout repair first: a flat 프로젝트/<name>.md (from a blind
	// RPC write) routes onto its 대표.md slot. No LLM involved.
	suspects := t.gatherSuspects(ctx, touched)
	if len(suspects) == 0 {
		t.logger.Info("wiki-review: no duplicate candidates among touched pages", "touched", len(touched))
		return nil
	}

	verdicts, err := t.judge(ctx, suspects)
	if err != nil {
		t.logger.Warn("wiki-review: verdict call failed (skipping cycle)", "error", err)
		return nil // fail-open
	}
	merged := t.applyVerdicts(ctx, suspects, verdicts)
	t.logger.Info("wiki-review cycle completed",
		"touched", len(touched), "suspects", len(suspects), "merged", merged)
	return nil
}

// wikiLogEntryRe matches an audit-log section header: "## [2026-07-02 15:04] op".
var wikiLogEntryRe = regexp.MustCompile(`^## \[(\d{4}-\d{2}-\d{2} \d{2}:\d{2})\] (\S+)$`)

// recentlyTouchedPages parses the wiki audit log for pages created/updated
// since the given time, newest first, deduped, capped at wikiReviewMaxPages.
// Raw-data pages (메일분석/거래 — deterministic writers) are excluded.
func (t *wikiReviewTask) recentlyTouchedPages(since time.Time) []string {
	data, err := os.ReadFile(filepath.Join(t.wikiStore.Dir(), "log.md"))
	if err != nil {
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
		if wiki.IsProjectRawDataPath(path) || wiki.IsMailAnalysisPath(path) {
			continue
		}
		entries = append(entries, entry{ts: ts, path: path})
	}
	// Newest first, dedup by path, cap.
	seen := make(map[string]bool, len(entries))
	var out []string
	for i := len(entries) - 1; i >= 0 && len(out) < wikiReviewMaxPages; i-- {
		p := entries[i].path
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// gatherSuspects reads each touched page, repairs flat-layout strays, and
// collects near-match candidates. Same-project pages are never candidates —
// 대표.md/로그.md/detail pages of one project are intentional slots, not dups.
func (t *wikiReviewTask) gatherSuspects(ctx context.Context, touched []string) []wikiReviewSuspect {
	var suspects []wikiReviewSuspect
	for _, p := range touched {
		// Layout repair: a flat project page routes onto its 대표.md slot.
		if np := wiki.NormalizeProjectPagePath(p); np != p {
			if err := t.wikiStore.MovePage(p, np); err == nil {
				t.logger.Info("wiki-review: flat project page moved to layout slot", "from", p, "to", np)
				p = np
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

// applyVerdicts folds high-confidence duplicates, capped, with a git snapshot
// before the first destructive action. A verdict may only name a candidate the
// gather step actually offered — anything else is ignored (LLM hallucination).
func (t *wikiReviewTask) applyVerdicts(ctx context.Context, suspects []wikiReviewSuspect, verdicts []wikiReviewVerdict) int {
	offered := make(map[string]map[string]bool, len(suspects))
	for _, s := range suspects {
		set := make(map[string]bool, len(s.candidates))
		for _, c := range s.candidates {
			set[c.Path] = true
		}
		offered[s.path] = set
	}

	merged := 0
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
	return merged
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
func (s *Server) registerWikiReviewTask(homeDir string) {
	if s.wikiStore == nil {
		return
	}
	if os.Getenv("DENEB_WIKI_REVIEW_DISABLE") == "1" {
		s.logger.Info("wiki-review disabled via DENEB_WIKI_REVIEW_DISABLE")
		return
	}
	stateDir, ok := s.productionStateDir(homeDir)
	if !ok {
		return
	}
	s.autonomousSvc.RegisterTask(&wikiReviewTask{
		wikiStore: s.wikiStore,
		activity:  s.activity,
		logger:    s.logger,
		statePath: filepath.Join(stateDir, wikiReviewStateFile),
		llm: func(ctx context.Context, system, user string, maxTokens int) (string, error) {
			return pilot.CallLocalLLM(ctx, system, user, maxTokens, map[string]any{"temperature": 0})
		},
	})
	s.logger.Info("wiki-review task registered", "interval", wikiReviewInterval.String())
}

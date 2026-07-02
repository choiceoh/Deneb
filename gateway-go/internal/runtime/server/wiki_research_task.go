// wiki_research_task.go — autonomous deep-research refresh of project wiki pages.
//
// Every 6h, this task picks one project (프로젝트) wiki page and runs a single
// agent turn that re-investigates the page's subject from Deneb's *own*
// accumulated knowledge — mail archive, conversation recall (polaris), the
// knowledge graph (graphify), contacts, and cross-linked wiki pages — then
// updates the page in place when it finds genuinely new facts.
//
// This is the active, page-driven counterpart to the dreamer (wiki/dreamer.go):
// the dreamer passively consolidates diary/MEMORY.md into the wiki, whereas this
// task takes an existing important page and actively searches every internal
// source for what changed since it was last written. No web access — the
// wiki-research preset (toolpreset) drops the web tool, so nothing external is
// called and no web-sourced text can pollute the curated memory.
//
// Selection is round-robin: pages are ordered by how long ago this task last
// refreshed them (never-refreshed first), so one cycle per 6h walks the whole
// project set over time without re-doing the same page. State lives beside the
// other autonomous state files (~/.deneb/wiki-research-state.json).
//
// Like the daily memory backup, it is registered only for the production state
// dir — a dev/live-test gateway must not mutate the shared curated wiki.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/monitoring"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolpreset"
	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
)

// Compile-time interface compliance.
var _ autonomous.PeriodicTask = (*wikiResearchTask)(nil)

const (
	// wikiResearchCategory is the only category this task refreshes. Project
	// pages (deals, decisions, milestones) are exactly the ones whose internal
	// signal — new mail, new conversations — keeps accumulating between cycles.
	wikiResearchCategory = "프로젝트"
	// wikiResearchInterval is the cadence. One page per cycle bounds cost.
	wikiResearchInterval = 6 * time.Hour
	// wikiResearchTurnTimeout caps a single research turn. The chat pipeline's
	// own turn deadline may cap it shorter; this is the outer bound so a stuck
	// turn never wedges the cycle.
	wikiResearchTurnTimeout = 6 * time.Minute
	// wikiResearchMaxBackfill is how many SKELETON 대표페이지 (layout-migration
	// mints, wiki.RepSkeletonMarker) one cycle may fill. Normal cycles stay at
	// one page; the burst applies only while empty rep pages remain, so the
	// post-migration fleet (~39) fills in days instead of weeks.
	wikiResearchMaxBackfill = 3
	// wikiResearchStateFile holds the per-page last-refreshed timestamps used
	// for round-robin selection.
	wikiResearchStateFile = "wiki-research-state.json"
	// wikiResearchSessionKey isolates these background turns from user sessions.
	wikiResearchSessionKey = "wiki-research"
)

// wikiResearchState persists round-robin progress: relPath -> last-refreshed
// unix millis. Pages absent from the map have never been refreshed and sort
// first.
type wikiResearchState struct {
	Version    int              `json:"version"`
	Researched map[string]int64 `json:"researched"`
}

// wikiResearchTask implements autonomous.PeriodicTask.
type wikiResearchTask struct {
	chatHandler *chat.Handler
	wikiStore   *wiki.Store
	activity    *monitoring.ActivityTracker
	logger      *slog.Logger
	statePath   string
}

func (t *wikiResearchTask) Name() string            { return "wiki-research" }
func (t *wikiResearchTask) Interval() time.Duration { return wikiResearchInterval }

func (t *wikiResearchTask) Run(ctx context.Context) error {
	if t.chatHandler == nil || t.wikiStore == nil {
		return fmt.Errorf("wiki-research: chat handler or wiki store not available")
	}

	// One page per normal cycle; while layout-migration SKELETON rep pages
	// remain, keep going up to the backfill burst so the empty fleet fills in
	// days. The user-activity gate re-checks between turns.
	var firstErr error
	for i := 0; i < wikiResearchMaxBackfill; i++ {
		target, ranSkeleton, err := t.runOne(ctx)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		if target == "" || !ranSkeleton {
			break
		}
	}
	return firstErr
}

// runOne selects and researches a single page. Returns the target path ("" when
// nothing ran), whether the target was a skeleton 대표페이지, and the turn error.
func (t *wikiResearchTask) runOne(ctx context.Context) (string, bool, error) {
	// Defer to the user: a research turn runs the main model and reads the
	// memory stores, competing with interactive turns for the local GPU. If the
	// user is active, skip this cycle — round-robin still advances next time.
	if t.activity != nil {
		idle := time.Duration(time.Now().UnixMilli()-t.activity.LastActivityAt()) * time.Millisecond
		if idle < 5*time.Minute {
			t.logger.Info("wiki-research: skipped, user active", "idle", idle.Round(time.Second))
			return "", false, nil
		}
	}

	state := t.loadState()
	target := t.selectTarget(state)
	if target == nil {
		t.logger.Debug("wiki-research: no eligible project pages")
		return "", false, nil
	}

	runCtx, cancel := context.WithTimeout(ctx, wikiResearchTurnTimeout)
	defer cancel()

	prompt := t.buildPrompt(target)
	result, err := t.chatHandler.SendSync(runCtx, wikiResearchSessionKey, prompt, "", &chat.SyncOptions{
		ToolPreset:       string(toolpreset.PresetWikiResearch),
		MaxHistoryTokens: 20_000,
		// Background maintenance turn: it researches from the memory stores via
		// tools, not from its own past research turns. Keep it ephemeral so the
		// "wiki-research" session never grows an unbounded transcript (the trap
		// that stalled the boot session — see boot_task.go).
		EphemeralUser:      true,
		EphemeralAssistant: true,
		// The prompt already names the page and orders the research; the recall
		// preflight would only re-inject the same wiki context.
		SkipRecall: true,
	})
	// Advance the round-robin pointer regardless of the turn outcome — recording
	// the attempt even on error is what keeps a "poison page" (one whose turn
	// reliably fails: an oversized body that blows the turn timeout, a tool that
	// trips on its content) from being re-selected every cycle and starving every
	// other project page. selectTarget orders by least-recently-attempted, so a
	// failed page simply comes back around on the next full rotation instead of
	// wedging selection. A successful "found nothing new" turn advances the same
	// way.
	state.Researched[target.path] = time.Now().UnixMilli()
	if serr := t.saveState(state); serr != nil {
		t.logger.Warn("wiki-research: failed to persist state", "error", serr)
	}

	if err != nil {
		return target.path, target.skeleton, fmt.Errorf("wiki-research: agent turn failed for %s: %w", target.path, err)
	}

	// Commit the wiki dir so this cycle's write (if any) is an isolated,
	// revertible point in the wiki git history, alongside the dream-cycle and
	// daily-backup snapshots. Only after a successful turn — a failed turn made
	// no wiki change.
	t.wikiStore.SnapshotGit(ctx, fmt.Sprintf("wiki-research: %s", target.path))

	t.logger.Info(
		"wiki-research cycle completed",
		"page", target.path,
		"title", target.title,
		"skeleton", target.skeleton,
		"output_len", len(result.Text),
	)
	return target.path, target.skeleton, nil
}

// wikiResearchCandidate is a project page eligible for refresh.
type wikiResearchCandidate struct {
	path       string
	title      string
	summary    string
	updated    string // YYYY-MM-DD, the page's last content update
	importance float64
	lastRun    int64 // unix millis this task last refreshed it; 0 = never
	skeleton   bool  // layout-migration mint (wiki.RepSkeletonMarker) awaiting backfill
}

// selectTarget picks the project page most overdue for a refresh: never-refreshed
// pages first, then stalest content, then highest importance. Archived,
// superseded, and empty pages are skipped.
func (t *wikiResearchTask) selectTarget(state *wikiResearchState) *wikiResearchCandidate {
	paths, err := t.wikiStore.ListPages(wikiResearchCategory)
	if err != nil {
		t.logger.Warn("wiki-research: failed to list project pages", "error", err)
		return nil
	}

	var cands []wikiResearchCandidate
	for _, p := range paths {
		// Research refreshes project 대표페이지 only — raw mail-analysis pages, deal
		// ledger entries, and per-project sub-pages are inputs to research, not
		// research targets (re-researching a raw mail page just re-creates it).
		if !wiki.IsProjectRepPage(p) {
			continue
		}
		page, err := t.wikiStore.ReadPage(p)
		if err != nil || page == nil {
			continue
		}
		if page.Meta.Archived || page.Meta.SupersededBy != "" {
			continue
		}
		if strings.TrimSpace(page.Body) == "" {
			continue
		}
		cands = append(cands, wikiResearchCandidate{
			path:       p,
			title:      page.Meta.Title,
			summary:    page.Meta.Summary,
			updated:    page.Meta.Updated,
			importance: page.Meta.Importance,
			lastRun:    state.Researched[p],
			skeleton:   strings.Contains(page.Body, wiki.RepSkeletonMarker),
		})
	}
	if len(cands) == 0 {
		return nil
	}

	sort.Slice(cands, func(i, j int) bool {
		if cands[i].skeleton != cands[j].skeleton {
			return cands[i].skeleton // empty migration mints first — they carry no facts yet
		}
		if cands[i].lastRun != cands[j].lastRun {
			return cands[i].lastRun < cands[j].lastRun // never/least-recently refreshed first
		}
		if cands[i].updated != cands[j].updated {
			return cands[i].updated < cands[j].updated // stalest content first
		}
		return cands[i].importance > cands[j].importance // most important first
	})
	return &cands[0]
}

// buildPrompt instructs an internal-only deep-research refresh of one page.
func (t *wikiResearchTask) buildPrompt(c *wikiResearchCandidate) string {
	var b strings.Builder
	b.WriteString("[자율 위키 리서치 — 백그라운드 유지보수 턴]\n\n")
	b.WriteString(fmt.Sprintf("대상 프로젝트 위키 페이지: %s\n", c.path))
	if c.title != "" {
		b.WriteString(fmt.Sprintf("제목: %s\n", c.title))
	}
	if c.summary != "" {
		b.WriteString(fmt.Sprintf("현재 요약: %s\n", c.summary))
	}
	if c.updated != "" {
		b.WriteString(fmt.Sprintf("마지막 갱신일: %s\n", c.updated))
	}
	if c.skeleton {
		b.WriteString("주의: 이 페이지는 레이아웃 이관으로 만든 빈 스켈레톤입니다. 같은 폴더의 하위 문서(로그·메일분석·상세)와 내부 소스를 종합해 요약·핵심 사실을 처음부터 채우세요.\n")
	}
	b.WriteString(`
이 페이지의 주제에 대해 내부 소스만으로 심층 리서치를 수행해 페이지를 최신화하세요. 외부 웹 검색은 하지 않습니다 (도구에 없음).

1. 먼저 wiki(action=read)로 대상 페이지 본문 전체와 관련(related) 페이지를 읽어 현재 내용을 파악합니다.
2. 다음 내부 소스에서 마지막 갱신일 이후의 새 정보를 찾습니다:
   - mail_archive: 이 프로젝트 관련 새 메일/스레드
   - polaris: 관련된 최근 대화/회상
   - graphify, knowledge, contacts: 연결된 인물·조직·사실
3. 진짜로 새롭거나 바뀐 사실이 있으면 wiki(action=write)로 본문에 반영합니다:
   - 새 사실을 본문에 통합하고 Updated를 오늘로 갱신
   - 기존 사실과 모순되면 supersedes로 옛 내용을 대체 처리
   - 출처 신뢰도에 맞게 confidence 설정, importance는 유지
   - **새 페이지를 만들지 마세요.** 대상 페이지(대표.md)를 갱신하고, 시간순 진행 이력은 같은 폴더의 로그.md에 append합니다
4. 새로운 내용이 없으면 페이지를 건드리지 말고 조용히 종료합니다. 형식만 바꾸는 불필요한 재작성 금지.

이것은 사용자에게 보내는 응답이 아니라 백그라운드 메모리 유지보수 작업입니다. 사용자에게 알리지 마세요.`)
	return b.String()
}

func (t *wikiResearchTask) loadState() *wikiResearchState {
	st := &wikiResearchState{Version: 1, Researched: map[string]int64{}}
	data, err := os.ReadFile(t.statePath)
	if err != nil {
		return st // missing/unreadable → fresh state
	}
	if err := json.Unmarshal(data, st); err != nil {
		t.logger.Warn("wiki-research: corrupt state, starting fresh", "error", err)
		return &wikiResearchState{Version: 1, Researched: map[string]int64{}}
	}
	if st.Researched == nil {
		st.Researched = map[string]int64{}
	}
	return st
}

func (t *wikiResearchTask) saveState(st *wikiResearchState) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	// Shared flock + unique-tmp + atomic-rename helper instead of a hand-rolled
	// fixed-".tmp" write, matching the repo's other JSON state files.
	return atomicfile.WriteFile(t.statePath, data, &atomicfile.Options{Perm: 0o600})
}

// registerWikiResearchTask wires the 6h project-wiki refresh into the autonomous
// service. Production state dir only — a dev/live-test gateway must not mutate
// the shared curated wiki (mirrors registerMemoryBackupTask's gate).
func (s *Server) registerWikiResearchTask(homeDir string) {
	if s.chatHandler == nil || s.wikiStore == nil {
		return
	}
	if os.Getenv("DENEB_WIKI_RESEARCH_DISABLE") == "1" {
		s.logger.Info("wiki-research disabled via DENEB_WIKI_RESEARCH_DISABLE")
		return
	}
	// Production state dir only — a dev/live-test gateway must not mutate the
	// shared curated wiki (same gate as the offsite memory backup).
	stateDir, ok := s.productionStateDir(homeDir)
	if !ok {
		return
	}
	s.autonomousSvc.RegisterTask(&wikiResearchTask{
		chatHandler: s.chatHandler,
		wikiStore:   s.wikiStore,
		activity:    s.activity,
		logger:      s.logger,
		statePath:   filepath.Join(stateDir, wikiResearchStateFile),
	})
	s.logger.Info("wiki-research task registered", "interval", wikiResearchInterval.String())
}

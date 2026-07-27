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
// The research turn itself follows a within-run epistemic working-memory
// protocol (SLEUTH, arXiv:2607.12267): the agent re-emits a facts/hypotheses/
// open-questions block at the head of every response so multi-hop findings
// survive context dilution and the 20k-token history cap, then folds the block
// into the page's 현재 상태 / ## 미해결 질문 sections — the cross-run half of
// the same epistemic state.
//
// Like the daily memory backup, it is registered only for the production state
// dir — a dev/live-test gateway must not mutate the shared curated wiki.
package wikiwork

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/monitoring"
	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/toolpreset"
	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
)

// Compile-time interface compliance.
var _ autonomous.PeriodicTask = (*wikiResearchTask)(nil)

const (
	// wikiResearchCategory is the only category this task refreshes. Project
	// pages (deals, decisions, milestones) are exactly the ones whose internal
	// signal — new mail, new conversations — keeps accumulating between cycles.
	wikiResearchCategory = "프로젝트"
	// wikiResearchDemandTerms caps how many demanded terms steer selection —
	// enough to cover a working week's unanswered questions without letting one
	// noisy day match every page.
	wikiResearchDemandTerms = 20
	// wikiResearchInterval is the cadence. One page per cycle bounds cost.
	wikiResearchInterval = 6 * time.Hour
	// wikiResearchTurnTimeout caps a single research turn. The chat pipeline's
	// own turn deadline may cap it shorter; this is the outer bound so a stuck
	// turn never wedges the cycle. 6m proved too tight in prod (2026-07-06: a
	// legitimate 15-turn research run was killed at 363s, stop=timeout — the
	// cycle's work lost); 12m still bounds a wedged turn to one cycle slot.
	wikiResearchTurnTimeout = 12 * time.Minute
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

const (
	// ResearchStateFile is the round-robin refresh state filename.
	ResearchStateFile = wikiResearchStateFile
	// ResearchInterval is the autonomous refresh cadence.
	ResearchInterval = wikiResearchInterval
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
	chatHandler chatport.SyncRunner
	wikiStore   *wiki.Store
	activity    *monitoring.ActivityTracker
	logger      *slog.Logger
	statePath   string
	// workspaceDir locates the operator wiki brief (WIKI.md — wiki.LoadWikiBrief).
	// Empty disables brief injection.
	workspaceDir string
	// postCycleScout, when set, fires after each successful research turn with
	// the refreshed page path. The wiki-scout task hooks in here so a question
	// the internal pass just wrote down ("searched everything internal, no
	// answer") goes external immediately instead of waiting out the scheduled
	// scout cycle. nil = no immediate trigger.
	postCycleScout func(ctx context.Context, repPath string)
	// maintMu is the shared wiki-maintenance lock (scout.MaintenanceLock).
	// Held around this task's mutating turn so a scheduled scout turn can't
	// concurrently rewrite the same rep page. Released BEFORE postCycleScout
	// fires (the scout re-acquires it itself). nil disables the guard.
	maintMu *sync.Mutex
}

// ResearchTask refreshes project wiki pages from Deneb's internal sources.
type ResearchTask = wikiResearchTask

// NewResearchTask constructs the autonomous project-wiki research worker.
func NewResearchTask(
	chatHandler chatport.SyncRunner,
	wikiStore *wiki.Store,
	activity *monitoring.ActivityTracker,
	logger *slog.Logger,
	statePath string,
	workspaceDir string,
) *ResearchTask {
	return &wikiResearchTask{
		chatHandler:  chatHandler,
		wikiStore:    wikiStore,
		activity:     activity,
		logger:       logger,
		statePath:    statePath,
		workspaceDir: workspaceDir,
	}
}

// SetPostCycleScout wires the immediate external-scout trigger (see the
// postCycleScout field). Called once at registration, before the task runs.
func (t *wikiResearchTask) SetPostCycleScout(fn func(ctx context.Context, repPath string)) {
	t.postCycleScout = fn
}

// SetMaintenanceLock shares the wiki-maintenance lock with the scout so their
// mutating turns can't overlap. Called once at registration.
func (t *wikiResearchTask) SetMaintenanceLock(mu *sync.Mutex) {
	t.maintMu = mu
}

// Name returns the component's stable scheduler name.
func (t *wikiResearchTask) Name() string { return "wiki-research" }

// Interval returns the component's scheduling cadence.
func (t *wikiResearchTask) Interval() time.Duration { return wikiResearchInterval }

// Run executes one scheduled task cycle.
func (t *wikiResearchTask) Run(ctx context.Context) error {
	if t.chatHandler == nil || !t.chatHandler.ChatReady() || t.wikiStore == nil {
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

	// Serialize this mutating turn against the scout (shared lock). Acquired
	// after target selection so a no-op selection never holds it; released
	// before the post-cycle scout trigger, which re-acquires it itself.
	maintHeld := false
	if t.maintMu != nil {
		if !t.maintMu.TryLock() {
			t.logger.Info("wiki-research: skipped, wiki maintenance lock busy")
			return "", false, nil
		}
		maintHeld = true
	}
	releaseMaint := func() {
		if maintHeld {
			t.maintMu.Unlock()
			maintHeld = false
		}
	}
	defer releaseMaint() // backstop; the success path releases earlier

	runCtx, cancel := context.WithTimeout(ctx, wikiResearchTurnTimeout)
	defer cancel()

	prompt := t.buildPrompt(target)
	result, err := t.chatHandler.RunSync(runCtx, chatport.SyncRequest{
		SessionKey:       wikiResearchSessionKey,
		Message:          prompt,
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

	// Release the maintenance lock before triggering the scout — the scout's
	// runTurn re-acquires the same lock, so holding it here would deadlock.
	releaseMaint()

	// Hand the refreshed page to the external scout: it no-ops unless the
	// turn above added open questions dated today. Sequential on purpose —
	// one background lane, no goroutine fan-out for a rare bounded turn.
	if t.postCycleScout != nil {
		t.postCycleScout(ctx, target.path)
	}
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
	noStatus   bool  // 현재 상태 section empty — the 2026-07 audit's top gap (57%)
	activity   int   // raw-data pages (메일분석/자료/회의록) newer than the rep's updated: date
	demanded   bool  // the user asked about this subject and recall came back empty
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

	// Demand from the recall-miss ledger (wiki/recall_misses.go): questions the
	// user actually asked that the wiki could not answer. Round-robin staleness
	// is a proxy for "might be worth refreshing"; this is the real thing.
	demand := t.wikiStore.RecallDemandTerms(time.Now(), wikiResearchDemandTerms)

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
			noStatus:   strings.TrimSpace(page.Section("현재 상태")) == "",
			activity:   t.rawDataActivity(p, page.Meta.Updated),
			demanded:   pageMatchesDemand(page.Meta.Title, page.Meta.Summary, demand),
		})
	}
	if len(cands) == 0 {
		return nil
	}

	sort.Slice(cands, func(i, j int) bool {
		if cands[i].skeleton != cands[j].skeleton {
			return cands[i].skeleton // empty migration mints first — they carry no facts yet
		}
		if cands[i].noStatus != cands[j].noStatus {
			// Pages with no 현재 상태 next: the anchor injects the rep page on
			// every project mention, and an empty status section injects
			// nothing — filling these first is the fastest quality win
			// (2026-07-05 audit: 57% of live projects had none).
			return cands[i].noStatus
		}
		// Demand next: a page the user asked about and got nothing for is a
		// measured hole, which beats every staleness proxy below. It sits after
		// the two structural gaps (skeleton / no 현재 상태) because those pages
		// answer nothing at all yet.
		if cands[i].demanded != cands[j].demanded {
			return cands[i].demanded
		}
		// Freshness SLO (2026-07-19): a rep page whose folder keeps receiving
		// raw data (new 메일분석/자료/회의록 since its updated: date) is rotting
		// in public — its summary is what every recall anchor injects. High
		// activity jumps the round-robin; the bucket comparison (3+) keeps a
		// noisy folder from monopolizing every cycle.
		ai, aj := cands[i].activity, cands[j].activity
		if (ai >= 3) != (aj >= 3) {
			return ai >= 3
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

// pageMatchesDemand reports whether any demanded term appears in the page's
// title or summary. Substring matching on purpose: there is no Korean
// morphological analyzer here, so "완도군의" must still match a 완도 page.
// The PATH is deliberately excluded — it carries the category segment
// ("프로젝트/…"), which would make a category word match every candidate.
func pageMatchesDemand(title, summary string, demand []string) bool {
	if len(demand) == 0 {
		return false
	}
	hay := strings.ToLower(title + " " + summary)
	for _, term := range demand {
		if term != "" && strings.Contains(hay, term) {
			return true
		}
	}
	return false
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
	// Operator steering (WIKI.md): re-read every cycle so a brief edit takes
	// effect on the next research turn without a restart (see wiki/brief.go).
	b.WriteString(wiki.WikiBriefSection(wiki.LoadWikiBrief(t.workspaceDir)))
	// Within-run working-memory block (SLEUTH): re-emitted in full at the head
	// of every model response, so the newest message always carries the whole
	// investigation state even after history compaction buries early turns.
	// The commit rule is state-conditional ("questions empty/immaterial → go
	// write"), NOT budget-based — a turn-budget pre-warning was tried on the
	// main agent loop and regressed into premature give-ups (see agent/grace.go).
	b.WriteString(`
이 페이지의 주제에 대해 내부 소스만으로 심층 리서치를 수행해 페이지를 최신화하세요. 외부 웹 검색은 하지 않습니다 (도구에 없음).

작업 기억 규약 (매 응답 유지): 이 리서치는 여러 도구 호출에 걸친 멀티홉 조사라, 초기 발견이 뒤의 검색 결과에 파묻히기 쉽습니다. 도구를 호출하는 매 응답의 맨 앞에 아래 상태 블록 전체를 최신 상태로 다시 쓰고 시작하세요:
   [사실] F1. <확정 사실> (출처: [[페이지]] 또는 메일·대화 식별자) — 도구 결과로 확인된 것만 번호를 이어 누적합니다. 앞의 사실이 틀렸다고 판명되면 삭제하지 말고 "Fn. (정정: F1 무효) <바른 사실>"로 남기세요.
   [가설] H1. <페이지에 반영할 후보 변경·판단> | 지지: F# | 모순: F# — 새 사실이 나올 때마다 갱신합니다.
   [질문] Q1. <남은 불확실성> | 다음 행동: <도구와 쿼리> | 우선순위: 상/중/하 — 답이 확인된 질문은 제거합니다.
   다음 도구 호출은 항상 [질문] 중 우선순위가 가장 높은 것의 '다음 행동'입니다. [질문]이 비었거나 남은 질문의 답이 페이지 반영 내용을 바꾸지 못하면, 추가 확인 검색 없이 곧장 3의 페이지 반영으로 진행하세요.
   종료 시 접기: [사실]은 출처와 함께 본문과 "현재 상태"에 반영하고, 확정하지 못한 [가설]은 본문에 쓰지 않으며(추측 금지), 내부 소스로 답을 못 찾은 [질문] 중 프로젝트 진행에 실제로 중요한 것만 4의 규칙대로 "## 미해결 질문"에 남깁니다.

1. 먼저 wiki(action=read)로 대상 페이지 본문 전체와 관련(related) 페이지를 읽어 현재 내용을 파악합니다.
2. 다음 내부 소스에서 마지막 갱신일 이후의 새 정보를 찾습니다:
   - mail_archive: 이 프로젝트 관련 새 메일/스레드
   - polaris: 관련된 최근 대화/회상
   - graphify, knowledge, contacts: 연결된 인물·조직·사실
3. 진짜로 새롭거나 바뀐 사실이 있으면 wiki(action=write)로 본문에 반영합니다:
   - 새 사실을 본문에 통합하고 Updated를 오늘로 갱신
   - 기존 사실과 모순되면 supersedes로 옛 내용을 대체 처리
   - 출처 신뢰도에 맞게 confidence 설정, importance는 유지
   - **새 페이지를 만들지 마세요.** 위에 명시된 대상 페이지 경로를 그대로 갱신하고(레거시 flat 경로면 그 경로 그대로), 시간순 진행 이력은 그 프로젝트의 로그.md(프로젝트/<이름>/로그.md)에 append합니다
   - client(거래처): 내부 소스에서 발주처/계약 상대가 확인되는데 client가 비어 있으면 계열사 단위 정식명 1개로 기입하세요 (예: "기아", "현대차", "LG전자", "금호타이어" — 그룹명·㈜ 등 법인 접미어 금지). 이미 있으면 유지, 자체 개발 등 거래처 없는 사업이거나 불확실하면 비워둠(추측 금지)
   - sites(현장): 내부 소스에서 현장 위치가 확인되는데 sites가 비어 있으면 고정 규칙 "광역약칭 시/군 읍/면/동 [리]"(예: "전북 군산시 옥구읍 수산리")로 기입하세요. 이미 있으면 유지, 불확실하면 비워둠(추측 금지)
   - kinds(특성): 비어 있으면 2단 체계 "1차/2차"로 기입하세요 — 태양광(발전소 사업 — 시공·개발·인허가 포함; 2차: 토지/루프탑/수상/ESS — ESS 사업도 태양광), 기자재(모듈/인버터/케이블/기타), 풍력(육상/해상), 기타(용역/협력). 2차가 불명확하면 1차만, 1차만 있는 기존 값에 2차가 확인되면 세분화(예: 태양광 → 태양광/루프탑). 명백한 것만(추측 금지)
   - stage(단계): 내부 소스에서 사업 단계가 확인되면 고정 어휘 하나로 기입/갱신하세요 — 제안 → 견적 → 입찰 → 개발 → 계약협의 → 시공/납품 → 운영 (진행 순), 종결/유실 (말단). 개발=자체개발 인허가·부지 확보. 시공/납품은 병렬 트랙: 현장 사업=시공, 기자재(조달) 건의 계약 이행=납품. 진행 신호 예: 견적서 송부=견적, 입찰/적격심사=입찰, 개발행위허가·부지 계약=개발, 계약서 검토·날인·PF=계약협의, 착공·공정=시공, 선적·출하·납기 진행=납품, 준공·O&M=운영, 중단 결정·불참 종결=유실/종결. 기존 값보다 후행 단계 증거가 나오면 갱신, 불확실하면 두세요(추측 금지)
   - 현장문서 게이트: stage가 제안·견적·입찰(영업 단계)이면 대표페이지에 현장 상세 섹션(주소 목록·현장 스펙 문단)을 만들지 마세요 — sites 메타데이터 한 줄까지만. 개발·계약협의 이상 또는 기존 O&M 자산부터 상세 문서가 값어치를 합니다 (자체개발은 부지가 본체라 개발 단계부터)
   - 증거 함정(sites/client 판정 시): ①메일 서명 블록의 주소는 발신자 회사 소재지이지 현장이 아닙니다 ②견적서의 공급자란 주소는 탑솔라 본사(전남 나주시)입니다 ③"~공장 가배치" 메일의 현장은 그 공장이지 우리측 조직이 아닙니다 — 본문에서 현장으로 명시된 주소만 채택하세요
3-1. 메타데이터 검증 (기입의 반대 방향 — 오염 탐지): 페이지에 이미 기록된 client/sites/stage 값이 이번에 읽은 내부 증거와 **모순**되면, 값을 직접 고치지 말고 "## 미해결 질문"에 검증 요청 불릿을 추가하세요 (예: "- YYYY-MM-DD client가 'ZTT'로 기록돼 있으나 [[메일분석 페이지]]의 계약 상대는 남도에코 — 확인 필요"). 근거 [[페이지]] 링크를 반드시 병기합니다. client는 한 번 기록되면 시스템이 보존하는 필드라 오기입은 이 경로로만 교정됩니다. 예외: stage는 후행 단계 증거가 명백하면 3의 규칙대로 직접 갱신 (진행은 정상 변화지 모순이 아님)
4. 대상 페이지의 "## 미해결 질문" 섹션을 관리합니다:
   - 내부 소스를 다 뒤져도 답을 못 찾은, 이 프로젝트 진행에 실제로 중요한 질문이 있으면 "- YYYY-MM-DD 질문" 형식 불릿으로 추가 (섹션 전체 최대 5개, 이미 있는 질문과 중복 금지, 사소한 질문은 넣지 않음)
   - 이번 리서치에서 답이 확인된 기존 질문은 섹션에서 제거하고 답을 본문에 반영하되, 로그.md에 '## [YYYY-MM-DD] 질문해결 | <질문 요지>' 섹션으로 답과 근거([[페이지]] 링크)를 함께 남기세요 — 질문이 흔적 없이 증발하면 안 됩니다
   - 정황 변화로 더 이상 이 프로젝트 진행에 중요하지 않아진 질문은 섹션에서 제거하고 로그.md에 '## [YYYY-MM-DD] 질문폐기 | <질문 요지>' 섹션으로 폐기 사유 한 줄을 남기세요. 단지 답을 못 찾았다는 이유로는 폐기하지 마세요 — 그런 질문은 그대로 둬서 승격되게 합니다
   - 기존 불릿의 날짜는 절대 갱신하지 마세요 (오래된 날짜가 승격 신호입니다)
5. 새 사실도 없고 미해결 질문 변경도 없으면 페이지를 건드리지 말고 조용히 종료합니다. 형식만 바꾸는 불필요한 재작성 금지.

이것은 사용자에게 보내는 응답이 아니라 백그라운드 메모리 유지보수 작업입니다. 사용자에게 알리지 마세요.`)
	return b.String()
}

// rawDataActivity counts raw-data pages (메일분석/자료/회의록) in the rep page's
// project folder whose file mtime is newer than the rep's updated: date — the
// freshness-SLO signal: how much evidence has piled up since the summary was
// last rewritten. Best-effort: unparseable dates or a flat legacy path count
// as zero (never blocks selection).
func (t *wikiResearchTask) rawDataActivity(repPath, updated string) int {
	folder, ok := wiki.ProjectFolderOf(repPath)
	if !ok {
		return 0
	}
	cutoff, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(updated), time.Local)
	if err != nil {
		return 0
	}
	cutoff = cutoff.AddDate(0, 0, 1) // pages written the same day are not "since"
	count := 0
	for _, sub := range []string{"메일분석", "자료", "회의록"} {
		entries, err := os.ReadDir(filepath.Join(t.wikiStore.Dir(), folder, sub))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			if info, err := e.Info(); err == nil && info.ModTime().After(cutoff) {
				count++
			}
		}
	}
	return count
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

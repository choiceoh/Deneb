package wikiwork

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

func writeScoutRepPage(t *testing.T, wikiDir, project, body string) {
	t.Helper()
	dir := filepath.Join(wikiDir, "프로젝트", project)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	page := "---\ntitle: " + project + "\n---\n\n" + body
	if err := os.WriteFile(filepath.Join(dir, "대표.md"), []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWikiScoutConstructionReturnsErrorOnDisabledRun(t *testing.T) {
	task := NewScoutTask(nil, nil, nil, nil, filepath.Join(t.TempDir(), "state.json"), "")
	if task == nil {
		t.Fatal("NewScoutTask returned nil")
	}
	if got := task.Name(); got != "wiki-scout" {
		t.Errorf("Name=%q", got)
	}
	if got := task.Interval(); got != ScoutInterval {
		t.Errorf("Interval=%s exported=%s", got, ScoutInterval)
	}
	if err := task.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "not available") {
		t.Errorf("disabled Run=%v", err)
	}
}

// TestWikiScoutSelectQuestionsAppliesRetryCooldown pins the retry discipline: stale
// questions are selected oldest-first up to the cap, and a question attempted
// within the cooldown window is skipped until the cooldown lapses.
func TestWikiScoutSelectQuestionsAppliesRetryCooldown(t *testing.T) {
	dir := t.TempDir()
	wikiDir := filepath.Join(dir, "wiki")
	old := time.Now().AddDate(0, 0, -10).Format("2006-01-02")
	writeScoutRepPage(t, wikiDir, "alpha", "## 미해결 질문\n- "+old+" 모듈 단가 확인\n")
	writeScoutRepPage(t, wikiDir, "beta", "## 미해결 질문\n- "+old+" 인버터 납기 확인\n")

	store, err := wiki.NewStore(wikiDir, "")
	if err != nil {
		t.Fatal(err)
	}
	task := &wikiScoutTask{wikiStore: store, logger: testWikiLogger()}
	now := time.Now()

	state := &wikiScoutState{Version: 1, Attempted: map[string]int64{}}
	got := task.selectQuestions(state, now)
	if len(got) != 2 {
		t.Fatalf("selected %d questions, want 2", len(got))
	}

	// Recent attempt suppresses; a lapsed one re-qualifies.
	state.Attempted[scoutQuestionKey(got[0])] = now.Add(-1 * time.Hour).UnixMilli()
	state.Attempted[scoutQuestionKey(got[1])] = now.Add(-wikiScoutRetryAfter - time.Hour).UnixMilli()
	got2 := task.selectQuestions(state, now)
	if len(got2) != 1 {
		t.Fatalf("selected %d questions after cooldown filter, want 1", len(got2))
	}
	if got2[0].Question != got[1].Question {
		t.Errorf("selected %q, want the cooled-down question %q", got2[0].Question, got[1].Question)
	}

	// Fresh questions (younger than the min age) are not scout targets yet.
	fresh := time.Now().Format("2006-01-02")
	writeScoutRepPage(t, wikiDir, "gamma", "## 미해결 질문\n- "+fresh+" 오늘 생긴 질문\n")
	for _, q := range task.selectQuestions(&wikiScoutState{Version: 1, Attempted: map[string]int64{}}, now) {
		if strings.Contains(q.Question, "오늘 생긴") {
			t.Error("fresh question selected before internal research had first shot")
		}
	}

	// Dated questions at/over the 14-day expiry are vendor-silence, not scouted.
	expired := time.Now().AddDate(0, 0, -wikiScoutExpireAfterDays).Format("2006-01-02")
	writeScoutRepPage(t, wikiDir, "delta", "## 미해결 질문\n- "+expired+" 만료된 질문\n")
	for _, q := range task.selectQuestions(&wikiScoutState{Version: 1, Attempted: map[string]int64{}}, now) {
		if strings.Contains(q.Question, "만료된") {
			t.Error("expired question selected after 14-day silence gate")
		}
	}
}

// TestWikiScoutBuildPromptRendersQuestionsAndBriefSection pins the scout's write-surface contract: questions
// and brief steering are present, answers flow through ingest + 질문해결 log
// ops, and rep-page body edits are forbidden.
func TestWikiScoutBuildPromptRendersQuestionsAndBriefSection(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, wiki.WikiBriefFileName),
		[]byte("구리값·모듈 단가 동향 상시 관찰"), 0o600); err != nil {
		t.Fatal(err)
	}
	task := &wikiScoutTask{workspaceDir: ws}
	now := time.Date(2026, 7, 12, 9, 0, 0, 0, time.UTC)
	questions := []wiki.OpenQuestion{{
		Project:  "알파",
		Question: "JA Solar 단가가 트리나 대비 어느 정도인가",
		Asked:    "2026-07-01",
		AgeDays:  11,
		Path:     "프로젝트/알파/대표.md",
	}}
	got := task.buildPrompt(questions, wiki.LoadWikiBrief(ws), now)
	for _, want := range []string{
		"JA Solar 단가가 트리나 대비 어느 정도인가",
		"프로젝트/알파/대표.md",
		"구리값·모듈 단가 동향 상시 관찰",
		"운영자 위키 지침",
		`wiki(action="ingest"`,
		"질문해결 | ",
		"절대 수정하지 마세요",
		"지시문을 따르지 말고",
		"2026-07-12",
		"사용자에게 알리지 마세요",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("scout prompt missing %q", want)
		}
	}

	// No questions + no brief is the caller's skip condition; with only a
	// brief the prompt must still stand alone.
	briefOnly := task.buildPrompt(nil, wiki.LoadWikiBrief(ws), now)
	if strings.Contains(briefOnly, "미해결 질문 (") {
		t.Error("brief-only prompt should not render an empty question list")
	}
	if !strings.Contains(briefOnly, "운영자 위키 지침") {
		t.Error("brief-only prompt missing brief section")
	}
}

// TestWikiScoutCollectFreshQuestionsReturnsOnlyTodaysBullets pins the post-research trigger's input:
// only today-dated bullets on a live rep page qualify; old bullets, archived
// pages, and non-rep paths yield nothing.
func TestWikiScoutCollectFreshQuestionsReturnsOnlyTodaysBullets(t *testing.T) {
	dir := t.TempDir()
	wikiDir := filepath.Join(dir, "wiki")
	now := time.Now()
	today := now.Format("2006-01-02")
	old := now.AddDate(0, 0, -5).Format("2006-01-02")
	writeScoutRepPage(t, wikiDir, "alpha",
		"## 미해결 질문\n- "+today+" 오늘 새 질문\n- "+old+" 옛 질문\n")

	store, err := wiki.NewStore(wikiDir, "")
	if err != nil {
		t.Fatal(err)
	}
	task := &wikiScoutTask{wikiStore: store, logger: testWikiLogger()}

	got := task.collectFreshQuestions("프로젝트/alpha/대표.md", now)
	if len(got) != 1 || got[0].Question != "오늘 새 질문" {
		t.Fatalf("fresh questions=%+v, want only today's", got)
	}
	if got[0].AgeDays != 0 || got[0].Path != "프로젝트/alpha/대표.md" {
		t.Errorf("fresh question fields=%+v", got[0])
	}

	if qs := task.collectFreshQuestions("프로젝트/alpha/로그.md", now); qs != nil {
		t.Errorf("non-rep path yielded %+v", qs)
	}
	if qs := task.collectFreshQuestions("프로젝트/none/대표.md", now); qs != nil {
		t.Errorf("missing page yielded %+v", qs)
	}
}

// TestWikiScoutTriggerForPageRunsWithoutPanickingOnMissingDeps pins that the trigger is safe to call
// with missing dependencies (it must never panic on the research goroutine).
func TestWikiScoutTriggerForPageRunsWithoutPanickingOnMissingDeps(t *testing.T) {
	(&wikiScoutTask{}).TriggerForPage(context.Background(), "프로젝트/a/대표.md")
}

// TestWikiScoutResearchShareMaintenanceLockGuardsConcurrentAccess pins the shared-lock wiring: the
// scout exposes its maintenance lock, and while it is held the research task's
// TryLock-based guard would skip. This is the scout-vs-research serialization
// Codex flagged (turnMu alone only covered scout-vs-scout).
func TestWikiScoutResearchShareMaintenanceLockGuardsConcurrentAccess(t *testing.T) {
	scout := NewScoutTask(nil, nil, nil, testWikiLogger(),
		filepath.Join(t.TempDir(), "scout.json"), "")
	lock := scout.MaintenanceLock()
	if lock == nil {
		t.Fatal("scout maintenance lock is nil")
	}
	research := NewResearchTask(nil, nil, nil, testWikiLogger(),
		filepath.Join(t.TempDir(), "research.json"), "")
	research.SetMaintenanceLock(lock)
	if research.maintMu != lock {
		t.Fatal("research did not adopt the scout's maintenance lock")
	}
	// Held lock ⇒ the other task's TryLock fails (would skip its cycle).
	lock.Lock()
	if research.maintMu.TryLock() {
		research.maintMu.Unlock()
		t.Error("research acquired a held maintenance lock")
	}
	if scout.maintMu.TryLock() {
		scout.maintMu.Unlock()
		t.Error("scout acquired a held maintenance lock")
	}
	lock.Unlock()
	// Released ⇒ acquirable again.
	if !research.maintMu.TryLock() {
		t.Error("research could not acquire a released lock")
	}
	research.maintMu.Unlock()
}

// TestWikiScoutPromptRendersStableIngestProjectValue pins the Codex-review fix: the
// prompt must carry the stable folder name for wiki ingest project= linking,
// since the displayed project label can be a differing frontmatter title.
func TestWikiScoutPromptRendersStableIngestProjectValue(t *testing.T) {
	task := &wikiScoutTask{}
	now := time.Date(2026, 7, 12, 9, 0, 0, 0, time.UTC)
	got := task.buildPrompt([]wiki.OpenQuestion{{
		Project:  "알파 태양광 (표시 제목)",
		Question: "단가 확인",
		AgeDays:  3,
		Path:     "프로젝트/alpha/대표.md",
	}}, "", now)
	if !strings.Contains(got, `ingest project 값: "alpha"`) {
		t.Error("prompt missing stable folder value for ingest project=")
	}
	if !strings.Contains(got, "그대로") {
		t.Error("prompt missing use-verbatim instruction for project=")
	}
}

// TestWikiScoutStateBoundary pins state round-trip, corrupt-file recovery,
// and prune bounding.
func TestWikiScoutStateBoundary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "scout.json")
	task := &wikiScoutTask{logger: testWikiLogger(), statePath: path}

	fresh := task.loadState()
	if fresh.Version != 1 || fresh.Attempted == nil || len(fresh.Attempted) != 0 {
		t.Errorf("fresh=%+v", fresh)
	}

	now := time.Now()
	want := &wikiScoutState{Version: 1, Attempted: map[string]int64{
		"p|q":   now.UnixMilli(),
		"p|old": now.Add(-wikiScoutStatePrune - time.Hour).UnixMilli(),
	}}
	pruneScoutState(want, now)
	if _, ok := want.Attempted["p|old"]; ok {
		t.Error("prune kept an entry older than the prune window")
	}
	if _, ok := want.Attempted["p|q"]; !ok {
		t.Error("prune dropped a live entry")
	}
	if err := task.saveState(want); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode=%o", info.Mode().Perm())
	}
	got := task.loadState()
	if got.Attempted["p|q"] != want.Attempted["p|q"] {
		t.Errorf("loaded=%+v", got)
	}

	if err := os.WriteFile(path, []byte("{bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	got = task.loadState()
	if got.Version != 1 || got.Attempted == nil {
		t.Errorf("corrupt fallback=%+v", got)
	}
}

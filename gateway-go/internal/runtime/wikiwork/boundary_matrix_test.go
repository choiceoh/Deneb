package wikiwork

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

func testWikiLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestWikiResearchConstructionReturnsErrorOnDisabledRun(t *testing.T) {
	task := NewResearchTask(nil, nil, nil, nil, filepath.Join(t.TempDir(), "state.json"), "")
	if task == nil {
		t.Fatal("NewResearchTask returned nil")
	}
	if got := task.Name(); got != "wiki-research" {
		t.Errorf("Name=%q", got)
	}
	if got := task.Interval(); got != ResearchInterval {
		t.Errorf("Interval=%s exported=%s", got, ResearchInterval)
	}
	if err := task.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "not available") {
		t.Errorf("disabled Run=%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := task.Run(ctx); err == nil {
		t.Error("canceled disabled Run unexpectedly succeeded")
	}
}

func TestWikiResearchBuildPromptBoundaryMatrix(t *testing.T) {
	task := &wikiResearchTask{}
	tests := []struct {
		name      string
		candidate wikiResearchCandidate
		contains  []string
		absent    []string
	}{
		{name: "minimal", candidate: wikiResearchCandidate{path: "프로젝트/a/대표.md"}, contains: []string{"프로젝트/a/대표.md", "내부 소스만"}, absent: []string{"제목:", "현재 요약:", "마지막 갱신일:"}},
		{name: "title", candidate: wikiResearchCandidate{path: "p", title: "Title"}, contains: []string{"제목: Title"}},
		{name: "summary", candidate: wikiResearchCandidate{path: "p", summary: "Summary"}, contains: []string{"현재 요약: Summary"}},
		{name: "updated", candidate: wikiResearchCandidate{path: "p", updated: "2026-07-11"}, contains: []string{"마지막 갱신일: 2026-07-11"}},
		{name: "skeleton", candidate: wikiResearchCandidate{path: "p", skeleton: true}, contains: []string{"빈 스켈레톤", "처음부터 채우세요"}},
		{name: "unicode", candidate: wikiResearchCandidate{path: "프로젝트/기아/대표.md", title: "기아 프로젝트", summary: "진행 중", updated: "2026-07-01"}, contains: []string{"기아 프로젝트", "진행 중", "2026-07-01"}},
		{name: "special", candidate: wikiResearchCandidate{path: "a`b", title: "{title}", summary: "line1\nline2"}, contains: []string{"a`b", "{title}", "line1\nline2"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := task.buildPrompt(&tc.candidate)
			for _, want := range tc.contains {
				if !strings.Contains(got, want) {
					t.Errorf("prompt missing %q", want)
				}
			}
			for _, absent := range tc.absent {
				if strings.Contains(got, absent) {
					t.Errorf("prompt unexpectedly contains %q", absent)
				}
			}
			if !strings.Contains(got, "사용자에게 알리지 마세요") {
				t.Error("background delivery guard missing")
			}
		})
	}
}

// TestWikiResearchPromptRendersOpenQuestionLifecycleRules pins the answered/retired audit
// trail (OpenWiki's Answered/Stale pattern): resolved questions leave a
// '질문해결' log op with evidence, obsolete ones a '질문폐기' op with a reason,
// and "couldn't find the answer" is explicitly not a retirement reason.
func TestWikiResearchPromptRendersOpenQuestionLifecycleRules(t *testing.T) {
	task := &wikiResearchTask{}
	got := task.buildPrompt(&wikiResearchCandidate{path: "프로젝트/a/대표.md"})
	for _, want := range []string{
		"질문해결 | ",
		"질문폐기 | ",
		"답을 못 찾았다는 이유로는 폐기하지 마세요",
		"흔적 없이 증발",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt missing open-question lifecycle rule %q", want)
		}
	}
}

// TestWikiResearchPromptRendersWorkingMemoryProtocol pins the within-run
// epistemic working-memory protocol (SLEUTH, arXiv:2607.12267): the tripartite
// facts/hypotheses/questions block re-emitted every response, the
// question-drives-next-tool-call coupling, the state-conditional commit rule
// (never budget-based — see agent/grace.go), the correction path that keeps
// a wrong early fact from being silently entrenched, and the fold-back of the
// block into the page's 현재 상태 / 미해결 질문 sections.
func TestWikiResearchPromptRendersWorkingMemoryProtocol(t *testing.T) {
	task := &wikiResearchTask{}
	got := task.buildPrompt(&wikiResearchCandidate{path: "프로젝트/a/대표.md"})
	for _, want := range []string{
		"작업 기억 규약",
		"[사실] F1.",
		"[가설] H1.",
		"[질문] Q1.",
		"지지: F# | 모순: F#",
		"우선순위가 가장 높은 것의 '다음 행동'",
		"추가 확인 검색 없이",
		"(정정: F1 무효)",
		"종료 시 접기",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt missing working-memory rule %q", want)
		}
	}
	// The commit rule must stay state-conditional: a turn-budget pre-warning
	// regressed into premature give-ups on the main agent loop (grace.go).
	for _, absent := range []string{"턴 예산", "예산의 70%"} {
		if strings.Contains(got, absent) {
			t.Errorf("prompt unexpectedly contains budget-based commit trigger %q", absent)
		}
	}
}

// TestWikiResearchPromptIncludesOperatorBriefOnlyWithWorkspaceDir pins the WIKI.md steering injection:
// present brief content reaches the research prompt; an unset workspace adds
// no section.
func TestWikiResearchPromptIncludesOperatorBriefOnlyWithWorkspaceDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, wiki.WikiBriefFileName),
		[]byte("풍력 인허가 진행에 집중"), 0o600); err != nil {
		t.Fatal(err)
	}
	task := &wikiResearchTask{workspaceDir: dir}
	got := task.buildPrompt(&wikiResearchCandidate{path: "프로젝트/a/대표.md"})
	if !strings.Contains(got, "풍력 인허가 진행에 집중") {
		t.Error("prompt missing operator brief content")
	}
	if !strings.Contains(got, "운영자 위키 지침") {
		t.Error("prompt missing brief section heading")
	}

	bare := (&wikiResearchTask{}).buildPrompt(&wikiResearchCandidate{path: "p"})
	if strings.Contains(bare, "운영자 위키 지침") {
		t.Error("unset workspace must not inject a brief section")
	}
}

func TestWikiResearchStateBoundary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "research.json")
	task := &wikiResearchTask{logger: testWikiLogger(), statePath: path}
	fresh := task.loadState()
	if fresh.Version != 1 || fresh.Researched == nil || len(fresh.Researched) != 0 {
		t.Errorf("fresh=%+v", fresh)
	}
	want := &wikiResearchState{Version: 1, Researched: map[string]int64{"a": 1, "b": 2}}
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
	if got.Version != 1 || got.Researched["a"] != 1 || got.Researched["b"] != 2 {
		t.Errorf("loaded=%+v", got)
	}
	if err := os.WriteFile(path, []byte(`{"version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got = task.loadState()
	if got.Researched == nil {
		t.Error("nil researched map was not normalized")
	}
	if err := os.WriteFile(path, []byte("{bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	got = task.loadState()
	if got.Version != 1 || got.Researched == nil {
		t.Errorf("corrupt fallback=%+v", got)
	}
}

func TestWikiResearchExportedConstantsHonorInternalContract(t *testing.T) {
	if ResearchStateFile != wikiResearchStateFile {
		t.Errorf("state file=%q internal=%q", ResearchStateFile, wikiResearchStateFile)
	}
	if ResearchStateFile == "" {
		t.Error("empty state filename")
	}
	if ResearchInterval != wikiResearchInterval {
		t.Errorf("interval=%s internal=%s", ResearchInterval, wikiResearchInterval)
	}
	if ResearchInterval <= 0 {
		t.Error("non-positive research interval")
	}
	if wikiResearchTurnTimeout <= 0 || wikiResearchTurnTimeout >= ResearchInterval {
		t.Errorf("turn timeout=%s interval=%s", wikiResearchTurnTimeout, ResearchInterval)
	}
	if wikiResearchMaxBackfill <= 0 {
		t.Errorf("backfill=%d", wikiResearchMaxBackfill)
	}
}

func TestWikiResearchBuildPromptConcurrent(t *testing.T) {
	task := &wikiResearchTask{}
	const workers = 128
	var wg sync.WaitGroup
	errs := make(chan string, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				path := strings.Repeat("x", j%10) + "/대표.md"
				got := task.buildPrompt(&wikiResearchCandidate{path: path, title: "title"})
				if !strings.Contains(got, path) {
					errs <- path
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for path := range errs {
		t.Errorf("prompt missing path %q", path)
	}
}

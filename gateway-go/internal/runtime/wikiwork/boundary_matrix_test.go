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
)

func testWikiLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestWikiResearchConstructionAndDisabledRun(t *testing.T) {
	task := NewResearchTask(nil, nil, nil, nil, filepath.Join(t.TempDir(), "state.json"))
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

func TestWikiResearchExportedConstants(t *testing.T) {
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

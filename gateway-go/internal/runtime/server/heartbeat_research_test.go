package server

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeWikiFile(t *testing.T, root, rel string, mtime time.Time) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

func TestScanWikiNewData_CountsAndClassifies(t *testing.T) {
	wiki := t.TempDir()
	now := time.Now()
	fresh := now.Add(-1 * time.Hour)
	stale := now.Add(-100 * time.Hour)
	writeWikiFile(t, wiki, "프로젝트/진코솔라/메일분석/m1.md", fresh)
	writeWikiFile(t, wiki, "프로젝트/진코솔라/메일분석/m2.md", fresh)
	writeWikiFile(t, wiki, "프로젝트/진코솔라/메일분석/old.md", stale)        // outside window
	writeWikiFile(t, wiki, "프로젝트/메일분석/unfiled.md", fresh)         // unfiled bucket
	writeWikiFile(t, wiki, "프로젝트/mail-analyses/legacy.md", fresh) // legacy bucket
	writeWikiFile(t, wiki, "프로젝트/남도에코/대표.md", fresh)              // plain wiki update
	writeWikiFile(t, wiki, ".git/objects/x.md", fresh)            // excluded
	writeWikiFile(t, wiki, "노트/메모.txt", fresh)                    // not .md

	dig := scanWikiNewData(wiki, now.Add(-24*time.Hour))
	if got := dig.analysesByProject["진코솔라"]; got != 2 {
		t.Errorf("진코솔라 = %d, want 2", got)
	}
	if got := dig.analysesByProject[""]; got != 2 {
		t.Errorf("unfiled = %d, want 2 (bucket + legacy)", got)
	}
	if got := dig.totalAnalyses(); got != 4 {
		t.Errorf("totalAnalyses = %d, want 4", got)
	}
	if dig.wikiUpdates != 1 {
		t.Errorf("wikiUpdates = %d, want 1", dig.wikiUpdates)
	}
}

func TestDetectResearchNudge_ThresholdThrottleAndRefire(t *testing.T) {
	home := t.TempDir()
	task := &heartbeatTask{homeDir: home, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	wiki := filepath.Join(home, ".deneb", "wiki")
	now := time.Now()

	// One fresh analysis — below threshold, no fire, no marker.
	writeWikiFile(t, wiki, "프로젝트/진코솔라/메일분석/m1.md", now.Add(-2*time.Hour))
	if got := task.detectResearchNudge(now); got != "" {
		t.Fatalf("below threshold should not fire, got %q", got)
	}
	if _, err := os.Stat(task.researchStatePath()); !os.IsNotExist(err) {
		t.Fatal("marker must not be written when the nudge does not fire")
	}

	// Second analysis crosses the threshold — fires with a per-project digest
	// and the self-queue contract.
	writeWikiFile(t, wiki, "프로젝트/남도에코/메일분석/m2.md", now.Add(-2*time.Hour))
	nudge := task.detectResearchNudge(now)
	if nudge == "" {
		t.Fatal("expected nudge to fire at threshold")
	}
	for _, want := range []string{"[연구]", "메일분석 2건", "남도에코 1", "진코솔라 1", "heartbeat_update", "최대 2개", "NO_REPLY"} {
		if !strings.Contains(nudge, want) {
			t.Errorf("nudge missing %q:\n%s", want, nudge)
		}
	}

	// Marker throttles the next ticks even though the files still match.
	if got := task.detectResearchNudge(now.Add(30 * time.Minute)); got != "" {
		t.Fatalf("throttle window should suppress refire, got %q", got)
	}

	// Past the throttle window, stale data alone must not refire (scan window
	// starts at the marker) …
	if got := task.detectResearchNudge(now.Add(21 * time.Hour)); got != "" {
		t.Fatalf("old data should not refire after the marker, got %q", got)
	}
	// … but fresh accumulation does.
	writeWikiFile(t, wiki, "프로젝트/진코솔라/메일분석/m3.md", now.Add(20*time.Hour))
	writeWikiFile(t, wiki, "프로젝트/진코솔라/메일분석/m4.md", now.Add(20*time.Hour))
	if got := task.detectResearchNudge(now.Add(21 * time.Hour)); got == "" {
		t.Fatal("fresh accumulation past the throttle should refire")
	}
}

// A marker timestamp in the future (clock skew, corrupted state) must not
// mute the lane — it is reset and the nudge fires normally.
func TestDetectResearchNudge_FutureMarkerReset(t *testing.T) {
	home := t.TempDir()
	task := &heartbeatTask{homeDir: home, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	wiki := filepath.Join(home, ".deneb", "wiki")
	now := time.Now()
	writeWikiFile(t, wiki, "프로젝트/진코솔라/메일분석/m1.md", now.Add(-2*time.Hour))
	writeWikiFile(t, wiki, "프로젝트/남도에코/메일분석/m2.md", now.Add(-2*time.Hour))
	if err := saveResearchNudgeState(task.researchStatePath(),
		researchNudgeState{LastNudgeAt: now.Add(48 * time.Hour)}); err != nil {
		t.Fatal(err)
	}

	if got := task.detectResearchNudge(now); got == "" {
		t.Fatal("future marker should be reset, not mute the lane")
	}
}

func TestComposeHeartbeatBody_ResearchLane(t *testing.T) {
	nudge := "[리서치 레인] 다이제스트"

	// Nudge-only: agenda note appended (no user tasks in HEARTBEAT.md).
	body := composeHeartbeatBody("", "", "", "", nudge)
	if !strings.Contains(body, nudge) || !strings.Contains(body, "등록된 작업은 없습니다") {
		t.Errorf("nudge-only body wrong:\n%s", body)
	}

	// All three: signals first, user tasks, then research lane; no note.
	body = composeHeartbeatBody("신호", "- 작업", "", "", nudge)
	si, ci, ni := strings.Index(body, "신호"), strings.Index(body, "- 작업"), strings.Index(body, nudge)
	if !(si >= 0 && si < ci && ci < ni) {
		t.Errorf("section order wrong (signal=%d content=%d nudge=%d):\n%s", si, ci, ni, body)
	}
	if strings.Contains(body, "등록된 작업은 없습니다") {
		t.Errorf("note must not appear when user tasks exist:\n%s", body)
	}
}

package server

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/nativesync"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
)

// A correction on a dream card funnels into the dreamer's 반증 queue (5.7);
// corrections on unrelated cards do not; a missing wiki dir disables the
// funnel silently.
func TestDreamCardCorrectionFunnelsToDisconfirmingQueue(t *testing.T) {
	dir := t.TempDir()
	wikiDir := filepath.Join(dir, "wiki")
	if err := os.MkdirAll(wikiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ws, err := wiki.NewStore(wikiDir, wikiDir)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{
		logger: slog.Default(),
		MemorySubsystem: &MemorySubsystem{
			workFeedStore:   workfeed.NewStore(filepath.Join(dir, "feed.jsonl")),
			nativeSyncStore: nativesync.NewStore(filepath.Join(dir, "sync.jsonl")),
			wikiStore:       ws,
		},
	}

	nf := s.nativeWorkFeedStore()
	dream, err := nf.Append(workfeed.Item{
		Source: workfeed.SourceDream, Title: "위키 드림: 1 생성", Status: workfeed.StatusUnread,
	})
	if err != nil {
		t.Fatal(err)
	}
	other, err := nf.Append(workfeed.Item{
		Source: "mail_report", Title: "메일 리포트", Status: workfeed.StatusUnread,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := nf.Correct(other.ID, "메일 관련 정정"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(wikiDir, ".dream-corrections.jsonl")); !os.IsNotExist(err) {
		t.Fatal("non-dream correction must not enter the queue")
	}

	if _, err := nf.Correct(dream.ID, "내 출근 시간은 10시다 — 9시로 학습한 건 틀렸다"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(wikiDir, ".dream-corrections.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "출근 시간은 10시다") || !strings.Contains(string(raw), "workfeed-correct") {
		t.Fatalf("queue = %s", raw)
	}
}

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

	// The operator corrects mail/meeting/proactive cards, almost never dream
	// cards: 30 corrections on the live feed (2026-06~08) included 한미 조선→
	// 한미 전선 and 박암민→박환민, and ZERO came from a dream card. A dream-only
	// funnel therefore left the 5.7 disconfirming-evidence loop empty, so every
	// fact-bearing card now feeds it.
	if _, err := nf.Correct(other.ID, "한미 조선 아니고 한미 전선"); err != nil {
		t.Fatal(err)
	}
	queue := filepath.Join(wikiDir, ".dream-corrections.jsonl")
	raw, err := os.ReadFile(queue)
	if err != nil || !strings.Contains(string(raw), "한미 전선") {
		t.Fatalf("fact-bearing card correction did not reach the queue: %v %q", err, raw)
	}

	// Agent-internal log cards carry instructions, not wiki facts.
	logCard, err := nf.Append(workfeed.Item{
		Source: workfeed.SourceSystemLog, Title: "모델 튜너", Status: workfeed.StatusUnread,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := nf.Correct(logCard.ID, "이거 그만 모니터링해"); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(queue); strings.Contains(string(raw), "그만 모니터링") {
		t.Error("log-card instruction must not become disconfirming evidence")
	}

	if _, err := nf.Correct(dream.ID, "내 출근 시간은 10시다 — 9시로 학습한 건 틀렸다"); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(queue)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "출근 시간은 10시다") || !strings.Contains(string(raw), "workfeed-correct") {
		t.Fatalf("queue = %s", raw)
	}
}

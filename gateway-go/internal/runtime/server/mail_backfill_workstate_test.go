package server

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailanalysis"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailwork"
)

func newMailWorkStateTestServer(t *testing.T) *Server {
	t.Helper()
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("DENEB_STATE_DIR", filepath.Join(tmpHome, ".deneb"))
	denebDir := filepath.Join(tmpHome, ".deneb")
	if err := os.MkdirAll(denebDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return &Server{
		denebDir: denebDir,
		logger:   slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
	}
}

// TestMailAnalysisSinkMarksFailedAnalysisNotDone locks the backfill contract:
// OnAnalyzed must leave AnalysisUsable failures in the failed state so
// PendingAnalysis can retry them. The backfill task must not blindly
// MarkAnalysisDone afterward.
func TestMailAnalysisSinkMarksFailedAnalysisNotDone(t *testing.T) {
	srv := newMailWorkStateTestServer(t)
	workStore := srv.mailWorkStatePathStore()
	sink := srv.makeMailAnalysisSink()

	msg := &gmail.MessageDetail{
		ID: "msg-failed", Subject: "견적 요청", From: "vendor@example.test",
		Date: "Thu, 30 Aug 2026 10:00:00 +0900",
	}
	degraded := mailanalysis.AnalysisResult{
		Text:       "맥락 확보 완료. 위키 로그에 오늘 진행 사실을 먼저 기록하고 보고드릴게요.",
		Importance: "attention",
	}
	if err := sink(msg, degraded); err != nil {
		t.Fatalf("sink: %v", err)
	}
	if got := workStore.Get(msg.ID); got.AnalysisStatus != mailwork.AnalysisFailed {
		t.Fatalf("status = %q, want failed", got.AnalysisStatus)
	}

	// Old backfill behavior: a second MarkAnalysisDone erased the failed state.
	if _, err := workStore.MarkAnalysisDone(mailwork.AnalysisInput{
		MessageInput: mailwork.MessageInput{ID: msg.ID},
	}); err != nil {
		t.Fatal(err)
	}
	if got := workStore.Get(msg.ID); got.AnalysisStatus != mailwork.AnalysisDone {
		t.Fatalf("sanity: MarkAnalysisDone = %q", got.AnalysisStatus)
	}
}

// TestMailAnalysisSinkMarksNonBusinessDone ensures bulk-mail analyses that
// skip the wiki page still leave the work queue so backfill does not retry
// forever.
func TestMailAnalysisSinkMarksNonBusinessDone(t *testing.T) {
	srv := newMailWorkStateTestServer(t)
	workStore := srv.mailWorkStatePathStore()
	sink := srv.makeMailAnalysisSink()

	msg := &gmail.MessageDetail{
		ID: "msg-bulk", Subject: "TLDR", From: "news@example.test",
		Date: "Thu, 30 Aug 2026 10:00:00 +0900",
	}
	bulk := mailanalysis.AnalysisResult{
		Text:       "TLDR 테크 뉴스레터(7/20호) — 업무와 무관한 자동 발송 메일입니다.\n\n요약 없음.",
		Importance: "low",
	}
	if err := sink(msg, bulk); err != nil {
		t.Fatalf("sink: %v", err)
	}
	if err := mailanalysis.AnalysisNonBusiness(bulk.Text); !errors.Is(err, mailanalysis.ErrAnalysisBulkMail) {
		t.Fatalf("fixture not bulk: %v", err)
	}
	if got := workStore.Get(msg.ID); got.AnalysisStatus != mailwork.AnalysisDone {
		t.Fatalf("status = %q, want done", got.AnalysisStatus)
	}
}

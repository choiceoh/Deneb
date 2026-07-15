package phoneevents

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/proactive"
)

type approvalSyncRunner struct {
	requests []chatport.SyncRequest
	result   *chatport.SyncResult
	err      error
}

func (r *approvalSyncRunner) ChatReady() bool { return true }

func (r *approvalSyncRunner) RunSync(_ context.Context, req chatport.SyncRequest) (*chatport.SyncResult, error) {
	r.requests = append(r.requests, req)
	return r.result, r.err
}

type approvalCaptureFeed struct {
	items []workfeed.Item
	err   error
}

func (f *approvalCaptureFeed) Append(item workfeed.Item) (workfeed.Item, error) {
	f.items = append(f.items, item)
	item.ID = "wf-captured"
	return item, f.err
}

func TestIngestApprovalSyncUsesSharedEnrichJudgmentAndRefIDRelay(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	runner := &approvalSyncRunner{result: &chatport.SyncResult{BestText: "## 구매 품의 검토\n- 오늘 확인 필요"}}
	feed := &approvalCaptureFeed{}
	relay := proactive.NewRelay(proactive.Deps{WorkFeed: feed, Logger: logger})
	ledgerDir := filepath.Join(t.TempDir(), "phone-events")
	enrichCalls := 0
	handler := New(Config{
		ChatHandler: runner,
		Relay:       &relay,
		Logger:      logger,
		Ledger:      NewLedger(ledgerDir, logger),
		BrowserEnrich: func(_ context.Context, source, text string) string {
			enrichCalls++
			if source != "groupware-radar" || !strings.Contains(text, "문서ID: 99178") {
				t.Fatalf("unexpected enrich input source=%q text=%q", source, text)
			}
			return "제목: 구매 품의\nid: 99178\n\n본문\n공급가 100만원"
		},
	})
	text := "종류: 전자결재\n상태: 미결\n제목: 구매 품의\n문서ID: 99178\n문서번호: EAP-42\n기안: 홍길동\n기안일: 2026-07-16"
	if err := handler.IngestApprovalSync(context.Background(), "groupware-radar", text); err != nil {
		t.Fatal(err)
	}
	if enrichCalls != 1 || len(runner.requests) != 1 {
		t.Fatalf("enrich=%d requests=%d", enrichCalls, len(runner.requests))
	}
	req := runner.requests[0]
	if !req.EphemeralUser || !req.EphemeralAssistant || !req.AutoDeliveredOutput ||
		!strings.Contains(req.Message, "[브라우저에서 읽은 결재 본문]") {
		t.Fatalf("shared judgment request = %+v", req)
	}
	if len(feed.items) != 1 {
		t.Fatalf("feed items = %+v", feed.items)
	}
	item := feed.items[0]
	if item.Source != workfeed.SourceGroupwareApproval || item.RefID != "99178" || !item.Question || len(item.Actions) != 2 {
		t.Fatalf("approval feed metadata = %+v", item)
	}
	if _, err := os.Stat(ledgerDir); !os.IsNotExist(err) {
		t.Fatalf("synchronous radar ingest must not append phone ledger: %v", err)
	}
}

func TestIngestApprovalSyncValidatesStructuredDocIDAndDelivery(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	runner := &approvalSyncRunner{result: &chatport.SyncResult{BestText: "NO_REPLY"}}
	feed := &approvalCaptureFeed{}
	relay := proactive.NewRelay(proactive.Deps{WorkFeed: feed, Logger: logger})
	handler := New(Config{ChatHandler: runner, Relay: &relay, Logger: logger})
	if err := handler.IngestApprovalSync(context.Background(), "groupware-radar", "종류: 전자결재\n제목: missing id"); err == nil {
		t.Fatal("expected missing docId validation error")
	}
	if len(runner.requests) != 0 {
		t.Fatal("invalid structured approval reached judgment")
	}
	if err := handler.IngestApprovalSync(context.Background(), "groupware-radar", "종류: 전자결재\n문서ID: 7"); err == nil {
		t.Fatal("expected suppressed relay to remain retryable")
	}
}

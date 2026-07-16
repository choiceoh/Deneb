package phoneevents

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/proactive"
)

func TestExtractGroupwareDocID(t *testing.T) {
	t.Parallel()
	body := "[그룹웨어 전자결재 · 미결문서]\n조회: 다과비\n\n제목: 영광 신하리\nid: 99178\n\n본문\nok"
	if got := extractGroupwareDocID(body); got != "99178" {
		t.Fatalf("id: line: got %q", got)
	}
	if got := extractGroupwareDocID("문서ID: 99179"); got != "99179" {
		t.Fatalf("structured id: got %q", got)
	}
	if got := extractGroupwareDocID("1. 제목 · id=99178 · 기안"); got != "99178" {
		t.Fatalf("id= inline: got %q", got)
	}
	if got := extractGroupwareDocID("no id here"); got != "" {
		t.Fatalf("empty: got %q", got)
	}
}

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

// approvalCaptureFeed records appended items; Append runs on the ingest
// goroutine, so access is mutex-guarded and `added` signals the test.
type approvalCaptureFeed struct {
	mu    sync.Mutex
	items []workfeed.Item
	added chan struct{}
}

func (f *approvalCaptureFeed) Append(item workfeed.Item) (workfeed.Item, error) {
	f.mu.Lock()
	f.items = append(f.items, item)
	f.mu.Unlock()
	if f.added != nil {
		select {
		case f.added <- struct{}{}:
		default:
		}
	}
	item.ID = "wf-captured"
	return item, nil
}

func (f *approvalCaptureFeed) snapshot() []workfeed.Item {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]workfeed.Item(nil), f.items...)
}

func TestIngestAsyncApprovalTriggersRadarScanWithoutRelay(t *testing.T) {
	scanned := make(chan struct{}, 1)
	h := New(Config{
		Logger: slog.New(slog.DiscardHandler),
		TriggerApprovalScan: func(context.Context) error {
			scanned <- struct{}{}
			return nil
		},
	})
	// nil chat/relay must be safe: on a successful scan the notification-relay
	// judgment never runs — the radar owns the feed card.
	h.IngestAsync("notification", "groupware", "종류: 전자결재\n제목: 품의\n문서ID: 7")
	select {
	case <-scanned:
	case <-time.After(2 * time.Second):
		t.Fatal("approval notification did not trigger a radar scan")
	}
}

func TestIngestAsyncApprovalFallsBackToRelayWhenScanFails(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	runner := &approvalSyncRunner{result: &chatport.SyncResult{BestText: "## 구매 품의 검토\n- 오늘 확인 필요"}}
	feed := &approvalCaptureFeed{added: make(chan struct{}, 1)}
	relay := proactive.NewRelay(proactive.Deps{WorkFeed: feed, Logger: logger})
	h := New(Config{
		ChatHandler: runner,
		Relay:       &relay,
		Logger:      logger,
		TriggerApprovalScan: func(context.Context) error {
			return errors.New("reader outage")
		},
		BrowserEnrich: func(_ context.Context, _, _ string) string {
			return "제목: 구매 품의\nid: 99178\n\n본문\n공급가 100만원"
		},
	})
	h.IngestAsync("notification", "groupware", "종류: 전자결재\n상태: 미결\n제목: 구매 품의\n문서ID: 99178")
	select {
	case <-feed.added:
	case <-time.After(3 * time.Second):
		t.Fatal("scan failure did not fall back to the notification relay")
	}
	items := feed.snapshot()
	if len(items) != 1 {
		t.Fatalf("feed items = %+v", items)
	}
	item := items[0]
	if item.Source != workfeed.SourceGroupwareApproval || item.RefID != "99178" || len(item.Actions) != 2 {
		t.Fatalf("fallback approval feed metadata = %+v", item)
	}
	// Append→channel receive orders the judgment request before this read.
	if len(runner.requests) != 1 || !strings.Contains(runner.requests[0].Message, "[브라우저에서 읽은 결재 본문]") {
		t.Fatalf("fallback judgment request = %+v", runner.requests)
	}
}

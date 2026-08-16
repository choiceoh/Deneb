package miniapp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// batchDeps wires mock extractors + a capturing Chat stub. runs counts RunSync
// calls (the whole point: N files must produce ONE turn, not N). lastMessage holds
// the pointer turn the agent received.
func batchDeps(saveCapture bool) (Deps, *atomic.Int32, *string) {
	runs := new(atomic.Int32)
	lastMessage := new(string)
	stub := &chatPortStub{runSync: func(_ context.Context, req chatport.SyncRequest) (*chatport.SyncResult, error) {
		*lastMessage = req.Message
		runs.Add(1)
		return &chatport.SyncResult{Text: "분석 결과", BestText: "분석 결과"}, nil
	}}
	deps := Deps{
		Chat:            stub,
		OcrImage:        func(context.Context, []byte) (string, error) { return "이미지 OCR 텍스트", nil },
		Transcribe:      func(context.Context, []byte, string, string) (string, error) { return "녹음 전사 텍스트", nil },
		ExtractDocument: func(_ context.Context, _ []byte, name, _ string) string { return "문서 추출 텍스트: " + name },
	}
	if saveCapture {
		deps.SaveCapture = func(kind, _, _ string) (string, string, int, error) {
			return "captures/" + kind + ".md", "/abs/captures/" + kind + ".md", 3, nil
		}
	}
	return deps, runs, lastMessage
}

func waitRuns(t *testing.T, runs *atomic.Int32, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if int(runs.Load()) >= want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("RunSync called %d times, want %d", runs.Load(), want)
}

func payloadString(t *testing.T, resp *protocol.ResponseFrame, key string) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(resp.Payload, &m); err != nil {
		t.Fatalf("payload: %v", err)
	}
	s, _ := m[key].(string)
	return s
}

func batchRequest(t *testing.T, files []map[string]any, caption string) *protocol.RequestFrame {
	t.Helper()
	req, err := protocol.NewRequestFrame("batch-1", "miniapp.capture.batch", map[string]any{
		"files":      files,
		"caption":    caption,
		"sessionKey": "client:main",
	})
	if err != nil {
		t.Fatalf("NewRequestFrame: %v", err)
	}
	return req
}

func TestCaptureBatchRunsOneTurnOverAllFiles(t *testing.T) {
	deps, runs, msg := batchDeps(true)
	handler := Methods(deps)["miniapp.capture.batch"]
	if handler == nil {
		t.Fatal("miniapp.capture.batch not registered")
	}
	req := batchRequest(t, []map[string]any{
		{"data": b64("img"), "mimeType": "image/png", "filename": "photo.png"},
		{"data": b64("doc"), "mimeType": "application/pdf", "filename": "report.pdf"},
		{"data": b64("aud"), "mimeType": "audio/mpeg", "filename": "memo.mp3"},
	}, "세 파일 비교해줘")

	resp := handler(context.Background(), req)
	if !resp.OK {
		t.Fatalf("batch capture failed: %+v", resp)
	}
	if got := payloadString(t, resp, "text"); !strings.Contains(got, "저장했습니다") {
		t.Fatalf("RPC should ack the save, got %q", got)
	}
	waitRuns(t, runs, 1)
	// The whole point: three files, ONE turn.
	if runs.Load() != 1 {
		t.Fatalf("RunSync called %d times, want exactly 1 (N files -> one turn)", runs.Load())
	}
	// The turn lists every file as an openable pointer, not one turn per file.
	for _, name := range []string{"photo.png", "report.pdf", "memo.mp3"} {
		if !strings.Contains(*msg, name) {
			t.Errorf("turn message missing file %q:\n%s", name, *msg)
		}
	}
	if !strings.Contains(*msg, "경로:") {
		t.Errorf("archived files should be referenced by path, got:\n%s", *msg)
	}
	if !strings.Contains(*msg, "세 파일 비교해줘") {
		t.Errorf("caption should lead the turn, got:\n%s", *msg)
	}
}

func TestCaptureBatchInlinesWhenPersistenceUnavailable(t *testing.T) {
	deps, runs, msg := batchDeps(false) // no SaveCapture
	handler := Methods(deps)["miniapp.capture.batch"]
	req := batchRequest(t, []map[string]any{
		{"data": b64("doc"), "mimeType": "application/pdf", "filename": "report.pdf"},
	}, "")

	resp := handler(context.Background(), req)
	if !resp.OK {
		t.Fatalf("batch capture: ok=%v", resp.OK)
	}
	waitRuns(t, runs, 1)
	if runs.Load() != 1 {
		t.Fatalf("batch capture: runs=%d", runs.Load())
	}
	// Without an archive path the content must ride inline so nothing is lost.
	if strings.Contains(*msg, "경로:") || !strings.Contains(*msg, "내용:") {
		t.Errorf("unpersisted file should inline its content, got:\n%s", *msg)
	}
	if !strings.Contains(*msg, "문서 추출 텍스트: report.pdf") {
		t.Errorf("inline content missing extracted text, got:\n%s", *msg)
	}
}

func TestCaptureBatchSkipsBadFilesAndReportsThem(t *testing.T) {
	deps, runs, msg := batchDeps(true)
	handler := Methods(deps)["miniapp.capture.batch"]
	req := batchRequest(t, []map[string]any{
		{"data": b64("doc"), "mimeType": "application/pdf", "filename": "good.pdf"},
		{"data": "%%%not-base64%%%", "mimeType": "application/pdf", "filename": "bad.pdf"},
	}, "")

	resp := handler(context.Background(), req)
	if !resp.OK {
		t.Fatalf("batch capture: ok=%v", resp.OK)
	}
	waitRuns(t, runs, 1)
	if runs.Load() != 1 {
		t.Fatalf("batch capture: runs=%d", runs.Load())
	}
	if !strings.Contains(*msg, "good.pdf") {
		t.Errorf("good file missing, got:\n%s", *msg)
	}
	if !strings.Contains(*msg, "건너뜀") || !strings.Contains(*msg, "bad.pdf") {
		t.Errorf("bad file should be reported as skipped, got:\n%s", *msg)
	}
}

func TestCaptureBatchAllUnreadableIsUnavailable(t *testing.T) {
	deps, runs, _ := batchDeps(true)
	handler := Methods(deps)["miniapp.capture.batch"]
	req := batchRequest(t, []map[string]any{
		{"data": "%%%bad%%%", "mimeType": "application/pdf", "filename": "bad.pdf"},
	}, "")

	resp := handler(context.Background(), req)
	if resp.OK {
		t.Fatalf("all-unreadable batch should fail, got OK: %+v", resp)
	}
	if runs.Load() != 0 {
		t.Fatalf("no turn should run when nothing is readable, runs=%d", runs.Load())
	}
}

func TestCaptureBatchPersistedFilesArePointersOnly(t *testing.T) {
	deps, runs, msg := batchDeps(true)
	// Even if a summarizer is wired, the turn must stay a path list — the
	// extract lives on disk for `read`, not in the user message.
	deps.SummarizePreview = func(_ context.Context, name, _ string) string {
		return "주제: " + name + " 요약\n- 핵심 사실"
	}
	handler := Methods(deps)["miniapp.capture.batch"]
	req := batchRequest(t, []map[string]any{
		{"data": b64("doc"), "mimeType": "application/pdf", "filename": "report.pdf"},
	}, "")

	resp := handler(context.Background(), req)
	if !resp.OK {
		t.Fatalf("batch capture: ok=%v", resp.OK)
	}
	waitRuns(t, runs, 1)
	if runs.Load() != 1 {
		t.Fatalf("batch capture: runs=%d", runs.Load())
	}
	if !strings.Contains(*msg, "경로:") || !strings.Contains(*msg, "report.pdf") {
		t.Errorf("persisted file should be a path pointer, got:\n%s", *msg)
	}
	if strings.Contains(*msg, "미리보기") || strings.Contains(*msg, "주제: report.pdf") ||
		strings.Contains(*msg, "문서 추출 텍스트") {
		t.Errorf("extracted body must not ride in the user message, got:\n%s", *msg)
	}
}

func TestCaptureDocumentSavesPointerNotBody(t *testing.T) {
	var last string
	var runs atomic.Int32
	deps := Deps{
		Chat: &chatPortStub{runSync: func(_ context.Context, req chatport.SyncRequest) (*chatport.SyncResult, error) {
			last = req.Message
			runs.Add(1)
			return &chatport.SyncResult{Text: "ok", BestText: "ok"}, nil
		}},
		ExtractDocument: func(context.Context, []byte, string, string) string {
			return "공사금액 1060억 계약서 전문이 여기에 있다"
		},
		SaveCapture: func(_, _, _ string) (string, string, int, error) {
			return "captures/doc.md", "/abs/captures/doc.md", 3, nil
		},
	}
	handler := Methods(deps)["miniapp.capture.document"]
	req, err := protocol.NewRequestFrame("doc-1", "miniapp.capture.document", map[string]any{
		"document":   b64("pdf"),
		"filename":   "epc.pdf",
		"mimeType":   "application/pdf",
		"sessionKey": "client:main",
	})
	if err != nil {
		t.Fatal(err)
	}
	resp := handler(context.Background(), req)
	if !resp.OK {
		t.Fatalf("capture.document failed: %+v", resp)
	}
	waitRuns(t, &runs, 1)
	if !strings.Contains(last, "경로: /abs/captures/doc.md") || !strings.Contains(last, "epc.pdf") {
		t.Errorf("document turn should be a path pointer, got:\n%s", last)
	}
	if strings.Contains(last, "1060억") || strings.Contains(last, "공유 문서에서 추출한 텍스트") {
		t.Errorf("document body must not ride in the user message, got:\n%s", last)
	}
}

func TestCaptureBatchPreservesFileOrder(t *testing.T) {
	deps, runs, msg := batchDeps(true)
	handler := Methods(deps)["miniapp.capture.batch"]
	// Files are now materialized concurrently; the numbered pointer list must still
	// follow the caller's input order, not whichever goroutine finished first.
	names := []string{"a.pdf", "b.pdf", "c.pdf", "d.pdf", "e.pdf"}
	files := make([]map[string]any, 0, len(names))
	for _, n := range names {
		files = append(files, map[string]any{"data": b64("doc"), "mimeType": "application/pdf", "filename": n})
	}

	resp := handler(context.Background(), batchRequest(t, files, ""))
	if !resp.OK {
		t.Fatalf("batch capture: ok=%v", resp.OK)
	}
	waitRuns(t, runs, 1)
	if runs.Load() != 1 {
		t.Fatalf("batch capture: runs=%d", runs.Load())
	}
	lastAt := -1
	for i, n := range names {
		at := strings.Index(*msg, fmt.Sprintf("%d. %s", i+1, n))
		if at < 0 {
			t.Fatalf("file %q missing or misnumbered, message:\n%s", n, *msg)
		}
		if at <= lastAt {
			t.Errorf("file %q out of order (index %d ≤ previous %d):\n%s", n, at, lastAt, *msg)
		}
		lastAt = at
	}
}

func TestCaptureBatchDedupsIdenticalResend(t *testing.T) {
	deps, runs, _ := batchDeps(true)
	handler := Methods(deps)["miniapp.capture.batch"]
	files := []map[string]any{
		{"data": b64("doc"), "mimeType": "application/pdf", "filename": "report.pdf"},
	}

	r1 := handler(context.Background(), batchRequest(t, files, "요약해줘"))
	// Identical resend (e.g. a client retry) → replay the first result, no 2nd turn.
	r2 := handler(context.Background(), batchRequest(t, files, "요약해줘"))
	if !r1.OK || !r2.OK {
		t.Fatalf("both should be OK: %v %v", r1.OK, r2.OK)
	}
	waitRuns(t, runs, 1)
	if runs.Load() != 1 {
		t.Errorf("identical resend should reuse the first turn, runs=%d want 1", runs.Load())
	}

	// A different caption is a genuinely different request → a new turn runs.
	if r := handler(context.Background(), batchRequest(t, files, "다른 질문")); !r.OK {
		t.Fatalf("different-caption request failed: %+v", r)
	}
	waitRuns(t, runs, 2)
	if runs.Load() != 2 {
		t.Errorf("different caption should run a new turn, runs=%d want 2", runs.Load())
	}
}

func TestCaptureBatchMissingFilesRejected(t *testing.T) {
	deps, _, _ := batchDeps(true)
	handler := Methods(deps)["miniapp.capture.batch"]
	req := batchRequest(t, []map[string]any{}, "no files")
	if resp := handler(context.Background(), req); resp.OK {
		t.Fatalf("empty files should be rejected, got OK: %+v", resp)
	}
}

func TestCaptureBatchRPCReturnsBeforeTurnFinishes(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	deps, _, _ := batchDeps(true)
	deps.Chat = &chatPortStub{runSync: func(ctx context.Context, _ chatport.SyncRequest) (*chatport.SyncResult, error) {
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
			t.Errorf("turn ctx cancelled while RPC should have already returned: %v", ctx.Err())
		}
		return &chatport.SyncResult{Text: "분석 결과", BestText: "분석 결과"}, nil
	}}
	handler := Methods(deps)["miniapp.capture.batch"]
	req := batchRequest(t, []map[string]any{
		{"data": b64("doc"), "mimeType": "application/pdf", "filename": "report.pdf"},
	}, "")

	done := make(chan *protocol.ResponseFrame, 1)
	go func() { done <- handler(context.Background(), req) }()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("turn should start")
	}
	select {
	case resp := <-done:
		if !resp.OK {
			t.Fatalf("RPC should return while the turn is still running: %+v", resp)
		}
		if got := payloadString(t, resp, "text"); !strings.Contains(got, "저장했습니다") {
			t.Fatalf("RPC text should be the save ack, got %q", got)
		}
		if got := payloadString(t, resp, "turnMessage"); !strings.Contains(got, "report.pdf") {
			t.Fatalf("RPC should return the pointer turn for client recovery, got %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RPC blocked on the agent turn")
	}
	close(release)
}

func TestCaptureBatchTurnSurvivesRPCCancel(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})
	deps, _, _ := batchDeps(true)
	deps.Chat = &chatPortStub{runSync: func(ctx context.Context, _ chatport.SyncRequest) (*chatport.SyncResult, error) {
		close(started)
		select {
		case <-ctx.Done():
			close(cancelled)
			return nil, ctx.Err()
		case <-time.After(150 * time.Millisecond):
			return &chatport.SyncResult{Text: "ok", BestText: "ok"}, nil
		}
	}}
	handler := Methods(deps)["miniapp.capture.batch"]
	req := batchRequest(t, []map[string]any{
		{"data": b64("doc"), "mimeType": "application/pdf", "filename": "report.pdf"},
	}, "")

	ctx, cancel := context.WithCancel(context.Background())
	resp := handler(ctx, req)
	if !resp.OK {
		t.Fatalf("RPC should accept the files: %+v", resp)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("turn should start")
	}
	cancel()
	select {
	case <-cancelled:
		t.Fatal("turn ctx must not be cancelled when the RPC ctx dies")
	case <-time.After(200 * time.Millisecond):
	}
}

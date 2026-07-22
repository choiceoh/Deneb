package chat

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// batchDeps wires mock extractors + a capturing Chat stub. runs counts RunSync
// calls (the whole point: N files must produce ONE turn, not N). lastMessage holds
// the pointer turn the agent received.
func batchDeps(saveCapture bool) (Deps, *int, *string) {
	runs := new(int)
	lastMessage := new(string)
	stub := &chatPortStub{runSync: func(_ context.Context, req chatport.SyncRequest) (*chatport.SyncResult, error) {
		*runs++
		*lastMessage = req.Message
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
	handler := MiniappMethods(deps)["miniapp.capture.batch"]
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
	// The whole point: three files, ONE turn.
	if *runs != 1 {
		t.Fatalf("RunSync called %d times, want exactly 1 (N files -> one turn)", *runs)
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
	handler := MiniappMethods(deps)["miniapp.capture.batch"]
	req := batchRequest(t, []map[string]any{
		{"data": b64("doc"), "mimeType": "application/pdf", "filename": "report.pdf"},
	}, "")

	resp := handler(context.Background(), req)
	if !resp.OK || *runs != 1 {
		t.Fatalf("batch capture: ok=%v runs=%d", resp.OK, *runs)
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
	handler := MiniappMethods(deps)["miniapp.capture.batch"]
	req := batchRequest(t, []map[string]any{
		{"data": b64("doc"), "mimeType": "application/pdf", "filename": "good.pdf"},
		{"data": "%%%not-base64%%%", "mimeType": "application/pdf", "filename": "bad.pdf"},
	}, "")

	resp := handler(context.Background(), req)
	if !resp.OK || *runs != 1 {
		t.Fatalf("batch capture: ok=%v runs=%d", resp.OK, *runs)
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
	handler := MiniappMethods(deps)["miniapp.capture.batch"]
	req := batchRequest(t, []map[string]any{
		{"data": "%%%bad%%%", "mimeType": "application/pdf", "filename": "bad.pdf"},
	}, "")

	resp := handler(context.Background(), req)
	if resp.OK {
		t.Fatalf("all-unreadable batch should fail, got OK: %+v", resp)
	}
	if *runs != 0 {
		t.Fatalf("no turn should run when nothing is readable, runs=%d", *runs)
	}
}

func TestCaptureBatchUsesSummaryPreviewWhenWired(t *testing.T) {
	deps, runs, msg := batchDeps(true)
	deps.SummarizePreview = func(_ context.Context, name, _ string) string {
		return "주제: " + name + " 요약\n- 핵심 사실"
	}
	handler := MiniappMethods(deps)["miniapp.capture.batch"]
	req := batchRequest(t, []map[string]any{
		{"data": b64("doc"), "mimeType": "application/pdf", "filename": "report.pdf"},
	}, "")

	resp := handler(context.Background(), req)
	if !resp.OK || *runs != 1 {
		t.Fatalf("batch capture: ok=%v runs=%d", resp.OK, *runs)
	}
	if !strings.Contains(*msg, "주제: report.pdf 요약") {
		t.Errorf("preview should be the tiny-model summary, got:\n%s", *msg)
	}
}

func TestCaptureBatchFallsBackToFrontCutWhenSummaryEmpty(t *testing.T) {
	deps, runs, msg := batchDeps(true)
	// Local model down/gated → SummarizePreview returns "" → front-cut fallback.
	deps.SummarizePreview = func(context.Context, string, string) string { return "" }
	handler := MiniappMethods(deps)["miniapp.capture.batch"]
	req := batchRequest(t, []map[string]any{
		{"data": b64("doc"), "mimeType": "application/pdf", "filename": "report.pdf"},
	}, "")

	resp := handler(context.Background(), req)
	if !resp.OK || *runs != 1 {
		t.Fatalf("batch capture: ok=%v runs=%d", resp.OK, *runs)
	}
	if !strings.Contains(*msg, "문서 추출 텍스트: report.pdf") {
		t.Errorf("empty summary should fall back to the front-cut preview, got:\n%s", *msg)
	}
}

func TestCaptureBatchMissingFilesRejected(t *testing.T) {
	deps, _, _ := batchDeps(true)
	handler := MiniappMethods(deps)["miniapp.capture.batch"]
	req := batchRequest(t, []map[string]any{}, "no files")
	if resp := handler(context.Background(), req); resp.OK {
		t.Fatalf("empty files should be rejected, got OK: %+v", resp)
	}
}

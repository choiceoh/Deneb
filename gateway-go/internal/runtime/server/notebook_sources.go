package server

// notebook_sources.go wires the notebook tool's external source ingesters
// (url/mail/diary) to real backends. Each returns readable text for a source
// ref; the tool snapshots it into the notebook at add time. The file kind
// (PDF/image OCR, text read) is handled in-package by the tool and needs no
// reader here. All readers degrade gracefully — a returned error becomes a
// user-facing "추가 실패" message, never a crash.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/core/coresecurity"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/web"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
)

// notebookFetchMaxBytes caps a fetched URL body (the tool re-clamps the text to
// its own ingest budget afterward).
const notebookFetchMaxBytes = 4 << 20 // 4 MB

// notebookFetchURL fetches a URL and returns readable text. It reuses the web
// package's SSRF-safe fetcher (web.FetchRaw → SharedClient with
// media.SSRFSafeDialer, which rejects loopback/link-local/metadata IPs at dial
// time — including across redirects) and gmail.HTMLToText for HTML→text.
// coresecurity.IsSafeURL is an early friendly reject for obviously-internal
// refs; the dialer is the authoritative guard. Matches toolctx.SourceReader.
//
// This path is reachable by a prompt-injected agent (it processes untrusted
// mail/web content), so SSRF safety here is load-bearing — do not swap in a
// plain http.Client.
func notebookFetchURL(ctx context.Context, rawURL string) (string, error) {
	u := strings.TrimSpace(rawURL)
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return "", fmt.Errorf("http(s) URL이 필요합니다: %s", rawURL)
	}
	if !coresecurity.IsSafeURL(u) {
		return "", fmt.Errorf("안전하지 않은(내부/사설망) URL이라 가져올 수 없습니다: %s", rawURL)
	}
	body, contentType, err := web.FetchRaw(ctx, u, notebookFetchMaxBytes)
	if err != nil {
		return "", err
	}
	if strings.Contains(strings.ToLower(contentType), "html") {
		return gmail.HTMLToText(string(body)), nil
	}
	return string(body), nil
}

// notebookReadMail reads a Gmail thread (by thread id) into formatted text.
// Needs Gmail auth (DefaultClient); errors surface as a graceful add failure.
// Matches toolctx.SourceReader.
func notebookReadMail(ctx context.Context, threadID string) (string, error) {
	c, err := gmail.DefaultClient()
	if err != nil {
		return "", err
	}
	msgs, err := c.GetThread(ctx, strings.TrimSpace(threadID))
	if err != nil {
		return "", err
	}
	if len(msgs) == 0 {
		return "", fmt.Errorf("스레드에 메시지가 없습니다: %s", threadID)
	}
	var sb strings.Builder
	for _, m := range msgs {
		sb.WriteString(gmail.FormatMessage(m))
		sb.WriteString("\n\n")
	}
	return sb.String(), nil
}

// notebookReadDiary reads a diary entry file under the wiki diary dir. ref is a
// date/filename ("2026-06-22" or "2026-06-22.md"); filepath.Base blocks path
// traversal so a ref can never escape the diary directory.
func notebookReadDiary(store *wiki.Store, ref string) (string, error) {
	if store == nil {
		return "", fmt.Errorf("위키/일기 저장소가 비활성입니다")
	}
	dir := store.DiaryDir()
	if dir == "" {
		return "", fmt.Errorf("일기 디렉터리가 설정되지 않았습니다")
	}
	name := filepath.Base(strings.TrimSpace(ref))
	if name == "" || name == "." || name == string(filepath.Separator) {
		return "", fmt.Errorf("일기 ref가 필요합니다 (예: 2026-06-22)")
	}
	if !strings.HasSuffix(name, ".md") {
		name += ".md"
	}
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return "", fmt.Errorf("일기 항목을 찾을 수 없습니다: %s", name)
	}
	return string(data), nil
}

// browse.go — the browse tool: read a web page through the RESIDENT headful
// browser sidecar (scripts/browser — Playwright persistent profile on an Xvfb
// display, human-shareable over noVNC). Unlike the `web` tool's plain fetch,
// the sidecar executes JS with a real browser fingerprint AND carries the
// login sessions the operator established over noVNC, so groupware/portal/
// member-only pages open server-side with no phone dependency. Read-only v1:
// navigate + settle + extract readable text.
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

const (
	// browseSidecarDefaultURL is the sidecar's loopback API (same host as the
	// gateway). Override with DENEB_BROWSE_URL for tests / remote layouts.
	browseSidecarDefaultURL = "http://127.0.0.1:18930"
	// browseTimeout bounds one page read: sidecar navigation (25s cap) +
	// settle + extraction, with headroom for a queued request ahead.
	browseTimeout = 75 * time.Second
)

func browseSidecarURL() string {
	if v := strings.TrimSpace(os.Getenv("DENEB_BROWSE_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	return browseSidecarDefaultURL
}

type browseSidecarResponse struct {
	OK        bool   `json:"ok"`
	URL       string `json:"url"`
	Title     string `json:"title"`
	Text      string `json:"text"`
	Truncated bool   `json:"truncated"`
	// Matched is the CSS-selector hit count, present only on a targeted read.
	// Pointer so "no selector sent" and "selector matched zero nodes" stay
	// distinguishable — the second one must reach the model as a statement.
	Matched *int   `json:"matched,omitempty"`
	Error   string `json:"error"`
}

// ToolBrowse reads a page via the resident browser sidecar.
func ToolBrowse() toolport.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			URL      string `json:"url"`
			WaitMs   int    `json:"wait_ms"`
			Selector string `json:"selector"`
		}
		if err := jsonutil.UnmarshalInto("browse params", input, &p); err != nil {
			return "", err
		}
		u := strings.TrimSpace(p.URL)
		if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			return "", fmt.Errorf("browse: url must be http(s), got %q", p.URL)
		}

		payload := map[string]any{"url": u, "waitMs": p.WaitMs}
		if sel := strings.TrimSpace(p.Selector); sel != "" {
			payload["selector"] = sel
		}
		body, err := json.Marshal(payload)
		if err != nil {
			return "", err
		}
		runCtx, cancel := context.WithTimeout(ctx, browseTimeout)
		defer cancel()
		req, err := http.NewRequestWithContext(runCtx, http.MethodPost,
			browseSidecarURL()+"/browse", bytes.NewReader(body))
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := httputil.NewClient(browseTimeout).Do(req)
		if err != nil {
			return "상주 브라우저 사이드카에 연결할 수 없습니다 (미기동일 수 있음 — scripts/browser/start-browser-sidecar.sh start). 공개 페이지는 web 도구로 시도하세요.", nil
		}
		defer resp.Body.Close()

		var out browseSidecarResponse
		if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
			return "", fmt.Errorf("browse: 사이드카 응답 파싱 실패: %w", err)
		}
		if !out.OK {
			msg := strings.TrimSpace(out.Error)
			if msg == "" {
				msg = fmt.Sprintf("HTTP %d", resp.StatusCode)
			}
			return "", fmt.Errorf("browse 실패: %s", msg)
		}
		if out.Matched != nil && *out.Matched == 0 {
			// An honest zero, never a silent whole-page fallback: the caller
			// scoped the read and the page has no such node — say exactly that.
			return fmt.Sprintf("[%s] %s\n\n셀렉터 %q에 일치하는 요소가 없습니다 (0건). 페이지 구조가 예상과 다릅니다 — 셀렉터를 바꾸거나 selector 없이 전체 본문을 읽으세요.",
				out.Title, out.URL, strings.TrimSpace(p.Selector)), nil
		}
		if strings.TrimSpace(out.Text) == "" {
			return "페이지가 열렸지만 본문 텍스트가 비어 있습니다 (렌더 지연·로그인 만료·빈 페이지일 수 있음). wait_ms를 늘리거나 로그인 상태를 확인하세요.", nil
		}
		var sb strings.Builder
		fmt.Fprintf(&sb, "[%s] %s", out.Title, out.URL)
		if out.Matched != nil {
			fmt.Fprintf(&sb, "\n(셀렉터 일치 %d건만 추출)", *out.Matched)
		}
		fmt.Fprintf(&sb, "\n\n%s", out.Text)
		if out.Truncated {
			sb.WriteString("\n\n[본문이 길어 16,000자에서 잘렸습니다]")
		}
		return sb.String(), nil
	}
}

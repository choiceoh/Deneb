// browser.go — Page Agent bridge client: operate the user's real Chrome tabs.
//
// Complements `web` (server-side HTTP fetch). This tool talks to a workstation
// bridge (scripts/dev/page-agent-bridge) that owns the Page Agent Chrome
// extension hub — so the agent inherits the user's logged-in sessions and can
// click/type through SPAs that `web` cannot render.
//
// Opt-in via DENEB_BROWSER_URL (+ DENEB_BROWSER_TOKEN). Unconfigured → calm
// "off" message, same pattern as fleet.
package runtimeops

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// Page Agent tasks often need many ReAct steps; keep the HTTP client patient
// but still cancelable via the turn context.
const browserToolTimeout = 10 * time.Minute

var browserToolHTTP = httputil.NewClient(browserToolTimeout)

// ToolBrowser drives the Page Agent workstation bridge.
func ToolBrowser(d *tooldeps.BrowserDeps) toolport.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Action string `json:"action"`
			Task   string `json:"task"`
		}
		if err := jsonutil.UnmarshalInto("browser params", input, &p); err != nil {
			return "", err
		}

		base := ""
		if d != nil && d.BaseURL != nil {
			base = strings.TrimRight(strings.TrimSpace(d.BaseURL()), "/")
		}
		if base == "" {
			return "브라우저 연동이 꺼져 있습니다 (게이트웨이에 DENEB_BROWSER_URL 미설정). " +
				"워크스테이션에서 scripts/dev/page-agent-bridge 를 띄우고 URL/토큰을 설정하라.", nil
		}
		token := ""
		if d != nil && d.Token != nil {
			token = d.Token()
		}
		bc := &browserCaller{base: base, token: token}

		switch strings.ToLower(strings.TrimSpace(p.Action)) {
		case "", "status":
			return browserStatus(ctx, bc)
		case "execute":
			task := strings.TrimSpace(p.Task)
			if task == "" {
				return "task가 필요합니다 — 브라우저에서 수행할 자연어 지시문을 넣으세요.", nil
			}
			return browserExecute(ctx, bc, task)
		case "stop":
			return browserStop(ctx, bc)
		default:
			return "", fmt.Errorf("unknown browser action %q (status|execute|stop)", p.Action)
		}
	}
}

type browserCaller struct {
	base  string
	token string
}

func (c *browserCaller) do(ctx context.Context, method, path string, body io.Reader) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return nil, 0, err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("X-Deneb-Browser-Token", c.token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := browserToolHTTP.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return nil, res.StatusCode, err
	}
	return raw, res.StatusCode, nil
}

func browserStatus(ctx context.Context, c *browserCaller) (string, error) {
	raw, code, err := c.do(ctx, http.MethodGet, "/v1/status", nil)
	if err != nil {
		return fmt.Sprintf("브라우저 브리지 연결 실패: %v", err), nil
	}
	if code == http.StatusUnauthorized {
		return "브라우저 브리지 인증 실패 — DENEB_BROWSER_TOKEN 을 확인하세요.", nil
	}
	if code >= 400 {
		return fmt.Sprintf("브라우저 status HTTP %d: %s", code, strings.TrimSpace(string(raw))), nil
	}
	var st struct {
		Connected bool `json:"connected"`
		Busy      bool `json:"busy"`
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		return fmt.Sprintf("브라우저 status 파싱 실패: %v\n%s", err, string(raw)), nil
	}
	hub := "끊김"
	if st.Connected {
		hub = "연결됨"
	}
	busy := "대기"
	if st.Busy {
		busy = "작업 중"
	}
	return fmt.Sprintf("브라우저 브리지: 허브 %s · %s", hub, busy), nil
}

func browserExecute(ctx context.Context, c *browserCaller, task string) (string, error) {
	payload, _ := json.Marshal(map[string]string{"task": task})
	raw, code, err := c.do(ctx, http.MethodPost, "/v1/execute", bytes.NewReader(payload))
	if err != nil {
		return fmt.Sprintf("브라우저 execute 실패: %v", err), nil
	}
	if code == http.StatusUnauthorized {
		return "브라우저 브리지 인증 실패 — DENEB_BROWSER_TOKEN 을 확인하세요.", nil
	}
	if code >= 400 {
		var errBody struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(raw, &errBody) == nil && errBody.Error != "" {
			return fmt.Sprintf("브라우저 execute 오류: %s", errBody.Error), nil
		}
		return fmt.Sprintf("브라우저 execute HTTP %d: %s", code, strings.TrimSpace(string(raw))), nil
	}
	var result struct {
		Success bool   `json:"success"`
		Data    string `json:"data"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Sprintf("브라우저 execute 파싱 실패: %v\n%s", err, string(raw)), nil
	}
	if result.Success {
		if strings.TrimSpace(result.Data) == "" {
			return "브라우저 작업 완료.", nil
		}
		return "브라우저 작업 완료.\n\n" + result.Data, nil
	}
	if strings.TrimSpace(result.Data) == "" {
		return "브라우저 작업 실패.", nil
	}
	return "브라우저 작업 실패.\n\n" + result.Data, nil
}

func browserStop(ctx context.Context, c *browserCaller) (string, error) {
	raw, code, err := c.do(ctx, http.MethodPost, "/v1/stop", nil)
	if err != nil {
		return fmt.Sprintf("브라우저 stop 실패: %v", err), nil
	}
	if code == http.StatusUnauthorized {
		return "브라우저 브리지 인증 실패 — DENEB_BROWSER_TOKEN 을 확인하세요.", nil
	}
	if code >= 400 {
		return fmt.Sprintf("브라우저 stop HTTP %d: %s", code, strings.TrimSpace(string(raw))), nil
	}
	return "브라우저 작업 중지 신호를 보냈습니다.", nil
}

// ApprovalBrowserEnrich reads an electronic-approval document through the Page
// Agent workstation bridge. The gateway (srv4) orchestrates; the user's logged-in
// Chrome on the workstation executes. Returns "" when the bridge is off,
// disconnected, busy, or the read fails — callers then fall back to the phone
// notification text alone. Never errors: proactive ingest must not abort on a
// browser hiccup.
//
// groupwareURL is the Douzone/Amaranth web base (e.g. https://tsgw.topsolar.kr);
// empty falls back to a generic "open groupware" instruction.
func ApprovalBrowserEnrich(ctx context.Context, baseURL, token, groupwareURL, source, text string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return ""
	}
	c := &browserCaller{base: base, token: token}

	statusCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	raw, code, err := c.do(statusCtx, http.MethodGet, "/v1/status", nil)
	if err != nil || code >= 400 {
		return ""
	}
	var st struct {
		Connected bool `json:"connected"`
		Busy      bool `json:"busy"`
	}
	if json.Unmarshal(raw, &st) != nil || !st.Connected || st.Busy {
		return ""
	}

	out, _ := browserExecute(ctx, c, buildApprovalReadTask(groupwareURL, source, text))
	out = strings.TrimSpace(out)
	if out == "" {
		return ""
	}
	// Transport / auth / execute failures stay as calm tool strings — treat them
	// as "no enrichment" so the judgment turn still runs on the notification.
	if strings.HasPrefix(out, "브라우저 ") && !strings.HasPrefix(out, "브라우저 작업 완료") {
		return ""
	}
	out = strings.TrimPrefix(out, "브라우저 작업 완료.\n\n")
	out = strings.TrimPrefix(out, "브라우저 작업 완료.")
	return strings.TrimSpace(out)
}

func buildApprovalReadTask(groupwareURL, source, text string) string {
	src := strings.TrimSpace(source)
	if src == "" {
		src = "그룹웨어"
	}
	site := strings.TrimSpace(groupwareURL)
	var open string
	if site != "" {
		open = fmt.Sprintf("%s 로 이동해 전자결재(아마란스)를 연 뒤", site)
	} else {
		open = "전자결재(아마란스/그룹웨어) 화면을 연 뒤"
	}
	return fmt.Sprintf(
		"%s 아래 알림에 해당하는 문서를 찾고, "+
			"본문·금액·결재선·첨부·마감/긴급도를 한국어로 요약하라. "+
			"결재를 승인·반려·상신하지 말고 읽기만 하라.\n\n출처: %s\n알림:\n%s",
		open, src, strings.TrimSpace(text),
	)
}

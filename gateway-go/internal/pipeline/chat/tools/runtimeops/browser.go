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

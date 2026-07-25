// Package browserbridge talks to the workstation Page Agent bridge that owns
// the user's logged-in Chrome session.
package browserbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
)

// Page Agent tasks often need many ReAct steps; keep the HTTP client patient
// but still cancelable via the turn context.
const defaultTimeout = 10 * time.Minute

var defaultHTTPClient = httputil.NewClient(defaultTimeout)

// Client is the narrow bridge contract shared by the browser tool and phone
// event approval enrichment.
type Client struct {
	base  string
	token string
}

// New returns a client for a Page Agent bridge base URL. An empty base creates
// an unconfigured client; callers can check Configured before issuing requests.
func New(baseURL, token string) *Client {
	return &Client{
		base:  strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		token: strings.TrimSpace(token),
	}
}

// Configured reports whether the bridge has a usable base URL.
func (c *Client) Configured() bool {
	return c != nil && c.base != ""
}

func (c *Client) do(ctx context.Context, method, path string, body io.Reader) ([]byte, int, error) {
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
	res, err := defaultHTTPClient.Do(req)
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

// Status returns the agent-facing bridge status string.
func (c *Client) Status(ctx context.Context) string {
	raw, code, err := c.do(ctx, http.MethodGet, "/v1/status", nil)
	if err != nil {
		return fmt.Sprintf("브라우저 브리지 연결 실패: %v", err)
	}
	if code == http.StatusUnauthorized {
		return "브라우저 브리지 인증 실패 — DENEB_BROWSER_TOKEN 을 확인하세요."
	}
	if code >= 400 {
		return fmt.Sprintf("브라우저 status HTTP %d: %s", code, strings.TrimSpace(string(raw)))
	}
	var st struct {
		Connected bool `json:"connected"`
		Busy      bool `json:"busy"`
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		return fmt.Sprintf("브라우저 status 파싱 실패: %v\n%s", err, string(raw))
	}
	hub := "끊김"
	if st.Connected {
		hub = "연결됨"
	}
	busy := "대기"
	if st.Busy {
		busy = "작업 중"
	}
	return fmt.Sprintf("브라우저 브리지: 허브 %s · %s", hub, busy)
}

// Execute submits a Page Agent task and returns the agent-facing result string.
func (c *Client) Execute(ctx context.Context, task string) string {
	payload, _ := json.Marshal(map[string]string{"task": task})
	raw, code, err := c.do(ctx, http.MethodPost, "/v1/execute", bytes.NewReader(payload))
	if err != nil {
		return fmt.Sprintf("브라우저 execute 실패: %v", err)
	}
	if code == http.StatusUnauthorized {
		return "브라우저 브리지 인증 실패 — DENEB_BROWSER_TOKEN 을 확인하세요."
	}
	if code >= 400 {
		var errBody struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(raw, &errBody) == nil && errBody.Error != "" {
			return fmt.Sprintf("브라우저 execute 오류: %s", errBody.Error)
		}
		return fmt.Sprintf("브라우저 execute HTTP %d: %s", code, strings.TrimSpace(string(raw)))
	}
	var result struct {
		Success bool   `json:"success"`
		Data    string `json:"data"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Sprintf("브라우저 execute 파싱 실패: %v\n%s", err, string(raw))
	}
	if result.Success {
		if strings.TrimSpace(result.Data) == "" {
			return "브라우저 작업 완료."
		}
		return "브라우저 작업 완료.\n\n" + result.Data
	}
	if strings.TrimSpace(result.Data) == "" {
		return "브라우저 작업 실패."
	}
	return "브라우저 작업 실패.\n\n" + result.Data
}

// Stop asks the bridge to cancel the current Page Agent task.
func (c *Client) Stop(ctx context.Context) string {
	raw, code, err := c.do(ctx, http.MethodPost, "/v1/stop", nil)
	if err != nil {
		return fmt.Sprintf("브라우저 stop 실패: %v", err)
	}
	if code == http.StatusUnauthorized {
		return "브라우저 브리지 인증 실패 — DENEB_BROWSER_TOKEN 을 확인하세요."
	}
	if code >= 400 {
		return fmt.Sprintf("브라우저 stop HTTP %d: %s", code, strings.TrimSpace(string(raw)))
	}
	return "브라우저 작업 중지 신호를 보냈습니다."
}

// ApprovalEnrich reads an electronic-approval document through the Page Agent
// bridge. Returns "" when the bridge is off, disconnected, busy, or the read
// fails so callers can fall back to the phone notification text alone.
func (c *Client) ApprovalEnrich(ctx context.Context, groupwareURL, source, text string) string {
	if !c.Configured() {
		return ""
	}
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

	out := c.Execute(ctx, buildApprovalReadTask(groupwareURL, source, text))
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

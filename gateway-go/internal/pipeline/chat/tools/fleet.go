package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// --- fleet tool ---
//
// The agent's hand on the SparkFleet control plane — the sibling service that
// launches and monitors the GPU model servers Deneb itself runs on. It speaks
// the same REST surface the native 플릿 tab uses (via the gateway's
// authenticated passthrough), so the chief-of-staff can answer "is the fleet
// ok / why did qwen36 die / restart it" without leaving chat. Mutating actions
// (launch/stop/restart/cancel) are real and immediate — the same ones the app
// exposes, authorized by the version-controlled recipe files.

const fleetToolTimeout = 45 * time.Second

var fleetToolHTTP = httputil.NewClient(fleetToolTimeout)

// minimal mirrors of SparkFleet's JSON responses (unknown fields ignored).
type fleetNodeView struct {
	Name      string `json:"name"`
	Role      string `json:"role"`
	Reachable bool   `json:"reachable"`
	Metrics   struct {
		GPUs []struct {
			UtilPct *int `json:"utilPct"`
			TempC   *int `json:"tempC"`
		} `json:"gpus"`
		Memory *struct {
			TotalKB     int64 `json:"totalKB"`
			AvailableKB int64 `json:"availableKB"`
		} `json:"memory"`
		Services []struct {
			Name string `json:"name"`
			OK   bool   `json:"ok"`
		} `json:"services"`
	} `json:"metrics"`
}

type fleetStateView struct {
	Nodes []fleetNodeView `json:"nodes"`
}

type fleetRecipeView struct {
	Name      string `json:"name"`
	Node      string `json:"node"`
	Container string `json:"container"`
	Port      int    `json:"port"`
	Status    struct {
		Running        bool   `json:"running"`
		WeightsPresent bool   `json:"weightsPresent"`
		Node           string `json:"node"`
	} `json:"status"`
}

func (r fleetRecipeView) runNode() string {
	if r.Status.Node != "" {
		return r.Status.Node
	}
	return r.Node
}

type fleetJobView struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	State string `json:"state"`
}

type fleetDiagnosisView struct {
	Container string `json:"container"`
	State     string `json:"state"`
	Findings  []struct {
		Cause string `json:"cause"`
		Fix   string `json:"fix"`
	} `json:"findings"`
	LLM string `json:"llm"`
}

// ToolFleet manages the SparkFleet control plane. A nil/empty base URL means the
// integration is off, in which case every action returns a calm "off" message.
func ToolFleet(d *toolctx.FleetDeps) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Action string `json:"action"`
			Recipe string `json:"recipe"`
			JobID  string `json:"jobId"`
		}
		if err := jsonutil.UnmarshalInto("fleet params", input, &p); err != nil {
			return "", err
		}

		base := ""
		if d != nil && d.BaseURL != nil {
			base = strings.TrimRight(strings.TrimSpace(d.BaseURL()), "/")
		}
		if base == "" {
			return "플릿 연동이 꺼져 있습니다 (게이트웨이에 DENEB_SPARKFLEET_URL 미설정).", nil
		}
		token := ""
		if d.Token != nil {
			token = d.Token()
		}
		fc := &fleetCaller{base: base, token: token}

		switch strings.ToLower(strings.TrimSpace(p.Action)) {
		case "", "status":
			return fleetStatus(ctx, fc)
		case "recipes":
			return fleetRecipesList(ctx, fc)
		case "jobs":
			return fleetJobsList(ctx, fc)
		case "launch", "stop", "restart":
			return fleetRecipeAction(ctx, fc, strings.ToLower(p.Action), strings.TrimSpace(p.Recipe))
		case "cancel":
			return fleetCancel(ctx, fc, strings.TrimSpace(p.JobID))
		case "diagnose":
			return fleetDiagnose(ctx, fc, strings.TrimSpace(p.Recipe))
		default:
			return "", fmt.Errorf("unknown fleet action %q (status|recipes|jobs|launch|stop|restart|cancel|diagnose)", p.Action)
		}
	}
}

// --- HTTP ---

type fleetCaller struct {
	base  string
	token string
}

func (c *fleetCaller) do(ctx context.Context, method, path string, body io.Reader) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("X-Fleet-Token", c.token)
	}
	resp, err := fleetToolHTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sparkfleet unreachable: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("sparkfleet %s %s: HTTP %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return data, nil
}

func (c *fleetCaller) getJSON(ctx context.Context, path string, out any) error {
	data, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func (c *fleetCaller) postJSON(ctx context.Context, path string, body []byte, out any) error {
	data, err := c.do(ctx, http.MethodPost, path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

// --- actions ---

func fleetStatus(ctx context.Context, fc *fleetCaller) (string, error) {
	var st fleetStateView
	if err := fc.getJSON(ctx, "/api/state", &st); err != nil {
		return "", err
	}
	var recipes []fleetRecipeView
	_ = fc.getJSON(ctx, "/api/recipes", &recipes) // best-effort; nodes alone are still useful
	var jobs []fleetJobView
	_ = fc.getJSON(ctx, "/api/jobs", &jobs)

	var b strings.Builder
	b.WriteString("# 플릿 상태\n\n## 노드\n")
	for _, n := range st.Nodes {
		state := "온라인"
		if !n.Reachable {
			state = "응답 없음"
		}
		b.WriteString(fmt.Sprintf("- %s (%s) — %s", n.Name, fleetDash(n.Role), state))
		if len(n.Metrics.GPUs) > 0 {
			g := n.Metrics.GPUs[0]
			b.WriteString(fmt.Sprintf(", GPU %s%% %s℃", fleetIntp(g.UtilPct), fleetIntp(g.TempC)))
		}
		if m := n.Metrics.Memory; m != nil && m.TotalKB > 0 {
			b.WriteString(fmt.Sprintf(", 메모리 %s/%s GiB", fleetGiB(m.TotalKB-m.AvailableKB), fleetGiB(m.TotalKB)))
		}
		var down []string
		for _, s := range n.Metrics.Services {
			if !s.OK {
				down = append(down, s.Name)
			}
		}
		if len(down) > 0 {
			b.WriteString(" ⚠ 다운: " + strings.Join(down, ", "))
		}
		b.WriteByte('\n')
	}
	if len(st.Nodes) == 0 {
		b.WriteString("- (노드 없음)\n")
	}

	running := 0
	for _, r := range recipes {
		if r.Status.Running {
			running++
		}
	}
	b.WriteString(fmt.Sprintf("\n## 레시피: %d개 (실행 중 %d · 중지 %d)\n", len(recipes), running, len(recipes)-running))
	for _, r := range recipes {
		b.WriteString("- " + fleetRecipeLine(r) + "\n")
	}

	var failed []fleetJobView
	for _, j := range jobs {
		if j.State == "failed" {
			failed = append(failed, j)
		}
	}
	if len(failed) > 0 {
		b.WriteString(fmt.Sprintf("\n## 최근 실패 작업 %d개\n", len(failed)))
		for i, j := range failed {
			if i >= 5 {
				break
			}
			b.WriteString(fmt.Sprintf("- %s (id: %s)\n", j.Title, j.ID))
		}
	}
	return b.String(), nil
}

func fleetRecipesList(ctx context.Context, fc *fleetCaller) (string, error) {
	var recipes []fleetRecipeView
	if err := fc.getJSON(ctx, "/api/recipes", &recipes); err != nil {
		return "", err
	}
	if len(recipes) == 0 {
		return "레시피가 없습니다.", nil
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("레시피 %d개:\n", len(recipes)))
	for _, r := range recipes {
		b.WriteString("- " + fleetRecipeLine(r) + "\n")
	}
	return b.String(), nil
}

func fleetJobsList(ctx context.Context, fc *fleetCaller) (string, error) {
	var jobs []fleetJobView
	if err := fc.getJSON(ctx, "/api/jobs", &jobs); err != nil {
		return "", err
	}
	if len(jobs) == 0 {
		return "진행 중이거나 최근 작업이 없습니다.", nil
	}
	var b strings.Builder
	b.WriteString("최근 작업:\n")
	for i, j := range jobs {
		if i >= 15 {
			break
		}
		b.WriteString(fmt.Sprintf("- [%s] %s (id: %s)\n", fleetDash(j.State), j.Title, j.ID))
	}
	return b.String(), nil
}

func fleetRecipeAction(ctx context.Context, fc *fleetCaller, action, recipe string) (string, error) {
	if recipe == "" {
		return fmt.Sprintf("%s 하려면 recipe 이름이 필요합니다 (action=recipes 로 목록 확인).", action), nil
	}
	body, _ := json.Marshal(map[string]string{"recipe": recipe, "action": action})
	var r struct {
		JobID string `json:"jobId"`
	}
	if err := fc.postJSON(ctx, "/api/recipes/action", body, &r); err != nil {
		return "", err
	}
	if r.JobID != "" {
		return fmt.Sprintf("%s — %s 시작됨. 작업 id %s (action=jobs 로 진행 확인).", recipe, action, r.JobID), nil
	}
	return fmt.Sprintf("%s — %s 완료.", recipe, action), nil
}

func fleetCancel(ctx context.Context, fc *fleetCaller, jobID string) (string, error) {
	if jobID == "" {
		return "취소하려면 jobId 가 필요합니다 (action=jobs 로 id 확인).", nil
	}
	if err := fc.postJSON(ctx, "/api/jobs/"+url.PathEscape(jobID)+"/cancel", []byte("{}"), nil); err != nil {
		return "", err
	}
	return fmt.Sprintf("작업 %s 취소됨 (전송류는 재시도 시 이어받음).", jobID), nil
}

func fleetDiagnose(ctx context.Context, fc *fleetCaller, recipe string) (string, error) {
	if recipe == "" {
		return "진단하려면 recipe 이름이 필요합니다.", nil
	}
	var recipes []fleetRecipeView
	if err := fc.getJSON(ctx, "/api/recipes", &recipes); err != nil {
		return "", err
	}
	var target *fleetRecipeView
	for i := range recipes {
		if recipes[i].Name == recipe {
			target = &recipes[i]
			break
		}
	}
	if target == nil {
		return fmt.Sprintf("레시피 %q 를 찾을 수 없습니다.", recipe), nil
	}
	if target.Container == "" {
		return fmt.Sprintf("레시피 %q 에 연결된 컨테이너가 없습니다.", recipe), nil
	}
	body, _ := json.Marshal(map[string]string{"node": target.runNode(), "container": target.Container})
	var d fleetDiagnosisView
	if err := fc.postJSON(ctx, "/api/assist/logs", body, &d); err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("# %s 진단 (%s)\n", recipe, d.Container))
	if d.State != "" {
		b.WriteString("상태: " + d.State + "\n")
	}
	for _, f := range d.Findings {
		b.WriteString("- " + f.Cause)
		if f.Fix != "" {
			b.WriteString(" → " + f.Fix)
		}
		b.WriteByte('\n')
	}
	if d.LLM != "" {
		b.WriteString("\nAI 분석:\n" + d.LLM + "\n")
	}
	if len(d.Findings) == 0 && d.LLM == "" {
		b.WriteString("알려진 실패 패턴이 없습니다.\n")
	}
	return b.String(), nil
}

// --- formatting helpers ---

func fleetRecipeLine(r fleetRecipeView) string {
	flag := "○ 중지"
	if r.Status.Running {
		flag = "● 실행"
	}
	line := fmt.Sprintf("%s  %s", flag, r.Name)
	loc := r.runNode()
	if loc != "" {
		line += " @" + loc
	}
	if r.Port > 0 {
		line += fmt.Sprintf(":%d", r.Port)
	}
	if !r.Status.Running && !r.Status.WeightsPresent {
		line += " (가중치 없음)"
	}
	return line
}

func fleetDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func fleetIntp(p *int) string {
	if p == nil {
		return "—"
	}
	return fmt.Sprintf("%d", *p)
}

func fleetGiB(kb int64) string {
	return fmt.Sprintf("%.0f", float64(kb)/1024.0/1024.0)
}

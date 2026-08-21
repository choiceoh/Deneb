// browser.go — Page Agent bridge tool: operate the user's real Chrome tabs.
//
// Complements `web` (server-side HTTP fetch). This tool talks to a workstation
// bridge (scripts/dev/page-agent-bridge) that owns the Page Agent Chrome
// extension hub — so the agent inherits the user's logged-in sessions and can
// click/type through SPAs that `web` cannot render.
//
// Opt-in via DENEB_BROWSER_URL (+ DENEB_BROWSER_TOKEN). Unconfigured → calm
// "off" message, same pattern as fleet.
package hostops

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/browserbridge"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

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
		bc := browserbridge.New(base, token)

		switch strings.ToLower(strings.TrimSpace(p.Action)) {
		case "", "status":
			return bc.Status(ctx), nil
		case "execute":
			task := strings.TrimSpace(p.Task)
			if task == "" {
				return "task가 필요합니다 — 브라우저에서 수행할 자연어 지시문을 넣으세요.", nil
			}
			return bc.Execute(ctx, task), nil
		case "stop":
			return bc.Stop(ctx), nil
		default:
			return "", fmt.Errorf("unknown browser action %q (status|execute|stop)", p.Action)
		}
	}
}

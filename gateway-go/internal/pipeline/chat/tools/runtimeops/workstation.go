// workstation.go — the workstation tool: Deneb drives the desktop client's
// tiled workspace (Andromeda) over the events push channel.
//
// The tool validates a screen-arrangement verb and hands it to
// WorkstationCommandFunc (the server backs it with the client push hub; see
// server_workstation.go). Delivery is fire-and-forget: the desktop client
// re-validates the command (its command-bus parser drops anything malformed),
// executes it, and surfaces a visible "화면 조정" nudge — so the model gets an
// immediate success once the frame is published to a connected desktop.
package runtimeops

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
)

// WorkstationCommandFunc delivers a validated workspace command to connected
// desktop workstations. nil error = the frame was published to at least one
// desktop client; an error means no desktop is connected (or the hub is down).
type WorkstationCommandFunc func(ctx context.Context, action string, args map[string]string) error

// workstationParams is the workstation tool input.
type workstationParams struct {
	Action string `json:"action"`
	View   string `json:"view"`
	Views  string `json:"views"`
	Ref    string `json:"ref"`
	Path   string `json:"path"`
	Query  string `json:"query"`
}

// workstationActions is the screen-verb allowlist — arrangement only, nothing
// that types into the chat or mutates data. Mirrors the desktop command bus
// (andromeda/src/commands.ts).
var workstationActions = map[string]bool{
	"open":   true,
	"split":  true,
	"close":  true,
	"focus":  true,
	"layout": true,
	"wiki":   true,
}

// buildWorkstationCommand validates params against the allowlist and returns
// the (action, args) frame for the desktop. Pure — unit-testable without the
// push channel.
func buildWorkstationCommand(p workstationParams) (string, map[string]string, error) {
	action := strings.ToLower(strings.TrimSpace(p.Action))
	if !workstationActions[action] {
		return "", nil, fmt.Errorf("workstation: unknown action=%q (open|split|close|focus|layout|wiki)", p.Action)
	}
	view := strings.TrimSpace(p.View)
	views := strings.TrimSpace(p.Views)
	ref := strings.TrimSpace(p.Ref)
	path := strings.TrimSpace(p.Path)
	query := strings.TrimSpace(p.Query)

	switch action {
	case "open", "split", "focus":
		if view == "" && !(action == "open" && path != "") {
			return "", nil, fmt.Errorf("workstation: action=%s needs view (pane key)", action)
		}
	case "layout":
		if views == "" {
			return "", nil, fmt.Errorf(`workstation: action=layout needs views (comma-separated pane keys, e.g. "mail,calendar")`)
		}
	case "wiki":
		if path == "" && ref == "" {
			return "", nil, fmt.Errorf("workstation: action=wiki needs path (위키 페이지 경로)")
		}
	}

	args := make(map[string]string, 5)
	if view != "" {
		args["view"] = view
	}
	if views != "" {
		args["views"] = views
	}
	if ref != "" {
		args["ref"] = ref
	}
	if path != "" {
		args["path"] = path
	}
	if query != "" {
		args["query"] = query
	}
	return action, args, nil
}

// ToolWorkstation creates the workstation tool handler. A nil send reports the
// capability as unavailable (no desktop push channel wired).
func ToolWorkstation(send WorkstationCommandFunc) toolport.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p workstationParams
		if err := jsonutil.UnmarshalInto("workstation params", input, &p); err != nil {
			return "", err
		}
		action, args, err := buildWorkstationCommand(p)
		if err != nil {
			return "", err
		}
		if send == nil {
			return "", fmt.Errorf("workstation control channel unavailable")
		}
		if err := send(ctx, action, args); err != nil {
			return "", err
		}
		return fmt.Sprintf("데스크톱 워크스테이션에 화면 명령을 보냈습니다 (%s). 클라이언트가 즉시 반영하고 '화면 조정' 알림으로 표시합니다.", action), nil
	}
}

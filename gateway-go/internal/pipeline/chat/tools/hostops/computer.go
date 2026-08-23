// computer.go — the computer tool: Deneb operates the user's desktop (mouse,
// keyboard, screen) through the Andromeda workstation shell.
//
// Sibling of `workstation` (which arranges Andromeda's OWN panes). This one
// reaches past the app to the host OS: screenshot → describe, click/type/key/
// scroll at screenshot coordinates. The tool validates the verb + arguments
// and hands the frame to ComputerCommandFunc; the server owns transport (events
// push channel, Kind=computer), the result round trip (the desktop reports
// back over miniapp.computer.result) and the screenshot → text description
// (tool results are text — the vision chain reads the capture for the model).
package hostops

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// ComputerCommandFunc executes one validated desktop command and returns the
// text the model should see (an action ack, or the screenshot description).
// An error means the command could not run: no desktop connected, computer use
// disabled in the desktop app, or the desktop reported a failure.
type ComputerCommandFunc func(ctx context.Context, action string, args map[string]string) (string, error)

// computerParams is the computer tool input. Coordinates are in the pixel
// space of the LAST screenshot (the desktop maps them back to the real screen,
// including HiDPI/downscale), so the model reasons in one frame only.
type computerParams struct {
	Action string `json:"action"`
	X      *int   `json:"x"`
	Y      *int   `json:"y"`
	ToX    *int   `json:"to_x"`
	ToY    *int   `json:"to_y"`
	Width  *int   `json:"width"`
	Height *int   `json:"height"`
	Text   string `json:"text"`
	Key    string `json:"key"`
	Button string `json:"button"`
	Scroll string `json:"scroll"`
	Amount *int   `json:"amount"`
	WaitMs *int   `json:"wait_ms"`
}

// computerActions is the verb allowlist — mirrored by the desktop executor
// (andromeda/src/computer.ts + src-tauri/src/computer.rs), which re-validates.
var computerActions = map[string]bool{
	"screenshot":   true,
	"click":        true,
	"double_click": true,
	"right_click":  true,
	"move":         true,
	"drag":         true,
	"scroll":       true,
	"type":         true,
	"key":          true,
	"wait":         true,
	"cursor":       true,
}

const (
	computerMaxTypeRunes = 4000
	computerMaxWaitMs    = 10000
	computerMaxScroll    = 50
	computerMaxCoord     = 16384
)

var (
	computerButtons    = map[string]bool{"left": true, "right": true, "middle": true}
	computerScrollDirs = map[string]bool{"up": true, "down": true, "left": true, "right": true}
)

func needCoord(action, name string, v *int) (string, error) {
	if v == nil {
		return "", fmt.Errorf("computer: action=%s needs %s (screenshot pixel coordinate)", action, name)
	}
	if *v < 0 || *v > computerMaxCoord {
		return "", fmt.Errorf("computer: %s=%d out of range (0..%d)", name, *v, computerMaxCoord)
	}
	return strconv.Itoa(*v), nil
}

// buildComputerCommand validates params against the allowlist and returns the
// (action, args) frame for the desktop. Pure — unit-testable without the push
// channel. Every value is a string because the push frame's Data map is.
func buildComputerCommand(p computerParams) (string, map[string]string, error) {
	action := strings.ToLower(strings.TrimSpace(p.Action))
	if !computerActions[action] {
		return "", nil, fmt.Errorf("computer: unknown action=%q (screenshot|click|double_click|right_click|move|drag|scroll|type|key|wait|cursor)", p.Action)
	}
	args := make(map[string]string, 6)
	setXY := func() error {
		x, err := needCoord(action, "x", p.X)
		if err != nil {
			return err
		}
		y, err := needCoord(action, "y", p.Y)
		if err != nil {
			return err
		}
		args["x"], args["y"] = x, y
		return nil
	}
	switch action {
	case "screenshot":
		// Optional zoom region: all four or none.
		set := 0
		for _, v := range []*int{p.X, p.Y, p.Width, p.Height} {
			if v != nil {
				set++
			}
		}
		if set != 0 && set != 4 {
			return "", nil, fmt.Errorf("computer: screenshot zoom needs x, y, width, height together")
		}
		if set == 4 {
			if err := setXY(); err != nil {
				return "", nil, err
			}
			if *p.Width <= 0 || *p.Height <= 0 || *p.Width > computerMaxCoord || *p.Height > computerMaxCoord {
				return "", nil, fmt.Errorf("computer: screenshot zoom width/height must be positive")
			}
			args["width"], args["height"] = strconv.Itoa(*p.Width), strconv.Itoa(*p.Height)
		}
	case "click", "double_click", "right_click", "move":
		if err := setXY(); err != nil {
			return "", nil, err
		}
		if action == "click" {
			b := strings.ToLower(strings.TrimSpace(p.Button))
			if b == "" {
				b = "left"
			}
			if !computerButtons[b] {
				return "", nil, fmt.Errorf("computer: button must be left|right|middle (got %q)", p.Button)
			}
			args["button"] = b
		}
	case "drag":
		if err := setXY(); err != nil {
			return "", nil, err
		}
		tx, err := needCoord(action, "to_x", p.ToX)
		if err != nil {
			return "", nil, err
		}
		ty, err := needCoord(action, "to_y", p.ToY)
		if err != nil {
			return "", nil, err
		}
		args["to_x"], args["to_y"] = tx, ty
	case "scroll":
		if err := setXY(); err != nil {
			return "", nil, err
		}
		dir := strings.ToLower(strings.TrimSpace(p.Scroll))
		if dir == "" {
			dir = "down"
		}
		if !computerScrollDirs[dir] {
			return "", nil, fmt.Errorf("computer: scroll must be up|down|left|right (got %q)", p.Scroll)
		}
		amount := 3
		if p.Amount != nil {
			amount = *p.Amount
		}
		if amount <= 0 || amount > computerMaxScroll {
			return "", nil, fmt.Errorf("computer: amount must be 1..%d (got %d)", computerMaxScroll, amount)
		}
		args["scroll"] = dir
		args["amount"] = strconv.Itoa(amount)
	case "type":
		if p.Text == "" {
			return "", nil, fmt.Errorf("computer: action=type needs text")
		}
		if n := len([]rune(p.Text)); n > computerMaxTypeRunes {
			return "", nil, fmt.Errorf("computer: text too long (%d runes, max %d) — split it", n, computerMaxTypeRunes)
		}
		args["text"] = p.Text
	case "key":
		k := strings.TrimSpace(p.Key)
		if k == "" {
			return "", nil, fmt.Errorf(`computer: action=key needs key (e.g. "Return", "ctrl+c", "cmd+shift+t")`)
		}
		args["key"] = k
	case "wait":
		ms := 1000
		if p.WaitMs != nil {
			ms = *p.WaitMs
		}
		if ms <= 0 || ms > computerMaxWaitMs {
			return "", nil, fmt.Errorf("computer: wait_ms must be 1..%d (got %d)", computerMaxWaitMs, ms)
		}
		args["wait_ms"] = strconv.Itoa(ms)
	case "cursor":
		// no args: reports the pointer position
	}
	return action, args, nil
}

// ToolComputer builds the computer tool executor. send is nil when no desktop
// channel is wired.
func ToolComputer(send ComputerCommandFunc) toolport.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p computerParams
		if err := jsonutil.UnmarshalInto("computer params", input, &p); err != nil {
			return "", err
		}
		action, args, err := buildComputerCommand(p)
		if err != nil {
			return "", err
		}
		if send == nil {
			return "", fmt.Errorf("computer: 데스크톱 채널이 배선되지 않아 컴퓨터를 조종할 수 없습니다")
		}
		out, err := send(ctx, action, args)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(out) == "" {
			out = "컴퓨터 조작 완료: " + DescribeComputerCommand(action, args)
		}
		return out, nil
	}
}

// DescribeComputerCommand renders a short Korean phrase for acks and logs.
// Exported so the server's dispatcher can label its own messages identically.
func DescribeComputerCommand(action string, args map[string]string) string {
	at := func() string { return "(" + args["x"] + "," + args["y"] + ")" }
	switch action {
	case "screenshot":
		if args["width"] != "" {
			return "스크린샷(확대 " + at() + " " + args["width"] + "×" + args["height"] + ")"
		}
		return "스크린샷"
	case "click":
		if b := args["button"]; b != "" && b != "left" {
			return b + " 클릭 " + at()
		}
		return "클릭 " + at()
	case "double_click":
		return "더블클릭 " + at()
	case "right_click":
		return "우클릭 " + at()
	case "move":
		return "마우스 이동 " + at()
	case "drag":
		return "드래그 " + at() + "→(" + args["to_x"] + "," + args["to_y"] + ")"
	case "scroll":
		return "스크롤 " + args["scroll"] + " ×" + args["amount"] + " " + at()
	case "type":
		t := []rune(args["text"])
		if len(t) > 40 {
			t = append(t[:40], '…')
		}
		return "입력 \"" + string(t) + "\""
	case "key":
		return "키 " + args["key"]
	case "wait":
		return "대기 " + args["wait_ms"] + "ms"
	case "cursor":
		return "커서 위치"
	}
	return action
}

package hostops

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func ip(v int) *int { return &v }

func TestBuildComputerCommand_Validation(t *testing.T) {
	cases := []struct {
		name    string
		params  computerParams
		wantErr string
	}{
		{"unknown action", computerParams{Action: "format_disk"}, "unknown action"},
		{"click without x", computerParams{Action: "click", Y: ip(1)}, "needs x"},
		{"click without y", computerParams{Action: "click", X: ip(1)}, "needs y"},
		{"negative coordinate", computerParams{Action: "move", X: ip(-1), Y: ip(1)}, "out of range"},
		{"bad button", computerParams{Action: "click", X: ip(1), Y: ip(1), Button: "back"}, "button must be"},
		{"drag without to", computerParams{Action: "drag", X: ip(1), Y: ip(1)}, "needs to_x"},
		{"scroll bad direction", computerParams{Action: "scroll", X: ip(1), Y: ip(1), Scroll: "diagonal"}, "scroll must be"},
		{"scroll amount too big", computerParams{Action: "scroll", X: ip(1), Y: ip(1), Amount: ip(999)}, "amount must be"},
		{"type without text", computerParams{Action: "type"}, "needs text"},
		{"type too long", computerParams{Action: "type", Text: strings.Repeat("가", computerMaxTypeRunes+1)}, "too long"},
		{"key without key", computerParams{Action: "key"}, "needs key"},
		{"wait too long", computerParams{Action: "wait", WaitMs: ip(60000)}, "wait_ms must be"},
		{"zoom partial", computerParams{Action: "screenshot", X: ip(1), Y: ip(1)}, "x, y, width, height together"},
		{"zoom zero size", computerParams{Action: "screenshot", X: ip(1), Y: ip(1), Width: ip(0), Height: ip(10)}, "must be positive"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := buildComputerCommand(tc.params); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestBuildComputerCommand_Frames(t *testing.T) {
	cases := []struct {
		name       string
		params     computerParams
		wantAction string
		wantArgs   map[string]string
	}{
		{"plain screenshot", computerParams{Action: "Screenshot"}, "screenshot", map[string]string{}},
		{
			"zoom screenshot",
			computerParams{Action: "screenshot", X: ip(10), Y: ip(20), Width: ip(300), Height: ip(200)},
			"screenshot",
			map[string]string{"x": "10", "y": "20", "width": "300", "height": "200"},
		},
		{"click defaults left", computerParams{Action: "click", X: ip(5), Y: ip(7)}, "click", map[string]string{"x": "5", "y": "7", "button": "left"}},
		{"right click", computerParams{Action: "right_click", X: ip(5), Y: ip(7)}, "right_click", map[string]string{"x": "5", "y": "7"}},
		{"drag", computerParams{Action: "drag", X: ip(1), Y: ip(2), ToX: ip(3), ToY: ip(4)}, "drag", map[string]string{"x": "1", "y": "2", "to_x": "3", "to_y": "4"}},
		{"scroll defaults", computerParams{Action: "scroll", X: ip(1), Y: ip(2)}, "scroll", map[string]string{"x": "1", "y": "2", "scroll": "down", "amount": "3"}},
		{"type", computerParams{Action: "type", Text: "안녕 hello"}, "type", map[string]string{"text": "안녕 hello"}},
		{"key", computerParams{Action: "key", Key: " ctrl+c "}, "key", map[string]string{"key": "ctrl+c"}},
		{"wait default", computerParams{Action: "wait"}, "wait", map[string]string{"wait_ms": "1000"}},
		{"cursor", computerParams{Action: "cursor"}, "cursor", map[string]string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			action, args, err := buildComputerCommand(tc.params)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if action != tc.wantAction {
				t.Fatalf("action = %q, want %q", action, tc.wantAction)
			}
			if len(args) != len(tc.wantArgs) {
				t.Fatalf("args = %v, want %v", args, tc.wantArgs)
			}
			for k, v := range tc.wantArgs {
				if args[k] != v {
					t.Fatalf("args[%s] = %q, want %q (all=%v)", k, args[k], v, args)
				}
			}
		})
	}
}

func TestToolComputer_SendPathAndErrors(t *testing.T) {
	t.Run("nil sender", func(t *testing.T) {
		_, err := ToolComputer(nil)(context.Background(), json.RawMessage(`{"action":"screenshot"}`))
		if err == nil || !strings.Contains(err.Error(), "배선되지 않아") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("invalid params never reach the sender", func(t *testing.T) {
		called := false
		fn := ToolComputer(func(context.Context, string, map[string]string) (string, error) {
			called = true
			return "", nil
		})
		if _, err := fn(context.Background(), json.RawMessage(`{"action":"click"}`)); err == nil {
			t.Fatal("expected validation error")
		}
		if called {
			t.Fatal("sender must not be called on invalid params")
		}
	})
	t.Run("sender output is returned verbatim", func(t *testing.T) {
		fn := ToolComputer(func(_ context.Context, action string, args map[string]string) (string, error) {
			if action != "screenshot" {
				t.Fatalf("action = %q", action)
			}
			return "화면 1280×800: 브라우저 창", nil
		})
		out, err := fn(context.Background(), json.RawMessage(`{"action":"screenshot"}`))
		if err != nil || out != "화면 1280×800: 브라우저 창" {
			t.Fatalf("out=%q err=%v", out, err)
		}
	})
	t.Run("empty sender output gets a Korean ack", func(t *testing.T) {
		fn := ToolComputer(func(context.Context, string, map[string]string) (string, error) { return "", nil })
		out, err := fn(context.Background(), json.RawMessage(`{"action":"click","x":10,"y":20}`))
		if err != nil || !strings.Contains(out, "클릭 (10,20)") {
			t.Fatalf("out=%q err=%v", out, err)
		}
	})
	t.Run("sender error propagates", func(t *testing.T) {
		want := errors.New("데스크톱 미연결")
		fn := ToolComputer(func(context.Context, string, map[string]string) (string, error) { return "", want })
		if _, err := fn(context.Background(), json.RawMessage(`{"action":"cursor"}`)); !errors.Is(err, want) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestDescribeComputerCommand(t *testing.T) {
	long := strings.Repeat("가", 60)
	cases := map[string]string{
		"screenshot":   "스크린샷",
		"click":        "클릭 (1,2)",
		"double_click": "더블클릭 (1,2)",
		"drag":         "드래그 (1,2)→(3,4)",
		"scroll":       "스크롤 up ×2 (1,2)",
		"key":          "키 ctrl+c",
		"wait":         "대기 500ms",
	}
	args := map[string]string{"x": "1", "y": "2", "to_x": "3", "to_y": "4", "scroll": "up", "amount": "2", "key": "ctrl+c", "wait_ms": "500", "button": "left"}
	for action, want := range cases {
		if got := DescribeComputerCommand(action, args); got != want {
			t.Errorf("%s: got %q want %q", action, got, want)
		}
	}
	if got := DescribeComputerCommand("type", map[string]string{"text": long}); !strings.HasSuffix(got, "…\"") || len([]rune(got)) > 50 {
		t.Errorf("long type text must be truncated: %q", got)
	}
	if got := DescribeComputerCommand("click", map[string]string{"x": "1", "y": "2", "button": "middle"}); got != "middle 클릭 (1,2)" {
		t.Errorf("middle click: %q", got)
	}
}

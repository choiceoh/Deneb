package hostops

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestBuildWorkstationCommand_Validation(t *testing.T) {
	cases := []struct {
		name    string
		params  workstationParams
		wantErr string
	}{
		{"unknown action", workstationParams{Action: "delete_all", View: "mail"}, "unknown action"},
		{"open without view or path", workstationParams{Action: "open"}, "needs view"},
		{"split without view", workstationParams{Action: "split"}, "needs view"},
		{"focus without view", workstationParams{Action: "focus"}, "needs view"},
		{"layout without views", workstationParams{Action: "layout"}, "needs views"},
		{"wiki without path", workstationParams{Action: "wiki"}, "needs path"},
		{"spotlight without ref", workstationParams{Action: "spotlight", View: "mail"}, "needs view + ref"},
		{"prefill on non-todo", workstationParams{Action: "prefill", View: "mail", Title: "x"}, "view=todo only"},
		{"prefill without title", workstationParams{Action: "prefill", View: "todo"}, "needs title"},
		{"bad date shape", workstationParams{Action: "open", View: "mail", Date: "7월 15일"}, "date must be YYYY-MM-DD"},
		{"impossible calendar date", workstationParams{Action: "open", View: "mail", Date: "2026-02-30"}, "date must be YYYY-MM-DD"},
		{"date outside open/split", workstationParams{Action: "focus", View: "mail", Date: "2026-07-15"}, "only valid with open/split"},
		{"bad due shape", workstationParams{Action: "prefill", View: "todo", Title: "x", Due: "내일"}, "due must be YYYY-MM-DD"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := buildWorkstationCommand(tc.params); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestBuildWorkstationCommand_Frames(t *testing.T) {
	action, args, err := buildWorkstationCommand(workstationParams{Action: " Split ", View: "calendar"})
	if err != nil || action != "split" || args["view"] != "calendar" {
		t.Fatalf("split frame = %q %v (%v)", action, args, err)
	}

	// open with a wiki path but no view is a valid wiki-style open.
	action, args, err = buildWorkstationCommand(workstationParams{Action: "open", Path: "프로젝트/데네브.md"})
	if err != nil || action != "open" || args["path"] != "프로젝트/데네브.md" {
		t.Fatalf("open-path frame = %q %v (%v)", action, args, err)
	}

	action, args, err = buildWorkstationCommand(workstationParams{Action: "layout", Views: "mail,calendar"})
	if err != nil || action != "layout" || args["views"] != "mail,calendar" {
		t.Fatalf("layout frame = %q %v (%v)", action, args, err)
	}

	// close without a view is the intentional close-focused form.
	action, args, err = buildWorkstationCommand(workstationParams{Action: "close"})
	if err != nil || action != "close" || len(args) != 0 {
		t.Fatalf("close frame = %q %v (%v)", action, args, err)
	}

	action, args, err = buildWorkstationCommand(workstationParams{Action: "open", View: "search", Query: "면허 대여"})
	if err != nil || args["query"] != "면허 대여" || args["view"] != "search" {
		t.Fatalf("search frame = %q %v (%v)", action, args, err)
	}

	action, args, err = buildWorkstationCommand(workstationParams{Action: "spotlight", View: "approvals", Ref: "99391"})
	if err != nil || action != "spotlight" || args["view"] != "approvals" || args["ref"] != "99391" {
		t.Fatalf("spotlight frame = %q %v (%v)", action, args, err)
	}

	action, args, err = buildWorkstationCommand(workstationParams{Action: "open", View: "mail", Date: "2026-07-15"})
	if err != nil || args["date"] != "2026-07-15" {
		t.Fatalf("date frame = %q %v (%v)", action, args, err)
	}

	action, args, err = buildWorkstationCommand(workstationParams{
		Action: "prefill", View: "todo", Title: "견적 회신", Due: "2026-07-21", Note: "부산항터미널",
	})
	if err != nil || action != "prefill" || args["title"] != "견적 회신" || args["due"] != "2026-07-21" || args["note"] != "부산항터미널" {
		t.Fatalf("prefill frame = %q %v (%v)", action, args, err)
	}
}

func TestToolWorkstation_DispatchAndErrors(t *testing.T) {
	var gotAction string
	var gotArgs map[string]string
	tool := ToolWorkstation(func(_ context.Context, action string, args map[string]string) error {
		gotAction, gotArgs = action, args
		return nil
	}, nil)

	out, err := tool(context.Background(), json.RawMessage(`{"action":"split","view":"mail"}`))
	if err != nil {
		t.Fatalf("tool err: %v", err)
	}
	if gotAction != "split" || gotArgs["view"] != "mail" {
		t.Fatalf("send got %q %v", gotAction, gotArgs)
	}
	if !strings.Contains(out, "split") {
		t.Fatalf("result %q should name the action", out)
	}

	// Validation failures never reach send.
	gotAction = ""
	if _, err := tool(context.Background(), json.RawMessage(`{"action":"nope"}`)); err == nil {
		t.Fatal("expected validation error")
	}
	if gotAction != "" {
		t.Fatalf("send must not fire on validation error, got %q", gotAction)
	}

	// Transport errors surface to the agent (e.g. no desktop connected).
	toolDown := ToolWorkstation(func(context.Context, string, map[string]string) error {
		return errors.New("연결된 데스크톱 워크스테이션(Andromeda)이 없습니다")
	}, nil)
	if _, err := toolDown(context.Background(), json.RawMessage(`{"action":"focus","view":"mail"}`)); err == nil {
		t.Fatal("expected transport error to propagate")
	}

	// A nil sender reports the capability as unavailable.
	toolNil := ToolWorkstation(nil, nil)
	if _, err := toolNil(context.Background(), json.RawMessage(`{"action":"focus","view":"mail"}`)); err == nil {
		t.Fatal("expected unavailable error with nil sender")
	}
}

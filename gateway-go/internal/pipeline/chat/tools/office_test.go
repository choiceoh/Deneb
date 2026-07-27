package tools

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

func runOffice(t *testing.T, params map[string]any) (string, error) {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	return ToolOffice(t.TempDir())(context.Background(), raw)
}

// The whitelist / required-arg checks run before any exec, so they are
// deterministic without the officecli binary present.
func TestToolOfficeRejectsUnknownCommand(t *testing.T) {
	// Resident-lifecycle and agent-integration verbs are intentionally excluded.
	for _, cmd := range []string{"install", "mcp", "open", "close", "bogus", ""} {
		out, err := runOffice(t, map[string]any{"command": cmd, "file": "book.xlsx"})
		if err == nil {
			t.Errorf("command %q: expected error, got output %q", cmd, out)
		}
	}
}

func TestToolOfficeRequiresFileExceptHelp(t *testing.T) {
	if _, err := runOffice(t, map[string]any{"command": "view"}); err == nil {
		t.Error("view without file: expected error")
	}
	// help needs no file; it does exec officecli, so only assert the missing-file
	// guard is bypassed when the binary is unavailable.
	if _, err := exec.LookPath("officecli"); err != nil {
		out, err := runOffice(t, map[string]any{"command": "help"})
		// No binary: the exec fails but is surfaced as a graceful message (nil
		// error), never the "file is required" pre-exec error.
		if err != nil {
			t.Fatalf("help should not hard-error: %v", err)
		}
		if strings.Contains(out, "file is required") {
			t.Errorf("help wrongly required a file: %q", out)
		}
	}
}

// Prompt-injection safeguard: credential / control-plane paths are hard-refused
// before any exec, so this is deterministic without the officecli binary.
func TestToolOfficeRefusesProtectedPaths(t *testing.T) {
	for _, f := range []string{
		"/home/deneb/.ssh/id_rsa",
		"/home/deneb/.deneb/auth.json",
		"/tmp/x/.env",
		"/home/deneb/.aws/credentials",
	} {
		out, err := runOffice(t, map[string]any{
			"command": "get", "file": f, "args": []string{"/Sheet1/A1"},
		})
		if err == nil {
			t.Errorf("protected path %q: expected refusal, got output %q", f, out)
			continue
		}
		if !strings.Contains(err.Error(), "access denied") {
			t.Errorf("protected path %q: expected access-denied error, got %v", f, err)
		}
	}
}

// End-to-end round-trip when the binary is installed (srv4 + CI runner have it).
func TestToolOfficeLiveRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("officecli"); err != nil {
		t.Skip("officecli not on PATH")
	}
	dir := t.TempDir()
	fn := ToolOffice(dir)
	call := func(params map[string]any) string {
		raw, _ := json.Marshal(params)
		out, err := fn(context.Background(), raw)
		if err != nil {
			t.Fatalf("office %v: %v", params["command"], err)
		}
		return out
	}
	// create → set a formula → read it back; the workbook is a relative path so
	// it must resolve under the workspace dir.
	call(map[string]any{"command": "create", "file": "wb.xlsx"})
	call(map[string]any{
		"command": "set", "file": "wb.xlsx",
		"args": []string{"/Sheet1/A1", "--prop", "value=42"},
	})
	call(map[string]any{
		"command": "set", "file": "wb.xlsx",
		"args": []string{"/Sheet1/A2", "--prop", "formula=A1*2"},
	})
	got := call(map[string]any{"command": "get", "file": "wb.xlsx", "args": []string{"/Sheet1/A2"}})
	if !strings.Contains(got, "84") {
		t.Errorf("expected auto-evaluated formula result 84 in %q", got)
	}
}

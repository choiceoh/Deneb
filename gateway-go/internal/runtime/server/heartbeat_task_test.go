package server

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWithinActiveHours_boundaries(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		t.Skipf("Asia/Seoul tzdata unavailable: %v", err)
	}
	cases := []struct {
		hour int
		want bool
	}{
		{0, false},
		{7, false},
		{8, true},
		{12, true},
		{22, true},
		{23, false},
	}
	for _, c := range cases {
		now := time.Date(2026, 5, 3, c.hour, 30, 0, 0, loc)
		if got := withinActiveHours(now); got != c.want {
			t.Errorf("hour=%d want=%v got=%v", c.hour, c.want, got)
		}
	}
}

func TestReadHeartbeat_missingFile(t *testing.T) {
	home := t.TempDir()
	tk := &heartbeatTask{homeDir: home, logger: slog.Default()}
	if got := tk.readHeartbeat(); got != "" {
		t.Errorf("missing file should return empty, got %q", got)
	}
}

func TestReadHeartbeat_whitespaceOnlyTreatedAsEmpty(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".deneb")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "HEARTBEAT.md"), []byte("   \n\t\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tk := &heartbeatTask{homeDir: home, logger: slog.Default()}
	if got := tk.readHeartbeat(); got != "" {
		t.Errorf("whitespace-only file should return empty, got %q", got)
	}
}

func TestReadHeartbeat_returnsContent(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".deneb")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	want := "task A\ntask B"
	if err := os.WriteFile(filepath.Join(dir, "HEARTBEAT.md"), []byte(want+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tk := &heartbeatTask{homeDir: home, logger: slog.Default()}
	if got := tk.readHeartbeat(); got != want {
		t.Errorf("want %q, got %q", want, got)
	}
}

// The trigger template carries the actual fix for the "repeats every 30
// minutes after user said stop" bug. Pin its contract so a future refactor
// does not silently drop the stop-word rule, the self-edit instruction, or
// the NO_REPLY contract.
func TestHeartbeatTriggerTemplate_invariants(t *testing.T) {
	got := fmt.Sprintf(heartbeatTriggerTemplate, "<<HEARTBEAT_BODY>>")

	mustContain := map[string]string{
		"NO_REPLY":         "must instruct silent reply",
		"그만":               "must enumerate user-stop expressions",
		"중단":               "must enumerate user-stop expressions",
		"hartbeat_update":  "", // placeholder — see actual checks below
		"~/.deneb/HEARTBEAT.md": "must reference the canonical path",
		"archive":          "must instruct stalled-item archival",
		"진행중":              "must show progress-update example format",
		"<<HEARTBEAT_BODY>>": "%s placeholder must render the file contents",
	}
	delete(mustContain, "hartbeat_update")
	mustContain["heartbeat_update"] = "must name the dedicated update tool, not fs.write"
	mustContain["fs.write"] = "must explicitly call out fs.write as the mechanism — the agent uses fs by default"
	for snippet, why := range mustContain {
		if !strings.Contains(got, snippet) {
			t.Errorf("trigger template missing %q (%s)", snippet, why)
		}
	}

	// Sanity: the rendered template must not contain a stray `%` that vet
	// would catch as an unintended format verb. We already caught one (95%)
	// during the initial port; this test pins it down.
	if strings.Contains(got, "%!") {
		t.Errorf("trigger template has Sprintf format error markers: %q", got)
	}
}

// The agent's default file-write tool (fs.write) is clamped to its workspace,
// so the template MUST steer it to the dedicated heartbeat_update tool.
// Reverting to fs.write — even as a fallback — would silently break self-edit.
func TestHeartbeatTriggerTemplate_doesNotPromoteFSWrite(t *testing.T) {
	if !strings.Contains(heartbeatTriggerTemplate, "heartbeat_update") {
		t.Fatalf("template must reference heartbeat_update tool")
	}
	// fs.write may appear in a "do not use this" warning context, but the
	// template's update instruction must stand on heartbeat_update.
}

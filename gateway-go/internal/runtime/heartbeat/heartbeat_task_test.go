package heartbeat

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	runtimesession "github.com/choiceoh/deneb/gateway-go/internal/domain/session"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
)

// deliveryCaptureRunner is a SyncRunner test double that records the request it
// received and returns a canned result.
type deliveryCaptureRunner struct {
	called bool
	req    chatport.SyncRequest
	result *chatport.SyncResult
}

func (c *deliveryCaptureRunner) ChatReady() bool { return true }

func (c *deliveryCaptureRunner) RunSync(_ context.Context, req chatport.SyncRequest) (*chatport.SyncResult, error) {
	c.called = true
	c.req = req
	return c.result, nil
}

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
func TestHeartbeatTriggerTemplateContractStringsPresent(t *testing.T) {
	got := fmt.Sprintf(heartbeatTriggerTemplate, "<<HEARTBEAT_BODY>>")

	mustContain := map[string]string{
		"NO_REPLY":              "must instruct silent reply",
		"그만":                    "must enumerate user-stop expressions",
		"중단":                    "must enumerate user-stop expressions",
		"hartbeat_update":       "", // placeholder — see actual checks below
		"~/.deneb/HEARTBEAT.md": "must reference the canonical path",
		"archive":               "must instruct stalled-item archival",
		"진행중":                   "must show progress-update example format",
		"대화 transcript가 아니라":    "must keep repeat-loop state out of short-term transcript",
		"<<HEARTBEAT_BODY>>":    "%s placeholder must render the file contents",
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

func TestHeartbeatSyncRequestWithoutTranscriptPersistence(t *testing.T) {
	req := heartbeatSyncRequest()
	if req.MaxHistoryTokens != heartbeatHistoryBudget {
		t.Fatalf("heartbeat history budget = %d, want %d", req.MaxHistoryTokens, heartbeatHistoryBudget)
	}
	if req.MaxToolCallAttempts == nil || *req.MaxToolCallAttempts != heartbeatMaxToolCallAttempts {
		t.Fatalf("heartbeat tool-call cap = %v, want %d", req.MaxToolCallAttempts, heartbeatMaxToolCallAttempts)
	}
	if !req.EphemeralUser {
		t.Fatal("heartbeat trigger must not persist as a user message")
	}
	if !req.EphemeralAssistant {
		t.Fatal("heartbeat assistant/tool output must not persist into short-term chat context")
	}
	if !req.AutoDeliveredOutput {
		t.Fatal("heartbeat report is delivered by the proactive relay, so AutoDeliveredOutput must be true")
	}
}

// A content-driven heartbeat tick must run in the ISOLATED submain session (not
// client:main), on the configured model, with AutoDeliveredOutput set, and must
// hand the report's BestText to the proactive-relay deliver closure — the wiring
// that isolates the tick from the user's live conversation while keeping the
// report visible.
func TestHeartbeatRunIsolatesSessionAndDeliversReport(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		t.Skipf("Asia/Seoul tzdata unavailable: %v", err)
	}
	home := t.TempDir()
	dir := filepath.Join(home, ".deneb")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A real task line guarantees the tick warrants a turn.
	if err := os.WriteFile(filepath.Join(dir, "HEARTBEAT.md"), []byte("- 납품 확인 진행중\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	runner := &deliveryCaptureRunner{result: &chatport.SyncResult{
		Text:     "## 납품 현황\n- 오늘 확인 필요",
		BestText: "## 납품 현황\n- 오늘 확인 필요",
	}}

	deliveredText := ""
	deliverCalled := false
	tk := NewTask(TaskConfig{
		ChatHandler: runner,
		Logger:      slog.Default(),
		HomeDir:     home,
		Model:       "submain",
		Deliver: func(text string) (bool, error) {
			deliverCalled = true
			deliveredText = text
			return true, nil
		},
		// Active hours, and no ActivityTracker so the idle gate is skipped.
		Now: func() time.Time { return time.Date(2026, 5, 3, 10, 0, 0, 0, loc) },
	})

	if err := tk.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !runner.called {
		t.Fatal("expected the content-driven tick to dispatch a turn")
	}
	if runner.req.SessionKey != runtimesession.HeartbeatWorkSessionKey {
		t.Errorf("session = %q, want isolated %q", runner.req.SessionKey, runtimesession.HeartbeatWorkSessionKey)
	}
	if runner.req.SessionKey == runtimesession.NativeWorkSessionKey {
		t.Error("heartbeat must NOT run in the live client:main session")
	}
	if runner.req.Model != "submain" {
		t.Errorf("model = %q, want submain", runner.req.Model)
	}
	if !runner.req.AutoDeliveredOutput {
		t.Error("AutoDeliveredOutput must be true so the agent skips the in-loop message tool")
	}
	if !deliverCalled {
		t.Fatal("report was never handed to the proactive-relay deliver closure")
	}
	if deliveredText != runner.result.BestText {
		t.Errorf("delivered %q, want BestText %q", deliveredText, runner.result.BestText)
	}
}

func TestHeartbeatTargetSessionKey_NativePreferredWithWorkFallback(t *testing.T) {
	cases := []struct {
		name string
		last string
		want string
	}{
		{name: "native work home", last: "client:main", want: "client:main"},
		{name: "native topic", last: "client:coding", want: "client:coding"},
		{name: "native fresh chat", last: "client:main:abc123", want: "client:main:abc123"},
		{name: "legacy telegram falls back", last: "telegram:42", want: "client:main"},
		{name: "empty falls back", last: "", want: "client:main"},
		{name: "invalid native falls back", last: "client:", want: "client:main"},
		{name: "system session falls back", last: "cron:nightly", want: "client:main"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runtimesession.HeartbeatTargetSession(tc.last); got != tc.want {
				t.Fatalf("heartbeatTargetSessionKey(%q) = %q, want %q", tc.last, got, tc.want)
			}
		})
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

// A scaffolding-only HEARTBEAT.md (section headers, comments, archived items)
// must not count as tasks — production 2026-07-05: a file holding only
// "## Active Tasks" kept every 30-min tick paying a full ~29K-token cloud turn.
func TestHeartbeatHasTasksTreatsScaffoldingAsEmpty(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    bool
	}{
		{"empty", "", false},
		{"header only", "## Active Tasks", false},
		{"headers and blanks", "# HEARTBEAT\n\n## Active Tasks\n\n", false},
		{"comment only", "## Active Tasks\n<!-- 여기에 작업을 적으세요 -->", false},
		{"archive only", "## Active Tasks\n\n## archive\n- [완료 07-01] 옛 작업", false},
		{"real task", "## Active Tasks\n- 매일 18시 진코솔라 회신 확인", true},
		{"task after archive section resumes", "## archive\n- 옛 작업\n\n## Active Tasks\n- 새 작업", true},
		{"plain text without headers", "진코솔라 LC 개설 진행 상황 점검", true},
		{"hashtag-tagged task is content", "## Active Tasks\n#urgent LC 개설 확인", true},
		{"nested heading stays archived", "## archive\n### 2026-07\n- 옛 작업", false},
		{"sibling section after nested archive resumes", "## archive\n### 2026-07\n- 옛것\n\n## Active Tasks\n- 새 작업", true},
		// "## status" = lane bookkeeping (sweep last-check notes). Production
		// 2026-08-01: sweep status parked at top level made 91% of firings a
		// full submain turn that concluded NO_REPLY — status must not wake the
		// heartbeat, and a real task alongside it still must.
		{"status only", "## status\n[자가개선 스윕 — 07-25 21:00 점검]\n이전 스윕 완료 상태 유지.", false},
		{"status case-insensitive", "## Status\n- 스윕 상태 유지", false},
		{"status plus archive only", "## status\n- 상태 메모\n\n## archive\n- 옛 작업", false},
		{"real task after status resumes", "## status\n- 상태 메모\n\n## Active Tasks\n- LC 개설 확인", true},
		{"nested heading stays status", "## status\n### 스윕\n- 상태 유지", false},
	}
	for _, tc := range cases {
		if got := heartbeatHasTasks(tc.content); got != tc.want {
			t.Errorf("%s: heartbeatHasTasks = %v, want %v", tc.name, got, tc.want)
		}
	}
}

package chat

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTaskProgress_BasicLifecycle(t *testing.T) {
	tp := NewTaskProgress("telegram:123", "run-1", "파일 읽어줘")

	if !tp.IsRunning() {
		t.Fatal("expected IsRunning to be true")
	}

	// Simulate tool execution.
	tp.AppendToolLog("read", "server.go:1-50")
	tp.AppendText("서버 파일을 읽겠습니다")
	tp.FinishTool("read", "200 lines read")
	tp.IncrementTurn()

	tp.AppendToolLog("grep", "health")
	tp.SetCurrentTool("grep")

	block := tp.FormatContextBlock()

	if !strings.Contains(block, "파일 읽어줘") {
		t.Error("expected user message in context block")
	}
	if !strings.Contains(block, "read") {
		t.Error("expected tool name 'read' in context block")
	}
	if !strings.Contains(block, "grep") {
		t.Error("expected current tool 'grep' in context block")
	}
	if !strings.Contains(block, "서버 파일을 읽겠습니다") {
		t.Error("expected text output in context block")
	}
	if !strings.Contains(block, "턴: 1/25") {
		t.Error("expected turn count in context block")
	}
	if !strings.Contains(block, "백그라운드") {
		t.Error("expected background task notice")
	}
}

func TestTaskProgress_TextTruncation(t *testing.T) {
	tp := NewTaskProgress("s", "r", "m")

	// Write more than 1200 chars to trigger truncation.
	longText := strings.Repeat("x", 1500)
	tp.AppendText(longText)

	block := tp.FormatContextBlock()
	// After truncation, lastText should be ~800 chars.
	if strings.Count(block, "x") > 900 {
		t.Error("expected text to be truncated to ~800 chars")
	}
}

func TestTaskProgress_ConcurrentAccess(t *testing.T) {
	tp := NewTaskProgress("s", "r", "concurrent test")

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			tp.AppendToolLog("tool", "input")
		}()
		go func() {
			defer wg.Done()
			tp.AppendText("delta")
		}()
		go func() {
			defer wg.Done()
			_ = tp.FormatContextBlock()
		}()
	}
	wg.Wait()
}

func TestTaskProgress_NilIsNotRunning(t *testing.T) {
	var tp *TaskProgress
	if tp.IsRunning() {
		t.Fatal("nil TaskProgress should not be running")
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "30초"},
		{60 * time.Second, "1분"},
		{90 * time.Second, "1분 30초"},
		{5 * time.Minute, "5분"},
	}
	for _, tc := range tests {
		got := formatDuration(tc.d)
		if got != tc.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

func TestTruncateStr(t *testing.T) {
	if got := truncateStr("hello", 10); got != "hello" {
		t.Errorf("truncateStr short = %q", got)
	}
	if got := truncateStr("hello world", 5); got != "hello…" {
		t.Errorf("truncateStr long = %q", got)
	}
}

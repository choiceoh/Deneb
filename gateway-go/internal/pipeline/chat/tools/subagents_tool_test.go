package tools

import (
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
)

// A finished sub-agent returns its stored output, auto-targeted when only one
// exists. This is the pull path the parent uses if it missed the proactive
// completion notification.
func TestSubagentsResult_DoneReturnsLastOutput(t *testing.T) {
	child := &session.Session{Key: "client:main:sub:1", Status: session.StatusDone, LastOutput: "분석 결과: 3건 발견"}
	out, err := subagentsResult(&toolctx.SessionDeps{}, []*session.Session{child}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "분석 결과: 3건 발견") {
		t.Errorf("out = %q, want it to contain the last output", out)
	}
}

// A still-running sub-agent reports "no result yet" rather than a blank result.
func TestSubagentsResult_RunningReportsNoResultYet(t *testing.T) {
	now := time.Now().UnixMilli()
	child := &session.Session{Key: "client:main:sub:1", Status: session.StatusRunning, StartedAt: &now}
	out, err := subagentsResult(&toolctx.SessionDeps{}, []*session.Session{child}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "still running") {
		t.Errorf("out = %q, want a still-running notice", out)
	}
}

// With multiple sub-agents and no target, ask the caller to disambiguate.
func TestSubagentsResult_MultipleRequiresTarget(t *testing.T) {
	children := []*session.Session{
		{Key: "client:main:sub:1", Status: session.StatusDone, LastOutput: "A"},
		{Key: "client:main:sub:2", Status: session.StatusDone, LastOutput: "B"},
	}
	out, err := subagentsResult(&toolctx.SessionDeps{}, children, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Multiple sub-agents") {
		t.Errorf("out = %q, want a disambiguation prompt", out)
	}
}

// Resolve by 1-based index and surface the failure reason when there is no
// output to show.
func TestSubagentsResult_FailedNoOutputShowsReason(t *testing.T) {
	children := []*session.Session{
		{Key: "client:main:sub:1", Status: session.StatusFailed, FailureReason: "컨텍스트 초과"},
	}
	out, err := subagentsResult(&toolctx.SessionDeps{}, children, "1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "컨텍스트 초과") {
		t.Errorf("out = %q, want the failure reason", out)
	}
}

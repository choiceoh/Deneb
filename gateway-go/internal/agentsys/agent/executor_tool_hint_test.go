package agent

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

func TestToolErrorHint_KnownPatterns(t *testing.T) {
	cases := []struct {
		name    string
		rawErr  string
		wantSub string // a distinctive substring of the expected hint
	}{
		{"bash syntax", "exec: bash: -c: line 1: syntax error near unexpected token `('", "셸 문법 오류"},
		{"command not found", "exec: /bin/sh: 1: foo: command not found", "PATH"},
		{"missing file", "open /tmp/nope.txt: no such file or directory", "경로가 없다"},
		{"is a directory", "read /etc: is a directory", "디렉토리를 파일처럼"},
		{"permission", "open /root/secret: permission denied", "권한이 없다"},
		{"dns", "dial tcp: lookup bad.host: no such host", "호스트 이름"},
		{"refused", "dial tcp 127.0.0.1:9999: connect: connection refused", "연결이 거부"},
		{"deadline", "Post \"http://x\": context deadline exceeded", "시간 초과"},
		{"timeout", "request timeout after 30s", "시간 초과"},
		{"json truncated", "unexpected end of JSON input", "중간에 끊겼"},
		{"json type", "json: cannot unmarshal string into Go value of type int", "타입이 스키마"},
		{"json char", "invalid character 'x' looking for beginning of value", "잘못된 문자"},
		{"unregistered", "tool not registered: frobnicate", "등록돼 있지 않다"},
		{"unknown tool", "unknown tool: frobnicate", "그런 도구는 없다"},
		{"exit status", "exec: command failed: exit status 2", "종료코드"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := toolErrorHint(tc.rawErr)
			if got == "" {
				t.Fatalf("toolErrorHint(%q) = \"\", want a hint containing %q", tc.rawErr, tc.wantSub)
			}
			if !strings.Contains(got, tc.wantSub) {
				t.Fatalf("toolErrorHint(%q) = %q, want substring %q", tc.rawErr, got, tc.wantSub)
			}
			// Every hint must steer away from a blind repeat — that is the whole point.
			if !strings.Contains(got, "반복") && !strings.Contains(got, "재시도") && !strings.Contains(got, "다시") && !strings.Contains(got, "골라") {
				t.Errorf("hint for %q lacks a recovery directive: %q", tc.name, got)
			}
		})
	}
}

func TestToolErrorHint_NoMatch(t *testing.T) {
	// Unknown errors and Deneb's own Korean tool errors must NOT match, so we
	// never double-hint a message that already guides the model.
	for _, raw := range []string{
		"",
		"something went wrong",
		"위키 페이지를 찾을 수 없습니다. 제목을 확인하세요.",
		"검색 결과가 없습니다.",
	} {
		if got := toolErrorHint(raw); got != "" {
			t.Errorf("toolErrorHint(%q) = %q, want \"\"", raw, got)
		}
	}
}

func TestToolErrorHint_CaseInsensitive(t *testing.T) {
	if toolErrorHint("SYNTAX ERROR near token") == "" {
		t.Fatal("expected case-insensitive match on uppercase error")
	}
}

func TestToolErrorHint_FirstMatchWins(t *testing.T) {
	// A raw error carrying both a precise cause (syntax error) and the generic
	// "exit status" tail must surface the precise hint, not the catch-all.
	got := toolErrorHint("bash: syntax error near `('\nexit status 2")
	if !strings.Contains(got, "셸 문법 오류") {
		t.Fatalf("ordering broke: got %q, want the syntax hint", got)
	}
}

// errToolExecutor always fails with a fixed error — drives the executeOneTool
// integration assertion below.
type errToolExecutor struct{ err error }

func (e *errToolExecutor) Execute(_ context.Context, _ string, _ json.RawMessage) (string, error) {
	return "", e.err
}

// TestExecuteOneTool_AppendsHint verifies the hint is wired into the live error
// path: the result keeps the raw error verbatim AND carries the recovery hint,
// and is still flagged IsError.
func TestExecuteOneTool_AppendsHint(t *testing.T) {
	tc := llm.ContentBlock{
		Type:  "tool_use",
		ID:    "tool_1",
		Name:  "exec",
		Input: json.RawMessage(`{"command":"foo"}`),
	}
	tools := &errToolExecutor{err: errors.New("/bin/sh: 1: foo: command not found")}

	block := executeOneTool(context.Background(), tc, tools, StreamHooks{},
		"", 0, slog.Default(), nil, nil)

	if !block.IsError {
		t.Fatalf("expected IsError=true, got false (content=%q)", block.Content)
	}
	if !strings.Contains(block.Content, "command not found") {
		t.Errorf("raw error not preserved for grounding: %q", block.Content)
	}
	if !strings.Contains(block.Content, "힌트:") {
		t.Errorf("recovery hint not appended: %q", block.Content)
	}
}

// TestExecuteOneTool_NoHintForUnknownError confirms an unrecognized error is
// passed through unchanged (no spurious hint suffix).
func TestExecuteOneTool_NoHintForUnknownError(t *testing.T) {
	tc := llm.ContentBlock{
		Type:  "tool_use",
		ID:    "tool_2",
		Name:  "vega",
		Input: json.RawMessage(`{}`),
	}
	tools := &errToolExecutor{err: errors.New("something opaque happened")}

	block := executeOneTool(context.Background(), tc, tools, StreamHooks{},
		"", 0, slog.Default(), nil, nil)

	if strings.Contains(block.Content, "힌트:") {
		t.Errorf("unexpected hint on unknown error: %q", block.Content)
	}
}

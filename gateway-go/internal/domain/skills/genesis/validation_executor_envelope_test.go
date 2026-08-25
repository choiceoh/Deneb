package genesis

import (
	"strings"
	"testing"
)

// Fixtures are verbatim shapes from the production journal (2026-08-25), where
// the behavioral gate skipped 6 times: the model returned a COMPLIANT plan
// wrapped in a single string field, and the escaped inner quotes made every
// existing parse path miss it.
func TestParseEmittedToolCalls_UnwrapsStringEnvelope(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want []string
	}{
		{
			name: "answer envelope carrying the plan",
			raw:  `{"answer":"{\"tool_calls\":[{\"name\":\"mail_archive\",\"args\":\"list(days=1)\"},{\"name\":\"wiki\",\"args\":\"search(프로젝트명)\"}]}"}`,
			want: []string{"mail_archive", "wiki"},
		},
		{
			name: "envelope carrying a bare array",
			raw:  `{"result":"[{\"name\":\"web\",\"args\":\"url=https://x\"}]"}`,
			want: []string{"web"},
		},
		{
			name: "plain object still parses",
			raw:  `{"tool_calls":[{"name":"exec","args":"ls"}]}`,
			want: []string{"exec"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls, err := parseEmittedToolCalls(tc.raw)
			if err != nil {
				t.Fatalf("parse failed: %v", err)
			}
			if len(calls) != len(tc.want) {
				t.Fatalf("got %d calls, want %d: %+v", len(calls), len(tc.want), calls)
			}
			for i, name := range tc.want {
				if calls[i].Name != name {
					t.Errorf("call %d = %q, want %q", i, calls[i].Name, name)
				}
			}
		})
	}
}

// A prose answer is the model DECLINING (the replay case carried no concrete
// input), not a plan. It must stay an error so the caller fails open rather
// than scoring the candidate against an empty plan it never produced.
func TestParseEmittedToolCalls_ProseEnvelopeStaysAnError(t *testing.T) {
	raw := `{"answer":"이 작업은 구체적인 문서나 도구 사용 절차가 제공되지 않아 시뮬레이션할 수 없습니다."}`
	if calls, err := parseEmittedToolCalls(raw); err == nil {
		t.Fatalf("prose answer parsed as a plan: %+v", calls)
	}
}

// The error must still carry a body head — that diagnostic is what made this
// root cause findable in the first place.
func TestParseEmittedToolCalls_ErrorKeepsBodyHead(t *testing.T) {
	_, err := parseEmittedToolCalls(`{"answer":"설명만 있고 계획이 없습니다"}`)
	if err == nil {
		t.Fatal("expected an error")
	}
	if got := err.Error(); !strings.Contains(got, "body:") {
		t.Errorf("error lost the body head: %q", got)
	}
}

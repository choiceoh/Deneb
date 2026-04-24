package server

import "testing"

func TestParseMainAgentSteerCommand(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{"plain text nudge", "/steer skip the tests", "skip the tests", true},
		{"korean nudge", "/steer 테스트는 건너뛰어", "테스트는 건너뛰어", true},
		{"leading whitespace", "   /steer and also do X", "and also do X", true},
		{"mixed case prefix", "/Steer please also run lint", "please also run lint", true},
		{"tab separator", "/steer\tdo this", "do this", true},

		// Defer to subagent dispatcher: looks like an id.
		{"numeric id", "/steer 1 go faster", "", false},
		{"3-digit id", "/steer 42 retry", "", false},
		{"hex runid prefix", "/steer abc12345 do it", "", false},

		// Not a /steer command at all.
		{"no prefix", "hello", "", false},
		{"unrelated slash", "/reset now", "", false},
		{"prefix without separator", "/steeraway", "", false},
		{"prefix only", "/steer", "", false},
		{"prefix with only whitespace", "/steer    ", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			note, ok := parseMainAgentSteerCommand(tc.in)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v (note=%q)", ok, tc.ok, note)
			}
			if note != tc.want {
				t.Errorf("note = %q, want %q", note, tc.want)
			}
		})
	}
}

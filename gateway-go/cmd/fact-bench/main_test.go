package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runBench(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

// The checked-in goldset is the ratchet: if the fact plane starts leaking a
// retired value, this is the test that says so.
func TestCheckedInGoldsetHoldsNoStaleExposure(t *testing.T) {
	code, stdout, stderr := runBench(t, "-json")
	if code != 0 {
		t.Fatalf("bench exit = %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	var result score
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("decode summary: %v (%s)", err, stdout)
	}
	if result.Cases < 5 {
		t.Fatalf("goldset is too small to be a ratchet: %d cases", result.Cases)
	}
	if result.StaleChecked == 0 || result.EvidenceChecked == 0 || result.CurrentChecked == 0 {
		t.Fatalf("goldset does not exercise every gated boundary: %+v", result)
	}
	if !result.clean() {
		t.Fatalf("checked-in goldset regressed: %+v", result)
	}
}

func writeGoldset(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gold.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestBenchFailsOnStaleExposureAndWrongWinner(t *testing.T) {
	cases := []struct {
		name string
		gold string
		want string
	}{
		{
			name: "wrong winner",
			gold: `{"schemaVersion":1,"cases":[{
				"id":"wrong-winner","subject":"self","key":"communication.language","kind":"preference",
				"ops":[{"op":"assert","value":"한국어로 답변","authority":"direct_user"},
				       {"op":"assert","value":"영어로 답변","authority":"direct_user"}],
				"current":"한국어로 답변"}]}`,
			want: "is not the winner",
		},
		{
			name: "tombstoned fact still current",
			gold: `{"schemaVersion":1,"cases":[{
				"id":"weak-forget","subject":"self","key":"diet.vegan","kind":"identity",
				"ops":[{"op":"assert","value":"비건 식단 유지","authority":"direct_user"},
				       {"op":"forget","authority":"inference"}],
				"current":""}]}`,
			want: "still current",
		},
		{
			name: "stale evidence allowed",
			gold: `{"schemaVersion":1,"cases":[{
				"id":"no-rules","subject":"self","key":"communication.language","kind":"preference",
				"ops":[{"op":"assert","value":"한국어로 답변","authority":"direct_user"}],
				"current":"한국어로 답변",
				"evidence":["사용자는 한국어로 답변받기를 원한다"]}]}`,
			want: "stale evidence was allowed through",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, stdout, stderr := runBench(t, "-gold", writeGoldset(t, tc.gold))
			if code != 1 {
				t.Fatalf("exit = %d, want 1\nstdout: %s\nstderr: %s", code, stdout, stderr)
			}
			if !strings.Contains(stdout, tc.want) {
				t.Fatalf("summary does not name the failure %q:\n%s", tc.want, stdout)
			}
		})
	}
}

func TestBenchRejectsMalformedGoldsets(t *testing.T) {
	cases := []struct {
		name string
		gold string
	}{
		{"bad schema version", `{"schemaVersion":7,"cases":[]}`},
		{"no cases", `{"schemaVersion":1,"cases":[]}`},
		{"unknown op", `{"schemaVersion":1,"cases":[{"id":"x","subject":"self","key":"a.b","ops":[{"op":"maybe"}]}]}`},
		{"not json", `{`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, _, stderr := runBench(t, "-gold", writeGoldset(t, tc.gold))
			if code != 2 {
				t.Fatalf("exit = %d, want 2 (stderr: %s)", code, stderr)
			}
		})
	}
}

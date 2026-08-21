package runtimeops

import (
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

func TestBoundaryDottedSetAndGetMatrix(t *testing.T) {
	root := map[string]any{}
	sets := []struct {
		path  string
		value any
	}{
		{path: "model", value: "main"},
		{path: "agents.defaults.workspace", value: "/workspace"},
		{path: "agents.defaults.tokens", value: float64(32000)},
		{path: "features.enabled", value: true},
		{path: "features.names", value: []any{"one", "two"}},
	}
	for _, set := range sets {
		if err := dottedSet(root, set.path, set.value); err != nil {
			t.Fatalf("dottedSet(%q): %v", set.path, err)
		}
		got, ok := dottedGet(root, set.path)
		if !ok || !reflect.DeepEqual(got, set.value) {
			t.Fatalf("dottedGet(%q) = %#v,%v, want %#v", set.path, got, ok, set.value)
		}
	}
	for _, path := range []string{"missing", "agents.missing", "agents.defaults.workspace.child", "model.child"} {
		if got, ok := dottedGet(root, path); ok || got != nil {
			t.Fatalf("dottedGet(%q) = %#v,%v", path, got, ok)
		}
	}
	for _, path := range []string{"", ".", "a.", ".a", "a..b", "  "} {
		if err := dottedSet(root, path, "x"); err == nil {
			t.Fatalf("dottedSet accepted invalid path %q", path)
		}
	}
}

func TestBoundaryDottedSetRejectsScalarConflictWithoutMutation(t *testing.T) {
	root := map[string]any{"agents": "scalar", "safe": map[string]any{"value": "old"}}
	before, _ := json.Marshal(root)
	err := dottedSet(root, "agents.defaults.workspace", "/new")
	if err == nil || !strings.Contains(err.Error(), `path conflict at "agents"`) {
		t.Fatalf("error = %v", err)
	}
	after, _ := json.Marshal(root)
	if string(after) != string(before) {
		t.Fatalf("conflicting set mutated root: before=%s after=%s", before, after)
	}
}

func TestBoundaryFormatGatewayUptimeMatrix(t *testing.T) {
	tests := []struct {
		duration time.Duration
		want     string
	}{
		{duration: -time.Second, want: "0s"},
		{duration: 0, want: "0s"},
		{duration: 999 * time.Millisecond, want: "0s"},
		{duration: time.Second, want: "1s"},
		{duration: 59*time.Second + 999*time.Millisecond, want: "59s"},
		{duration: time.Minute, want: "1m"},
		{duration: 59*time.Minute + 59*time.Second, want: "59m"},
		{duration: time.Hour, want: "1h 0m"},
		{duration: 2*time.Hour + 30*time.Minute, want: "2h 30m"},
		{duration: 24 * time.Hour, want: "1d 0h 0m"},
		{duration: 3*24*time.Hour + 4*time.Hour + 5*time.Minute, want: "3d 4h 5m"},
	}
	for _, tt := range tests {
		if got := formatGatewayUptime(tt.duration); got != tt.want {
			t.Fatalf("formatGatewayUptime(%s) = %q, want %q", tt.duration, got, tt.want)
		}
	}
}

func TestBoundaryFormatValueForSummaryMatrix(t *testing.T) {
	tests := []struct {
		value any
		want  string
	}{
		{value: nil, want: "null"},
		{value: "text", want: `"text"`},
		{value: "quote\"", want: `"quote\""`},
		{value: true, want: "true"},
		{value: false, want: "false"},
		{value: 3, want: "3"},
		{value: 3.5, want: "3.5"},
		{value: []string{"a", "b"}, want: `["a","b"]`},
		{value: map[string]any{"a": 1}, want: `{"a":1}`},
	}
	for _, tt := range tests {
		if got := formatValueForSummary(tt.value); got != tt.want {
			t.Fatalf("formatValueForSummary(%#v) = %q, want %q", tt.value, got, tt.want)
		}
	}
}

func TestBoundarySecretKeyDetectionNestedShapes(t *testing.T) {
	tests := []struct {
		name       string
		value      map[string]any
		wantPrefix string
	}{
		{name: "clean", value: map[string]any{"model": "main", "nested": map[string]any{"url": "x"}}, wantPrefix: ""},
		{name: "api key", value: map[string]any{"apiKey": "secret"}, wantPrefix: "apiKey"},
		{name: "nested token", value: map[string]any{"providers": map[string]any{"main": map[string]any{"token": "secret"}}}, wantPrefix: "providers.main.token"},
		{name: "array secret", value: map[string]any{"providers": []any{map[string]any{"name": "a"}, map[string]any{"password": "secret"}}}, wantPrefix: "providers[1].password"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findSecretKey("", tt.value)
			if got != tt.wantPrefix {
				t.Fatalf("findSecretKey = %q, want %q", got, tt.wantPrefix)
			}
		})
	}
}

func TestBoundaryApprovalPayloadCanonicalMapOrder(t *testing.T) {
	one := approvalPayload("config.patch", map[string]any{"z": 2, "a": 1})
	two := approvalPayload("config.patch", map[string]any{"a": 1, "z": 2})
	if one != two {
		t.Fatalf("canonical payload differs: %q vs %q", one, two)
	}
	if one != `["config.patch",{"a":1,"z":2}]` {
		t.Fatalf("payload = %q", one)
	}
}

func TestBoundaryApprovalTokensAreSingleUseAndBound(t *testing.T) {
	pendingApprovalsMu.Lock()
	pendingApprovals = map[string]pendingApproval{}
	pendingApprovalsMu.Unlock()
	t.Cleanup(func() {
		pendingApprovalsMu.Lock()
		pendingApprovals = map[string]pendingApproval{}
		pendingApprovalsMu.Unlock()
	})
	registerPendingApproval("token-a", "config.set", "payload-a")
	if got := consumePendingApproval("token-a", "config.patch", "payload-a"); !strings.Contains(got, "config.set") {
		t.Fatalf("action mismatch = %q", got)
	}
	if got := consumePendingApproval("token-a", "config.set", "payload-b"); !strings.Contains(got, "내용이 다릅니다") {
		t.Fatalf("payload mismatch = %q", got)
	}
	if got := consumePendingApproval("token-a", "config.set", "payload-a"); got != "" {
		t.Fatalf("valid consume = %q", got)
	}
	if got := consumePendingApproval("token-a", "config.set", "payload-a"); !strings.Contains(got, "유효하지 않거나 만료") {
		t.Fatalf("reused token = %q", got)
	}
}

func TestBoundaryApprovalConcurrentConsumeSucceedsExactlyOnce(t *testing.T) {
	pendingApprovalsMu.Lock()
	pendingApprovals = map[string]pendingApproval{}
	pendingApprovalsMu.Unlock()
	registerPendingApproval("shared", "config.set", "payload")
	const workers = 64
	start := make(chan struct{})
	results := make(chan string, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results <- consumePendingApproval("shared", "config.set", "payload")
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	success := 0
	for result := range results {
		if result == "" {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("successful consumes = %d, want 1", success)
	}
}

func TestBoundarySessionSearchTokenNormalizationMatrix(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "", want: ""},
		{in: "---", want: ""},
		{in: "-Project_", want: "project"},
		{in: "태양광에서", want: "태양광"},
		{in: "프로젝트를", want: "프로젝트"},
		{in: "찾아주세요", want: "찾아주세요"},
		{in: "CI", want: "ci"},
		{in: "PR", want: "pr"},
	}
	for _, tt := range tests {
		if got := normalizeSessionSearchToken(tt.in); got != tt.want {
			t.Fatalf("normalizeSessionSearchToken(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestBoundarySessionSearchSignalMatrix(t *testing.T) {
	tests := []struct {
		token string
		want  bool
	}{
		{token: "", want: false},
		{token: "a", want: false},
		{token: "ab", want: false},
		{token: "abc", want: true},
		{token: "ai", want: true},
		{token: "ci", want: true},
		{token: "pr", want: true},
		{token: "가", want: false},
		{token: "가나", want: true},
		{token: "12", want: true},
	}
	for _, tt := range tests {
		if got := isSessionSearchSignalToken(tt.token); got != tt.want {
			t.Fatalf("isSessionSearchSignalToken(%q) = %v, want %v", tt.token, got, tt.want)
		}
	}
}

func TestBoundaryTruncateUsesRuneBoundary(t *testing.T) {
	tests := []struct {
		text string
		max  int
		want string
	}{
		{text: "", max: 0, want: ""},
		{text: "abc", max: 3, want: "abc"},
		{text: "abcd", max: 3, want: "abc..."},
		{text: "가나다라", max: 3, want: "가나다..."},
		{text: "A📎B", max: 2, want: "A📎..."},
	}
	for _, tt := range tests {
		got := Truncate(tt.text, tt.max)
		if got != tt.want || !utf8.ValidString(got) {
			t.Fatalf("Truncate(%q,%d) = %q", tt.text, tt.max, got)
		}
	}
}

func TestBoundaryFleetFormattingHelpers(t *testing.T) {
	if fleetDash("") != "—" || fleetDash("node") != "node" {
		t.Fatal("fleetDash contract changed")
	}
	value := 7
	if fleetIntp(nil) != "—" || fleetIntp(&value) != "7" {
		t.Fatal("fleetIntp contract changed")
	}
	for kb, want := range map[int64]string{
		0:                "0",
		1024 * 1024:      "1",
		1536 * 1024:      "2",
		10 * 1024 * 1024: "10",
	} {
		if got := fleetGiB(kb); got != want {
			t.Fatalf("fleetGiB(%d) = %q, want %q", kb, got, want)
		}
	}
}

package externalmcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mcpclient"
)

func TestParseMCPServerSpecs(t *testing.T) {
	specs, err := parseMCPServerSpecs("plaud:회의·통화 녹음=npx -y @plaud-ai/mcp@latest; foo=uvx foo-mcp --flag=v ;")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(specs) != 2 {
		t.Fatalf("want 2 specs, got %d: %+v", len(specs), specs)
	}
	if specs[0].Name != "plaud" || specs[0].Label != "회의·통화 녹음" || specs[0].Cmdline != "npx -y @plaud-ai/mcp@latest" {
		t.Fatalf("spec[0] = %+v", specs[0])
	}
	// Command keeps its own '=' (only the first '=' separates key from command).
	if specs[1].Name != "foo" || specs[1].Label != "" || specs[1].Cmdline != "uvx foo-mcp --flag=v" {
		t.Fatalf("spec[1] = %+v", specs[1])
	}

	for _, bad := range []string{
		"no-equals-here",
		"plaud=",
		"9start=cmd", // must start with a letter
		"a=x;a=y",    // duplicate name
		"한글이름=cmd",   // outside [a-z0-9_-]
		"toolongname" + strings.Repeat("x", 40) + "=cmd", // > 32 chars
	} {
		if _, err := parseMCPServerSpecs(bad); err == nil {
			t.Errorf("parse(%q): want error, got nil", bad)
		}
	}

	// Uppercase keys are normalized, not rejected.
	specs, err = parseMCPServerSpecs("UPPER=cmd")
	if err != nil || specs[0].Name != "upper" {
		t.Fatalf("uppercase key should normalize to lowercase: %+v, %v", specs, err)
	}

	// Parse errors must not echo the command part (may carry credentials).
	_, err = parseMCPServerSpecs("bad name=cmd --token=SECRET123")
	if err == nil || strings.Contains(err.Error(), "SECRET123") {
		t.Fatalf("parse error leaks command: %v", err)
	}
}

func TestRegisterMCPServerToolsCreatesNamespacedDeferredDefs(t *testing.T) {
	registry := chat.NewToolRegistry()
	spec := mcpServerSpec{Name: "plaud", Label: "회의·통화 녹음"}
	tools := []mcpclient.ToolInfo{
		{Name: "search.recordings", Description: "search recordings", InputSchema: map[string]any{"type": "object"}},
		{Name: "get_transcript", Description: "fetch a transcript"},
	}
	var calledRemote string
	names := registerMCPServerTools(registry, spec, tools, func(_ context.Context, name string, _ json.RawMessage) (string, error) {
		calledRemote = name
		return "ok", nil
	})
	if len(names) != 2 || names[0] != "plaud_search_recordings" || names[1] != "plaud_get_transcript" {
		t.Fatalf("registered names = %v", names)
	}

	def, ok := registry.DeferredToolDef("plaud_search_recordings")
	if !ok {
		t.Fatal("tool not registered as deferred")
	}
	if !strings.Contains(def.Description, "회의·통화 녹음") || !strings.Contains(def.Description, "MCP(plaud)") {
		t.Fatalf("description missing label/namespace: %q", def.Description)
	}
	// The bridge must call the ORIGINAL remote tool name, not the sanitized one.
	if _, err := def.Fn(context.Background(), nil); err != nil {
		t.Fatalf("bridge call: %v", err)
	}
	if calledRemote != "search.recordings" {
		t.Fatalf("bridge called remote %q, want original name", calledRemote)
	}
}

func TestRegisterMCPServerToolsSkipsRedundantSelfPrefix(t *testing.T) {
	registry := chat.NewToolRegistry()
	spec := mcpServerSpec{Name: "codegraph", Label: "코드지도"}
	tools := []mcpclient.ToolInfo{
		{Name: "codegraph_explore", Description: "explore an area"}, // already self-prefixed
		{Name: "codegraph", Description: "bare server-named tool"},  // equals the namespace
		{Name: "node", Description: "one symbol"},                   // needs the prefix
	}
	names := registerMCPServerTools(registry, spec, tools, func(_ context.Context, _ string, _ json.RawMessage) (string, error) {
		return "ok", nil
	})
	if len(names) != 3 || names[0] != "codegraph_explore" || names[1] != "codegraph" || names[2] != "codegraph_node" {
		t.Fatalf("registered names = %v (want no double codegraph_codegraph_ prefix)", names)
	}
}

func TestClampToolNameTruncatesWithoutCollision(t *testing.T) {
	longA := "plaud_" + strings.Repeat("a", 60) + "_variant_one"
	longB := "plaud_" + strings.Repeat("a", 60) + "_variant_two"
	a := clampToolName(longA, "remote-a")
	b := clampToolName(longB, "remote-b")
	if len(a) > maxLLMToolNameLen || len(b) > maxLLMToolNameLen {
		t.Fatalf("clamped names exceed %d: %q %q", maxLLMToolNameLen, a, b)
	}
	if a == b {
		t.Fatalf("truncated names collide: %q", a)
	}
	if !strings.HasPrefix(a, "plaud_") {
		t.Fatalf("namespace prefix lost: %q", a)
	}
	if short := clampToolName("plaud_echo", "echo"); short != "plaud_echo" {
		t.Fatalf("short name must be untouched: %q", short)
	}
}

func TestSanitizeMCPToolNameNormalizesAndTruncatesLongNames(t *testing.T) {
	cases := map[string]string{
		"search_recordings": "search_recordings",
		"get-transcript":    "get-transcript",
		"weird.name/v2":     "weird_name_v2",
		"한글이름":              "____",
	}
	for in, want := range cases {
		if got := sanitizeMCPToolName(in); got != want {
			t.Errorf("sanitizeMCPToolName(%q) = %q, want %q", in, got, want)
		}
	}
	if got := sanitizeMCPToolName(""); got == "" {
		t.Error("empty name must map to a non-empty fallback")
	}
	long := make([]byte, 200)
	for i := range long {
		long[i] = 'a'
	}
	if got := sanitizeMCPToolName(string(long)); len(got) > 64 {
		t.Errorf("long name not truncated: %d chars", len(got))
	}
}

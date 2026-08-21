package mcpapi

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProtocolVersionsAndToolDefinitionsBoundary(t *testing.T) {
	versions := ProtocolVersions()
	if len(versions) == 0 || versions[0] != protocolVersion2026 {
		t.Fatalf("protocol versions = %v, want newest revision first", versions)
	}
	tools := ToolDefinitions()
	if len(tools) == 0 || tools[0].Name != "wiki_search" {
		t.Fatalf("tool allowlist = %#v, want wiki_search first", tools)
	}
	for _, tool := range tools {
		if !strings.HasPrefix(tool.Method, "miniapp.") {
			t.Errorf("tool %q escapes miniapp RPC surface: %q", tool.Name, tool.Method)
		}
	}
	if InternalID(json.RawMessage(`1`)) == InternalID(json.RawMessage(`"1"`)) {
		t.Fatal("numeric and string JSON-RPC ids must not collide")
	}
}

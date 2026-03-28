package commands

import (
	"testing"
)

func TestParseMcpCommand(t *testing.T) {
	// Not a /mcp command.
	if ParseMcpCommand("hello") != nil {
		t.Error("non-mcp should return nil")
	}

	// /mcp show.
	cmd := ParseMcpCommand("/mcp show")
	if cmd == nil || cmd.Action != "show" {
		t.Errorf("show: %+v", cmd)
	}

	// /mcp show <name>.
	cmd = ParseMcpCommand("/mcp show myserver")
	if cmd == nil || cmd.Action != "show" || cmd.Name != "myserver" {
		t.Errorf("show name: %+v", cmd)
	}

	// /mcp get (alias for show).
	cmd = ParseMcpCommand("/mcp get")
	if cmd == nil || cmd.Action != "show" {
		t.Errorf("get: %+v", cmd)
	}

	// /mcp set name=value.
	cmd = ParseMcpCommand("/mcp set server=true")
	if cmd == nil || cmd.Action != "set" || cmd.Name != "server" {
		t.Errorf("set: %+v", cmd)
	}

	// /mcp unset name.
	cmd = ParseMcpCommand("/mcp unset server")
	if cmd == nil || cmd.Action != "unset" || cmd.Name != "server" {
		t.Errorf("unset: %+v", cmd)
	}

	// /mcp unknown.
	cmd = ParseMcpCommand("/mcp unknown")
	if cmd == nil || cmd.Action != "error" {
		t.Errorf("unknown: %+v", cmd)
	}

	// Bare /mcp defaults to show.
	cmd = ParseMcpCommand("/mcp")
	if cmd == nil || cmd.Action != "show" {
		t.Errorf("bare: %+v", cmd)
	}
}

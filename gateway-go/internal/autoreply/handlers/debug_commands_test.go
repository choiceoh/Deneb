package handlers

import (
	"testing"
)

func TestParseDebugCommand(t *testing.T) {
	// Not a /debug command.
	if ParseDebugCommand("hello") != nil {
		t.Error("non-debug should return nil")
	}

	// /debug show.
	cmd := ParseDebugCommand("/debug show")
	if cmd == nil || cmd.Action != "show" {
		t.Errorf("show: %+v", cmd)
	}

	// /debug reset.
	cmd = ParseDebugCommand("/debug reset")
	if cmd == nil || cmd.Action != "reset" {
		t.Errorf("reset: %+v", cmd)
	}

	// /debug set path=value.
	cmd = ParseDebugCommand("/debug set foo=42")
	if cmd == nil || cmd.Action != "set" || cmd.Path != "foo" {
		t.Errorf("set: %+v", cmd)
	}

	// /debug unset path.
	cmd = ParseDebugCommand("/debug unset bar")
	if cmd == nil || cmd.Action != "unset" || cmd.Path != "bar" {
		t.Errorf("unset: %+v", cmd)
	}

	// /debug unknown action.
	cmd = ParseDebugCommand("/debug unknown")
	if cmd == nil || cmd.Action != "error" {
		t.Errorf("unknown: %+v", cmd)
	}

	// Bare /debug defaults to show.
	cmd = ParseDebugCommand("/debug")
	if cmd == nil || cmd.Action != "show" {
		t.Errorf("bare: %+v", cmd)
	}
}

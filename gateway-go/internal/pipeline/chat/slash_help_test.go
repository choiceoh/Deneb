package chat

import (
	"strings"
	"testing"
)

func TestParseSlashCommand_Help(t *testing.T) {
	for _, in := range []string{"/help", "/?", "/도움말", "/명령어", "/commands"} {
		got := ParseSlashCommand(in)
		if got == nil || !got.Handled || got.Command != "help" {
			t.Errorf("ParseSlashCommand(%q) = %+v, want command=help", in, got)
		}
	}
}

func TestRenderSlashHelp_ListsKeyCommands(t *testing.T) {
	out := renderSlashHelp()
	// Must surface the otherwise-undiscoverable commands, including /pin.
	for _, want := range []string{"/help", "/reset", "/pin", "/unpin", "/pins", "/status", "/model"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderSlashHelp() missing %q in:\n%s", want, out)
		}
	}
}

func TestBuiltinSlashCommands_NoEmptyNamesOrDescs(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range BuiltinSlashCommands() {
		if strings.TrimSpace(c.Name) == "" || strings.TrimSpace(c.Desc) == "" {
			t.Errorf("command has empty name or desc: %+v", c)
		}
		if seen[c.Name] {
			t.Errorf("duplicate command name in registry: %q", c.Name)
		}
		seen[c.Name] = true
	}
}

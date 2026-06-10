package chat

import (
	"strings"
	"testing"
)

func TestParseSlashCommand_Help(t *testing.T) {
	for _, in := range []string{"/help", "/?", "/도움말", "/help me"} {
		got := ParseSlashCommand(in)
		if got == nil || !got.Handled || got.Command != "help" {
			t.Errorf("ParseSlashCommand(%q) = %+v, want handled help command", in, got)
		}
	}
}

func TestSlashHelpText_ListsCommands(t *testing.T) {
	text := slashHelpText()
	for _, want := range []string{"/help", "/status", "/reset", "/kill", "/rollback", "/update", "/restart"} {
		if !strings.Contains(text, want) {
			t.Errorf("slashHelpText() missing %q", want)
		}
	}
	// Removed user commands must not be advertised.
	for _, gone := range []string{"/pin", "/model", "/think", "/mode", "/mail", "/insights"} {
		if strings.Contains(text, gone+" ") || strings.Contains(text, gone+"`") {
			t.Errorf("slashHelpText() still lists removed command %q", gone)
		}
	}
}

// TestSlashHelpEntriesAreParseable keeps the /help table in sync with the
// parser: every listed command's name must resolve to a handled builtin, so a
// help entry can never advertise a command that does not exist.
func TestSlashHelpEntriesAreParseable(t *testing.T) {
	for _, e := range slashBuiltinHelp {
		name := strings.Fields(e.usage)[0] // "/model <이름|역할>" -> "/model"
		got := ParseSlashCommand(name)
		if got == nil || !got.Handled {
			t.Errorf("help entry %q: ParseSlashCommand(%q) not handled (%+v)", e.usage, name, got)
		}
	}
}

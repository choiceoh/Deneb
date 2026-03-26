package autoreply

import "testing"

func testCommands() []ChatCommandDefinition {
	return []ChatCommandDefinition{
		{Key: "new", NativeName: "new", Description: "Start new session", TextAliases: []string{"/new"}, Scope: ScopeBoth},
		{Key: "reset", NativeName: "reset", Description: "Reset session", TextAliases: []string{"/reset"}, Scope: ScopeBoth},
		{Key: "status", NativeName: "status", Description: "Show status", TextAliases: []string{"/status"}, Scope: ScopeBoth},
		{Key: "model", NativeName: "model", Description: "Set model", TextAliases: []string{"/model"}, AcceptsArgs: true, Scope: ScopeBoth},
		{Key: "config", Description: "Configure", TextAliases: []string{"/config"}, AcceptsArgs: true, Scope: ScopeText},
		{Key: "bash", NativeName: "bash", Description: "Run bash", TextAliases: []string{"/bash", "/sh"}, AcceptsArgs: true, Scope: ScopeBoth,
			Args: []CommandArgDefinition{{Name: "command", Type: "string", CaptureRemaining: true}},
		},
	}
}

func TestCommandRegistryNormalize(t *testing.T) {
	r := NewCommandRegistry(testCommands())

	tests := []struct {
		name        string
		raw         string
		botUsername string
		want        string
	}{
		{"simple command", "/new", "", "/new"},
		{"with bot mention", "/new@MyBot", "MyBot", "/new"},
		{"colon syntax", "/model:gpt-4", "", "/model gpt-4"},
		{"alias", "/sh echo hi", "", "/bash echo hi"},
		{"unknown command", "/unknown", "", "/unknown"},
		{"non-command", "hello world", "", "hello world"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.NormalizeCommandBody(tt.raw, tt.botUsername)
			if got != tt.want {
				t.Errorf("NormalizeCommandBody(%q, %q) = %q, want %q", tt.raw, tt.botUsername, got, tt.want)
			}
		})
	}
}

func TestCommandRegistryHasControlCommand(t *testing.T) {
	r := NewCommandRegistry(testCommands())

	tests := []struct {
		name string
		text string
		want bool
	}{
		{"known command", "/new", true},
		{"with args", "/model gpt-4", true},
		{"alias", "/sh ls", true},
		{"unknown", "/unknown", false},
		{"non-command", "hello", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.HasControlCommand(tt.text, "")
			if got != tt.want {
				t.Errorf("HasControlCommand(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

func TestMaybeResolveTextAlias(t *testing.T) {
	r := NewCommandRegistry(testCommands())

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"known command", "/new", "/new"},
		{"alias", "/sh", "/bash"},
		{"unknown", "/unknown", ""},
		{"not command", "hello", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.MaybeResolveTextAlias(tt.raw)
			if got != tt.want {
				t.Errorf("MaybeResolveTextAlias(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestHasInlineCommandTokens(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		{"hello /status", true},
		{"hello !cmd", true},
		{"/new", true},
		{"hello world", false},
		{"", false},
		{"   ", false},
		{"http://example.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			got := HasInlineCommandTokens(tt.text)
			if got != tt.want {
				t.Errorf("HasInlineCommandTokens(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

func TestParseCommandArgs(t *testing.T) {
	cmd := &ChatCommandDefinition{
		Key:         "bash",
		AcceptsArgs: true,
		Args:        []CommandArgDefinition{{Name: "command", Type: "string", CaptureRemaining: true}},
	}

	t.Run("with args", func(t *testing.T) {
		args := ParseCommandArgs(cmd, "echo hello world")
		if args == nil {
			t.Fatal("expected args")
		}
		if args.Values["command"] != "echo hello world" {
			t.Errorf("expected 'echo hello world', got %q", args.Values["command"])
		}
	})

	t.Run("empty args", func(t *testing.T) {
		args := ParseCommandArgs(cmd, "")
		if args != nil {
			t.Error("expected nil for empty args")
		}
	})
}

func TestBuildCommandText(t *testing.T) {
	if got := BuildCommandText("model", "gpt-4"); got != "/model gpt-4" {
		t.Errorf("got %q", got)
	}
	if got := BuildCommandText("new", ""); got != "/new" {
		t.Errorf("got %q", got)
	}
}

func TestListNativeCommandSpecs(t *testing.T) {
	r := NewCommandRegistry(testCommands())

	specs := r.ListNativeCommandSpecs("")
	// /config is text-only, should not appear.
	for _, s := range specs {
		if s.Name == "config" {
			t.Error("text-only command should not appear in native specs")
		}
	}

	// Slack should override /status → /agentstatus.
	slackSpecs := r.ListNativeCommandSpecs("slack")
	for _, s := range slackSpecs {
		if s.Name == "agentstatus" {
			return // found the override
		}
	}
	t.Error("expected 'agentstatus' in slack native specs")
}

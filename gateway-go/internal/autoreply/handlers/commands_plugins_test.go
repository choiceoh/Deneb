package handlers

import "testing"

func TestParsePluginsCommand_List(t *testing.T) {
	cmd := ParsePluginsCommand("/plugins")
	if cmd == nil {
		t.Fatal("expected command")
	}
	if cmd.Action != PluginActionList {
		t.Fatalf("expected list, got %s", cmd.Action)
	}
}

func TestParsePluginsCommand_ListExplicit(t *testing.T) {
	cmd := ParsePluginsCommand("/plugins list")
	if cmd == nil {
		t.Fatal("expected command")
	}
	if cmd.Action != PluginActionList {
		t.Fatalf("expected list, got %s", cmd.Action)
	}
}

func TestParsePluginsCommand_Inspect(t *testing.T) {
	cmd := ParsePluginsCommand("/plugins inspect my-plugin")
	if cmd == nil {
		t.Fatal("expected command")
	}
	if cmd.Action != PluginActionInspect {
		t.Fatalf("expected inspect, got %s", cmd.Action)
	}
	if cmd.Name != "my-plugin" {
		t.Fatalf("expected name=my-plugin, got %q", cmd.Name)
	}
}

func TestParsePluginsCommand_Enable(t *testing.T) {
	cmd := ParsePluginsCommand("/plugins enable telegram")
	if cmd == nil {
		t.Fatal("expected command")
	}
	if cmd.Action != PluginActionEnable {
		t.Fatalf("expected enable, got %s", cmd.Action)
	}
	if cmd.Name != "telegram" {
		t.Fatalf("expected name=telegram, got %q", cmd.Name)
	}
}

func TestParsePluginsCommand_Disable(t *testing.T) {
	cmd := ParsePluginsCommand("/plugins disable telegram")
	if cmd == nil {
		t.Fatal("expected command")
	}
	if cmd.Action != PluginActionDisable {
		t.Fatalf("expected disable, got %s", cmd.Action)
	}
}

func TestParsePluginsCommand_EnableNoName(t *testing.T) {
	cmd := ParsePluginsCommand("/plugins enable")
	if cmd == nil {
		t.Fatal("expected command")
	}
	if cmd.Action != PluginActionError {
		t.Fatalf("expected error, got %s", cmd.Action)
	}
}

func TestParsePluginsCommand_NotPlugins(t *testing.T) {
	cmd := ParsePluginsCommand("hello world")
	if cmd != nil {
		t.Fatal("expected nil for non-plugins command")
	}
}

func TestParsePluginsCommand_Singular(t *testing.T) {
	cmd := ParsePluginsCommand("/plugin list")
	if cmd == nil {
		t.Fatal("expected command")
	}
	if cmd.Action != PluginActionList {
		t.Fatalf("expected list, got %s", cmd.Action)
	}
}

func TestFindPlugin(t *testing.T) {
	report := PluginStatusReport{
		Plugins: []PluginRecord{
			{ID: "telegram", Name: "Telegram Bot", Status: "loaded"},
			{ID: "discord", Name: "Discord", Status: "disabled"},
		},
	}

	p := FindPlugin(report, "telegram")
	if p == nil || p.ID != "telegram" {
		t.Fatal("expected to find telegram by ID")
	}

	p = FindPlugin(report, "Telegram Bot")
	if p == nil || p.ID != "telegram" {
		t.Fatal("expected to find telegram by name")
	}

	p = FindPlugin(report, "unknown")
	if p != nil {
		t.Fatal("expected nil for unknown plugin")
	}
}

func TestFormatPluginLabel(t *testing.T) {
	tests := []struct {
		plugin PluginRecord
		want   string
	}{
		{PluginRecord{ID: "test", Name: "test"}, "test"},
		{PluginRecord{ID: "test", Name: ""}, "test"},
		{PluginRecord{ID: "test", Name: "Test Plugin"}, "Test Plugin (test)"},
	}
	for _, tt := range tests {
		got := FormatPluginLabel(tt.plugin)
		if got != tt.want {
			t.Errorf("FormatPluginLabel(%v) = %q, want %q", tt.plugin, got, tt.want)
		}
	}
}

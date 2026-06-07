package chat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSlashCommand(t *testing.T) {
	tests := []struct {
		input   string
		wantNil bool
		wantCmd string
		wantArg string
	}{
		{"/reset", false, "reset", ""},
		{"/kill", false, "kill", ""},
		{"/stop", false, "kill", ""},
		{"/cancel", false, "kill", ""},
		{"/status", false, "status", ""},
		{"/model claude-opus-4-6", false, "model", "claude-opus-4-6"},
		{"/model", false, "model", ""},
		{"/think", false, "think", ""},
		{"/unknown", true, "", ""},
		{"hello", true, "", ""},
		{"", true, "", ""},
		{" /reset ", false, "reset", ""},
		{"/reset@MyBot", false, "reset", ""},
		{"/model@MyBot claude-opus-4-6", false, "model", "claude-opus-4-6"},
		{"/status@mybot", false, "status", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ParseSlashCommand(tt.input, "", nil)
			if tt.wantNil {
				if got != nil {
					t.Errorf("ParseSlashCommand(%q) = %+v, want nil", tt.input, got)
				}
				return
			}
			if got == nil {
				t.Fatalf("ParseSlashCommand(%q) = nil, want command", tt.input)
			}
			if got.Command != tt.wantCmd {
				t.Errorf("ParseSlashCommand(%q).Command = %q, want %q", tt.input, got.Command, tt.wantCmd)
			}
			if got.Args != tt.wantArg {
				t.Errorf("ParseSlashCommand(%q).Args = %q, want %q", tt.input, got.Args, tt.wantArg)
			}
		})
	}
}

func TestParseSlashCommand_Mail(t *testing.T) {
	for _, input := range []string{"/mail", "/메일", "/mail@MyBot"} {
		got := ParseSlashCommand(input, "", nil)
		if got == nil {
			t.Fatalf("ParseSlashCommand(%q) = nil, want command", input)
		}
		if got.Command != "mail" {
			t.Errorf("ParseSlashCommand(%q).Command = %q, want %q", input, got.Command, "mail")
		}
		if !got.Handled {
			t.Errorf("ParseSlashCommand(%q).Handled = false, want true", input)
		}
	}
}

func TestParseSlashCommand_UsesWorkspaceScopedSkillCache(t *testing.T) {
	InvalidateSkillsCache()
	workspaceA := t.TempDir()
	workspaceB := t.TempDir()
	writeTestSystemSkill(t, workspaceA, "alpha")
	writeTestSystemSkill(t, workspaceB, "beta")

	if got := ParseSlashCommand("/alpha", workspaceA, nil); got == nil || got.SkillName != "alpha" {
		t.Fatalf("expected alpha skill from workspace A, got %+v", got)
	}
	if got := ParseSlashCommand("/beta", workspaceB, nil); got == nil || got.SkillName != "beta" {
		t.Fatalf("expected beta skill from workspace B, got %+v", got)
	}
	if got := ParseSlashCommand("/alpha", workspaceB, nil); got != nil {
		t.Fatalf("expected workspace B not to reuse workspace A skills, got %+v", got)
	}
}

func TestLoadCachedSkillsPrompt_CachesPerWorkspace(t *testing.T) {
	InvalidateSkillsCache()
	workspaceA := t.TempDir()
	workspaceB := t.TempDir()
	writeTestSystemSkill(t, workspaceA, "alpha")
	writeTestSystemSkill(t, workspaceB, "beta")

	promptA := loadCachedSkillsPrompt(workspaceA, nil)
	if !strings.Contains(promptA, "alpha") {
		t.Fatalf("expected workspace A prompt to mention alpha, got %q", promptA)
	}
	promptB := loadCachedSkillsPrompt(workspaceB, nil)
	if !strings.Contains(promptB, "beta") {
		t.Fatalf("expected workspace B prompt to mention beta, got %q", promptB)
	}
	if strings.Contains(promptB, "alpha") {
		t.Fatalf("expected workspace B prompt not to mention alpha, got %q", promptB)
	}
}

func writeTestSystemSkill(t *testing.T, workspaceDir, name string) {
	t.Helper()
	skillDir := filepath.Join(workspaceDir, "skills", "coding", name)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	content := "---\n" +
		"name: " + name + "\n" +
		"type: system\n" +
		"description: \"test system skill\"\n" +
		"---\n\n" +
		"# " + name + "\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
}

package chat

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeConfirmArg(t *testing.T) {
	tests := []struct {
		raw  string
		want confirmIntent
	}{
		{"", confirmIntentBare},
		{"   ", confirmIntentBare},
		{"확인", confirmIntentYes},
		{"실행", confirmIntentYes},
		{"진행", confirmIntentYes},
		{"응", confirmIntentYes},
		{"네", confirmIntentYes},
		{"ㅇㅇ", confirmIntentYes},
		{"confirm", confirmIntentYes},
		{"YES", confirmIntentYes},
		{" Y ", confirmIntentYes},
		{"ok", confirmIntentYes},
		{"go", confirmIntentYes},
		{"maybe", confirmIntentUnknown},
		{"취소", confirmIntentUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			if got := normalizeConfirmArg(tt.raw); got != tt.want {
				t.Errorf("normalizeConfirmArg(%q) = %d, want %d", tt.raw, got, tt.want)
			}
		})
	}
}

func TestUpdateVersionNote(t *testing.T) {
	tests := []struct {
		version string
		want    string
	}{
		{"", ""},
		{"dev", ""},
		{"4.7.0", " (현재 v4.7.0)"},
	}
	for _, tt := range tests {
		if got := updateVersionNote(tt.version); got != tt.want {
			t.Errorf("updateVersionNote(%q) = %q, want %q", tt.version, got, tt.want)
		}
	}
}

func TestTruncateUpdateOutput(t *testing.T) {
	short := "build failed: undefined symbol"
	if got := truncateUpdateOutput(short); got != short {
		t.Errorf("short output should pass through unchanged, got %q", got)
	}

	long := strings.Repeat("x", 5000) + "REAL_ERROR_AT_END"
	got := truncateUpdateOutput(long)
	if len([]rune(got)) > 1100 {
		t.Errorf("truncated output too long: %d runes", len([]rune(got)))
	}
	if !strings.HasSuffix(got, "REAL_ERROR_AT_END") {
		t.Error("truncateUpdateOutput must keep the tail (the real error)")
	}
	if !strings.Contains(got, "생략") {
		t.Error("truncateUpdateOutput should mark that the head was dropped")
	}
}

func TestUpdatePrechecks(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()

	git := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	git("init")
	git("config", "user.email", "test@example.com")
	git("config", "user.name", "Test")
	git("checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "a.txt")
	git("commit", "-m", "initial")

	// On a non-main branch → hard block.
	if block, _ := updatePrechecks(ctx, root); block != updateBlockHard {
		t.Errorf("feature branch: block = %d, want updateBlockHard (%d)", block, updateBlockHard)
	}

	// On main with a clean worktree → proceed.
	git("branch", "-m", "main")
	if block, info := updatePrechecks(ctx, root); block != updateBlockNone {
		t.Errorf("clean main: block = %d info = %q, want updateBlockNone (%d)", block, info, updateBlockNone)
	}

	// On main with an uncommitted change → dirty hand-off, status carried.
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	block, info := updatePrechecks(ctx, root)
	if block != updateBlockDirty {
		t.Fatalf("dirty main: block = %d, want updateBlockDirty (%d)", block, updateBlockDirty)
	}
	if !strings.Contains(info, "a.txt") {
		t.Errorf("dirty main: info = %q, want it to name the changed file", info)
	}

	// Untracked-only worktree → auto-stash path (revert the tracked change
	// first, then add an untracked file).
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "build-artifact.tmp"), []byte("junk\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	block, info = updatePrechecks(ctx, root)
	if block != updateBlockUntracked {
		t.Fatalf("untracked-only main: block = %d, want updateBlockUntracked (%d)", block, updateBlockUntracked)
	}
	if !strings.Contains(info, "build-artifact.tmp") {
		t.Errorf("untracked-only main: info = %q, want it to name the untracked file", info)
	}

	// Untracked + tracked mix → still treated as dirty (tracked changes must
	// not be silently stashed).
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("changed again\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	block, _ = updatePrechecks(ctx, root)
	if block != updateBlockDirty {
		t.Errorf("mixed dirty: block = %d, want updateBlockDirty (%d)", block, updateBlockDirty)
	}
}

func TestIsUntrackedOnly(t *testing.T) {
	tests := []struct {
		name   string
		status string
		want   bool
	}{
		{"empty", "", false},
		{"single untracked", "?? foo.txt", true},
		{"multiple untracked", "?? a\n?? b\n?? c", true},
		{"trailing newline", "?? a\n?? b\n", true},
		{"untracked plus modified", "?? a\n M b", false},
		{"modified only", " M file.go", false},
		{"staged only", "M  file.go", false},
		{"renamed", "R  old -> new", false},
		{"deleted", " D removed.txt", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isUntrackedOnly(tt.status); got != tt.want {
				t.Errorf("isUntrackedOnly(%q) = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

func TestParsePRNumber(t *testing.T) {
	tests := []struct {
		subject string
		want    string
	}{
		{"feat(telegram): add /update slash command for in-app updates (#1643)", "1643"},
		{"feat(provider): add Xiaomi MiMo Token Plan and Kimi Code providers (#1638)", "1638"},
		{"feat(telegram): add /update (slash) command (#1644)", "1644"},
		{"chore(main): release 4.22.3 (#42)", "42"},
		{"fix: a plain commit with no PR ref", ""},
		{"refactor: trailing parens (not a pr)", ""},
		{"feat: weird empty ref (#)", ""},
		{"feat: missing close paren (#1643", ""},
		{"feat: non-numeric ref (#abc)", ""},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.subject, func(t *testing.T) {
			if got := parsePRNumber(tt.subject); got != tt.want {
				t.Errorf("parsePRNumber(%q) = %q, want %q", tt.subject, got, tt.want)
			}
		})
	}
}

func TestParseSlashCommand_Update(t *testing.T) {
	tests := []struct {
		input   string
		wantArg string
	}{
		{"/update", ""},
		{"/update 확인", "확인"},
		{"/update confirm", "confirm"},
		{"/업데이트", ""},
		{"/업데이트 확인", "확인"},
		{"/update@DenebBot", ""},
		{"/update@DenebBot 확인", "확인"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ParseSlashCommand(tt.input)
			if got == nil {
				t.Fatalf("ParseSlashCommand(%q) = nil, want command", tt.input)
			}
			if got.Command != "update" {
				t.Errorf("ParseSlashCommand(%q).Command = %q, want %q", tt.input, got.Command, "update")
			}
			if !got.Handled {
				t.Errorf("ParseSlashCommand(%q).Handled = false, want true", tt.input)
			}
			if got.Args != tt.wantArg {
				t.Errorf("ParseSlashCommand(%q).Args = %q, want %q", tt.input, got.Args, tt.wantArg)
			}
		})
	}
}

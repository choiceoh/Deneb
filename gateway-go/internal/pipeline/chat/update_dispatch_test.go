package chat

import (
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

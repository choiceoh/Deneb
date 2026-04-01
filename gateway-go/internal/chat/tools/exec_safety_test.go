package tools

import "testing"

func TestCheckDestructiveCommand(t *testing.T) {
	tests := []struct {
		command string
		danger  bool
	}{
		{"rm -rf /", true},
		{"rm -fr /tmp/build", true},
		{"rm file.txt", false},
		{"git reset --hard HEAD~3", true},
		{"git clean -fd", true},
		{"git push --force origin main", true},
		{"git push origin main", false},
		{"git checkout -- .", true},
		{"ls -la", false},
		{"sudo rm /etc/foo", true},
		{"chmod -R 777 /var", true},
		{"kill -9 1234", true},
		{"kill 1234", false},
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			checks := CheckDestructiveCommand(tt.command)
			if tt.danger && len(checks) == 0 {
				t.Error("expected destructive warning")
			}
			if !tt.danger && len(checks) > 0 {
				t.Errorf("unexpected warning: %v", checks[0].Description)
			}
		})
	}
}

func TestFormatDestructiveWarnings(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		if FormatDestructiveWarnings(nil) != "" {
			t.Error("expected empty string")
		}
	})

	t.Run("with warnings", func(t *testing.T) {
		checks := CheckDestructiveCommand("rm -rf /tmp && git push --force")
		s := FormatDestructiveWarnings(checks)
		if s == "" {
			t.Error("expected non-empty warning")
		}
	})
}

func TestDetectFileModification(t *testing.T) {
	tests := []struct {
		command string
		want    string
	}{
		{"sed -i 's/foo/bar/' file.txt", "sed_in_place"},
		{"sed --in-place 's/a/b/' f", "sed_in_place"},
		{"sed 's/foo/bar/' file.txt", ""},
		{"echo hello > file.txt", "redirect"},
		{"cat foo | tee output.txt", "tee"},
		{"ls -la", ""},
		{"grep foo | grep bar", ""},
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			got := DetectFileModification(tt.command)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

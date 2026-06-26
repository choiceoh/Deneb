package tools

import (
	"strings"
	"testing"
)

func TestCheckDestructiveCommand(t *testing.T) {
	tests := []struct {
		command string
		danger  bool
	}{
		{"rm -rf /", true},
		{"rm -fr /tmp/build", true},
		{"rm -r -f /tmp/build", true},
		{"rm -f -r /tmp/build", true},
		{"rm file.txt", false},
		{"git reset --hard HEAD~3", true},
		{"git clean -fd", true},
		{"git clean -d -f", true},
		{"git push --force origin main", true},
		{"git push --force-with-lease origin main", false},
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

func TestCheckCatastrophicCommand(t *testing.T) {
	tests := []struct {
		command string
		block   bool
	}{
		// Catastrophic — must block.
		{"rm -rf /", true},
		{"rm -rf /*", true},
		{"rm -rf / ", true},
		{"rm -rf --no-preserve-root /", true},
		{"rm -rf ~", true},
		{"rm -rf ~/", true},
		{"rm -rf $HOME", true},
		{"rm -rf ${HOME}", true},
		{"rm -rf /etc", true},
		{"rm -rf /usr/*", true},
		{"rm -rf /home", true},
		{"sudo rm -rf /", true},
		{"chmod -R 777 /", true},
		{"chmod -R 755 /etc", true},
		{"chown -R user:user /usr", true},
		{"dd if=/dev/zero of=/dev/sda", true},
		{"mkfs.ext4 /dev/sdb1", true},
		{"echo boom > /dev/sda", true},
		{":(){ :|:& };:", true},

		// Legitimate or recoverable — must NOT block (warn-only at most).
		{"rm -rf ./build", false},
		{"rm -fr /tmp/build", false},
		{"rm -rf ~/project/node_modules", false},
		{"rm -rf /etc/nginx", false}, // a subdir, not all of /etc
		{"rm -rf /home/choiceoh/scratch", false},
		{"rm file.txt", false},
		{"chmod -R 777 ./mydir", false},
		{"dd if=/dev/sda of=backup.img", false}, // reading a disk to a file is fine
		{"dd if=disk.img of=out.img", false},
		{"echo ok > /dev/null", false},
		{"git reset --hard HEAD~1", false},
		{"cat /etc/hostname", false},
		{"ls /etc", false},
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			blocked := CheckCatastrophicCommand(tt.command)
			if tt.block && len(blocked) == 0 {
				t.Errorf("expected %q to be blocked", tt.command)
			}
			if !tt.block && len(blocked) > 0 {
				t.Errorf("unexpected block of %q: %v", tt.command, blocked[0].Description)
			}
		})
	}
}

func TestFormatCatastrophicRefusal(t *testing.T) {
	if FormatCatastrophicRefusal(nil) != "" {
		t.Error("expected empty string for no checks")
	}
	s := FormatCatastrophicRefusal(CheckCatastrophicCommand("rm -rf /"))
	if s == "" {
		t.Fatal("expected a non-empty refusal message")
	}
	if !strings.Contains(s, "실행 거부") {
		t.Errorf("refusal message missing the Korean refusal marker: %q", s)
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

func TestInPlaceFileTargets(t *testing.T) {
	contains := func(xs []string, s string) bool {
		for _, x := range xs {
			if x == s {
				return true
			}
		}
		return false
	}
	cases := []struct {
		command string
		must    []string // targets that MUST be captured (so they get checkpointed)
		mustNot []string // tokens that must NOT appear (avoid spurious/cross-segment)
	}{
		{"sed -i 's/a/b/' main.go", []string{"main.go"}, nil},
		{"sed -i.bak 's/x/y/' config.yaml", []string{"config.yaml"}, []string{"-i.bak"}},
		{"sed --in-place 's/x/y/' a.go b.go", []string{"a.go", "b.go"}, nil},
		{"echo hi > out.txt", []string{"out.txt"}, nil},
		{"cat a | sed -i 's/x/y/' b.go", []string{"b.go"}, []string{"a"}}, // cross-segment isolation
		{"cat x >> log.txt", nil, []string{"log.txt"}},                    // append (>>) is not an in-place overwrite
		{"ls -la", nil, []string{"-la"}},                                  // no sed/redirect → nothing
		{"grep foo file.go", nil, []string{"file.go"}},                    // read-only → not a target
	}
	for _, tc := range cases {
		got := InPlaceFileTargets(tc.command)
		for _, m := range tc.must {
			if !contains(got, m) {
				t.Errorf("%q: expected target %q in %v", tc.command, m, got)
			}
		}
		for _, n := range tc.mustNot {
			if contains(got, n) {
				t.Errorf("%q: did not expect %q in %v", tc.command, n, got)
			}
		}
	}
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
		{"echo hello >> file.txt", ""},
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

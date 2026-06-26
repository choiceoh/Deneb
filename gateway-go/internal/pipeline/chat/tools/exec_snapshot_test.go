package tools

import (
	"reflect"
	"sort"
	"testing"
)

// targetSet renders execMutationTargets output as raw→mustExist for
// order-independent comparison.
func targetSet(ts []execTarget) map[string]bool {
	m := make(map[string]bool, len(ts))
	for _, t := range ts {
		m[t.raw] = t.mustExist
	}
	return m
}

func TestExecMutationTargets(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    map[string]bool // raw target → mustExist
	}{
		{
			name:    "sed in place drops script keeps file",
			command: "sed -i 's/foo/bar/' file.txt",
			want:    map[string]bool{"file.txt": true},
		},
		{
			name:    "sed in place multiple files",
			command: "sed -i s/a/b/ one.go two.go",
			want:    map[string]bool{"one.go": true, "two.go": true},
		},
		{
			name:    "sed in place with -e script flag",
			command: "sed -i -e 's/a/b/' main.go",
			want:    map[string]bool{"main.go": true},
		},
		{
			name:    "sed without -i is not a mutation",
			command: "sed 's/foo/bar/' file.txt",
			want:    map[string]bool{},
		},
		{
			name:    "redirect creates file (create-capable)",
			command: "echo hello > out.txt",
			want:    map[string]bool{"out.txt": false},
		},
		{
			name:    "append redirect is ignored",
			command: "echo hello >> out.txt",
			want:    map[string]bool{},
		},
		{
			name:    "redirect to dev null is ignored",
			command: "cat foo > /dev/null",
			want:    map[string]bool{},
		},
		{
			name:    "tee is create-capable",
			command: "echo x | tee report.txt",
			want:    map[string]bool{"report.txt": false},
		},
		{
			name:    "tee append flag still captures file",
			command: "echo x | tee -a report.txt",
			want:    map[string]bool{"report.txt": false},
		},
		{
			name:    "mv captures source and dest (must-exist)",
			command: "mv a.txt b.txt",
			want:    map[string]bool{"a.txt": true, "b.txt": true},
		},
		{
			name:    "cp captures source and dest (must-exist)",
			command: "cp src.txt dst.txt",
			want:    map[string]bool{"src.txt": true, "dst.txt": true},
		},
		{
			name:    "rm captures targets, drops flags",
			command: "rm -rf ./build old.log",
			want:    map[string]bool{"./build": true, "old.log": true},
		},
		{
			name:    "leading env assignment is stripped",
			command: "FOO=bar rm trash.txt",
			want:    map[string]bool{"trash.txt": true},
		},
		{
			name:    "sudo prefix is stripped",
			command: "sudo rm /tmp/scratch",
			want:    map[string]bool{"/tmp/scratch": true},
		},
		{
			name:    "glob targets are skipped",
			command: "rm *.tmp",
			want:    map[string]bool{},
		},
		{
			name:    "variable targets are skipped",
			command: "rm $TMPDIR/x",
			want:    map[string]bool{},
		},
		{
			name:    "non-mutating command yields nothing",
			command: "ls -la /etc",
			want:    map[string]bool{},
		},
		{
			name:    "chained cd then sed",
			command: "cd sub && sed -i s/a/b/ x.go",
			want:    map[string]bool{"x.go": true},
		},
		{
			name:    "redirect plus mutation in pipeline",
			command: "grep foo bar.txt | tee hits.txt",
			want:    map[string]bool{"hits.txt": false},
		},
		{
			name:    "quoted target is unquoted",
			command: `rm "my file.txt"`,
			want:    map[string]bool{"my file.txt": true},
		},
		{
			name:    "double dash ends option parsing",
			command: "rm -- -weird-name.txt",
			want:    map[string]bool{"-weird-name.txt": true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := targetSet(execMutationTargets(tt.command))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("execMutationTargets(%q) = %v, want %v", tt.command, got, tt.want)
			}
		})
	}
}

func TestLooksFileMutating(t *testing.T) {
	// looksFileMutating must be a strict superset of execMutationTargets:
	// anything that yields targets must pass the pre-filter.
	mutating := []string{
		"sed -i s/a/b/ f",
		"echo x > out.txt",
		"> out.txt", // segment-leading redirect
		"echo x | tee f",
		"mv a b",
		"cp a b",
		"rm f",
		"cd x && rm f",
	}
	for _, c := range mutating {
		if !looksFileMutating(c) {
			t.Errorf("looksFileMutating(%q) = false, want true", c)
		}
	}

	nonMutating := []string{
		"ls -la",
		"cat file.txt",
		"grep foo bar | grep baz",
		"echo hi >> log.txt",
		"sed 's/a/b/' f", // no -i
		"git status",
	}
	for _, c := range nonMutating {
		if looksFileMutating(c) {
			t.Errorf("looksFileMutating(%q) = true, want false", c)
		}
	}
}

func TestNonFlagArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{"drops flags", []string{"-r", "-f", "file"}, []string{"file"}},
		{"double dash terminator", []string{"--", "-weird"}, []string{"-weird"}},
		{"lone dash dropped", []string{"-", "real"}, []string{"real"}},
		{"long flag dropped", []string{"--in-place", "f"}, []string{"f"}},
		{"all operands", []string{"a", "b"}, []string{"a", "b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nonFlagArgs(tt.args)
			// Normalise nil vs empty for comparison.
			if got == nil {
				got = []string{}
			}
			want := tt.want
			sort.Strings(got)
			sort.Strings(want)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("nonFlagArgs(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

func TestUnquote(t *testing.T) {
	cases := map[string]string{
		`"hi"`:        "hi",
		`'hi'`:        "hi",
		`hi`:          "hi",
		`"unbalanced`: `"unbalanced`,
		`""`:          "",
	}
	for in, want := range cases {
		if got := unquote(in); got != want {
			t.Errorf("unquote(%q) = %q, want %q", in, got, want)
		}
	}
}

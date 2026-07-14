package gowire

import (
	"go/ast"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseStructsFindsDeclarationAndSpecMarkers(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"a.go": `package sample
//deneb:wire
type Alpha struct { Value string }
type Plain struct { Enabled bool }
`,
		"b.go": `package sample
type (
  //deneb:wire
  Zeta struct { Count int }
  Alias string
)
`,
		"ignored_test.go": `package sample
//deneb:wire
type TestOnly struct{}
`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	nested := filepath.Join(dir, "files")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "wire.go"), []byte(`package files
//deneb:wire
type Nested struct { Path string }
`), 0o600); err != nil {
		t.Fatal(err)
	}

	structs, marked, err := ParseStructs(dir, "deneb:wire")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(marked, []string{"Alpha", "Nested", "Zeta"}) {
		t.Fatalf("marked = %v", marked)
	}
	for _, name := range []string{"Alpha", "Nested", "Plain", "Zeta"} {
		if structs[name] == nil {
			t.Errorf("missing struct %q", name)
		}
	}
	if structs["Alias"] != nil || structs["TestOnly"] != nil {
		t.Fatalf("unexpected structs = %v", structs)
	}
}

func TestParseStructsReportsSourceErrors(t *testing.T) {
	if _, _, err := ParseStructs(filepath.Join(t.TempDir(), "missing"), "deneb:wire"); err == nil {
		t.Fatal("missing directory accepted")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "broken.go"), []byte("package sample\ntype"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ParseStructs(dir, "deneb:wire"); err == nil {
		t.Fatal("malformed source accepted")
	}
}

func TestJSONFieldNameParsesTagsAndIgnoresHyphenFields(t *testing.T) {
	tests := []struct {
		name, tag, want string
		skip            bool
	}{
		{"fallback", "", "DisplayName", false},
		{"renamed", "`json:\"display_name,omitempty\"`", "display_name", false},
		{"options only", "`json:\",omitempty\"`", "DisplayName", false},
		{"ignored", "`json:\"-\"`", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			field := &ast.Field{Names: []*ast.Ident{{Name: "DisplayName"}}}
			if tt.tag != "" {
				field.Tag = &ast.BasicLit{Value: tt.tag}
			}
			got, skip := JSONFieldName(field)
			if got != tt.want || skip != tt.skip {
				t.Fatalf("JSONFieldName = %q, %v", got, skip)
			}
		})
	}
}

func TestExportedNameUppercasesFirstRuneAndHandlesEmptyInput(t *testing.T) {
	for input, want := range map[string]string{"calendarEvent": "CalendarEvent", "already": "Already", "": "", "éclair": "Éclair"} {
		if got := ExportedName(input); got != want {
			t.Errorf("ExportedName(%q) = %q, want %q", input, got, want)
		}
	}
}

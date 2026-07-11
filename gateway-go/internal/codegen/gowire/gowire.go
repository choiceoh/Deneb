// Package gowire discovers Go structs that opt into generated client wire
// models and exposes the shared JSON-name rules used by every client generator.
package gowire

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ParseStructs returns every package-level struct in srcDir and its nested
// handler packages keyed by Go name, plus the sorted subset whose declaration
// carries marker. Test files are deliberately excluded because generated wire
// contracts come from production source only.
func ParseStructs(srcDir, marker string) (structs map[string]*ast.StructType, marked []string, err error) {
	fset := token.NewFileSet()
	type parsedFile struct {
		path string
		file *ast.File
	}
	var files []parsedFile
	err = filepath.WalkDir(srcDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		file, parseErr := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if parseErr != nil {
			return parseErr
		}
		files = append(files, parsedFile{path: path, file: file})
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	structs = map[string]*ast.StructType{}
	origins := map[string]string{}
	markedSet := map[string]bool{}
	for _, parsed := range files {
		for _, decl := range parsed.file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			declMarked := hasMarker(gen.Doc, marker)
			for _, spec := range gen.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				structure, ok := typeSpec.Type.(*ast.StructType)
				if !ok {
					continue
				}
				name := typeSpec.Name.Name
				if prior, exists := origins[name]; exists {
					return nil, nil, fmt.Errorf("duplicate wire struct name %q in %s and %s", name, prior, parsed.path)
				}
				structs[name] = structure
				origins[name] = parsed.path
				if declMarked || hasMarker(typeSpec.Doc, marker) {
					markedSet[name] = true
				}
			}
		}
	}

	for name := range markedSet {
		marked = append(marked, name)
	}
	sort.Strings(marked)
	return structs, marked, nil
}

func hasMarker(group *ast.CommentGroup, marker string) bool {
	if group == nil || marker == "" {
		return false
	}
	// CommentGroup.Text strips directive-style comments, so inspect the raw
	// comment list. This is exactly the shape of //deneb:wire.
	for _, comment := range group.List {
		if strings.Contains(comment.Text, marker) {
			return true
		}
	}
	return false
}

// JSONFieldName returns the serialized key for a named struct field and
// whether encoding/json excludes it.
func JSONFieldName(field *ast.Field) (name string, skip bool) {
	goName := field.Names[0].Name
	if field.Tag == nil {
		return goName, false
	}
	tag := reflect.StructTag(strings.Trim(field.Tag.Value, "`"))
	jsonTag := tag.Get("json")
	if jsonTag == "" {
		return goName, false
	}
	first := strings.Split(jsonTag, ",")[0]
	switch first {
	case "-":
		return "", true
	case "":
		return goName, false
	default:
		return first, false
	}
}

// ExportedName upper-cases the first rune so unexported Go wire names become
// idiomatic generated class/interface names.
func ExportedName(name string) string {
	if name == "" {
		return ""
	}
	first, size := utf8.DecodeRuneInString(name)
	return string(unicode.ToUpper(first)) + name[size:]
}

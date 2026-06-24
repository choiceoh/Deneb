// ts-models-gen generates TypeScript interfaces for the Andromeda desktop
// client from the Go miniapp wire structs — the same `//deneb:wire` source the
// Kotlin generator reads — so the desktop client and the gateway share a single
// source of truth for RPC response shapes and the two can't silently drift.
//
// A Go struct opts in by carrying a `//deneb:wire` directive in its doc comment
// (the very same marker kotlin-models-gen uses, so marking a struct emits BOTH
// the Kotlin data class and the TS interface). The generator parses the handler
// package's AST, emits one TS interface per opted-in struct, and transitively
// includes any struct types those structs reference.
//
// The gateway omits empty JSON fields, so every emitted property is optional.
//
// Usage (from gateway-go/):
//
//	go run cmd/ts-models-gen/main.go \
//	    -src internal/runtime/rpc/handler/handlerminiapp \
//	    -out ../../andromeda/src/gen/miniappWire.ts
//
// Add -check to compare against the committed file without writing (CI drift
// gate; mirrors kotlin-models-check). Andromeda drives this via `pnpm gen:wire`.
package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

// wireMarker is the doc-comment directive that opts a struct into generation —
// shared with kotlin-models-gen so one marker drives both clients.
const wireMarker = "deneb:wire"

func main() {
	var srcDir, outFile string
	var check bool
	for i := 1; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "-src":
			i++
			srcDir = arg(i)
		case "-out":
			i++
			outFile = arg(i)
		case "-check":
			check = true
		default:
			fail("unknown flag %q", os.Args[i])
		}
	}
	if srcDir == "" || outFile == "" {
		fail("usage: ts-models-gen -src DIR -out FILE [-check]")
	}

	structs, marked, err := parseStructs(srcDir)
	if err != nil {
		fail("parse %s: %v", srcDir, err)
	}
	if len(marked) == 0 {
		fail("no structs marked //%s in %s", wireMarker, srcDir)
	}

	ifaces, err := buildIfaces(structs, marked)
	if err != nil {
		fail("%v", err)
	}

	src := render(ifaces, srcDir)

	if check {
		existing, err := os.ReadFile(outFile)
		if err != nil {
			fail("read %s for check: %v (run `pnpm gen:wire`)", outFile, err)
		}
		if !bytes.Equal(existing, []byte(src)) {
			fail("%s is out of sync with the gateway wire structs — run `pnpm gen:wire` and commit", outFile)
		}
		fmt.Printf("ok: %s up to date (%d types)\n", outFile, len(ifaces))
		return
	}

	if err := os.MkdirAll(filepath.Dir(outFile), 0o755); err != nil {
		fail("mkdir: %v", err)
	}
	if err := os.WriteFile(outFile, []byte(src), 0o644); err != nil { //nolint:gosec // G306 — generated source, needs read access for the TS build
		fail("write %s: %v", outFile, err)
	}
	fmt.Printf("wrote %s (%d types)\n", outFile, len(ifaces))
}

// ---------------------------------------------------------------------------
// Parsing (identical to kotlin-models-gen — shared marker, shared discovery)
// ---------------------------------------------------------------------------

// parseStructs returns every package-level struct in srcDir keyed by Go name,
// plus the subset whose doc comment carries the //deneb:wire marker.
func parseStructs(srcDir string) (structs map[string]*ast.StructType, marked []string, err error) {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return nil, nil, err
	}

	fset := token.NewFileSet()
	var files []*ast.File
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, perr := parser.ParseFile(fset, filepath.Join(srcDir, name), nil, parser.ParseComments)
		if perr != nil {
			return nil, nil, perr
		}
		files = append(files, f)
	}

	structs = map[string]*ast.StructType{}
	markedSet := map[string]bool{}
	for _, file := range files {
		for _, decl := range file.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			declMarked := hasMarker(gd.Doc)
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					continue
				}
				structs[ts.Name.Name] = st
				if declMarked || hasMarker(ts.Doc) {
					markedSet[ts.Name.Name] = true
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

// hasMarker scans the raw comment lines for the directive. CommentGroup.Text()
// strips directive-style comments (//word:word), so we must scan .List directly.
func hasMarker(cg *ast.CommentGroup) bool {
	if cg == nil {
		return false
	}
	for _, c := range cg.List {
		if strings.Contains(c.Text, wireMarker) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Model building (Go struct -> TS interface)
// ---------------------------------------------------------------------------

type tsField struct {
	name string // TS property name (== JSON key)
	typ  string // TS type, e.g. "string", "CalendarAttendeeOut[]"
}

type tsIface struct {
	name   string
	fields []tsField
}

// buildIfaces resolves the marked roots and everything they reference
// (transitively) into TS interfaces, so marking the root struct is enough.
func buildIfaces(structs map[string]*ast.StructType, roots []string) ([]tsIface, error) {
	done := map[string]bool{}
	queue := append([]string(nil), roots...)
	var out []tsIface

	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if done[name] {
			continue
		}
		done[name] = true

		st := structs[name]
		if st == nil {
			return nil, fmt.Errorf("marked struct %q not found", name)
		}

		iface := tsIface{name: tsName(name)}
		for _, f := range st.Fields.List {
			if len(f.Names) != 1 {
				return nil, fmt.Errorf("%s: embedded or multi-name fields are unsupported", name)
			}
			jsonName, skip := jsonFieldName(f)
			if skip {
				continue
			}
			typ, refs, err := mapType(f.Type, structs)
			if err != nil {
				return nil, fmt.Errorf("%s.%s: %w", name, f.Names[0].Name, err)
			}
			iface.fields = append(iface.fields, tsField{name: jsonName, typ: typ})
			queue = append(queue, refs...)
		}
		out = append(out, iface)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out, nil
}

// mapType translates a Go field type into a TS type, and reports any package
// struct types it references (for transitivity). Mirrors the Kotlin generator's
// type table so both clients accept exactly the same JSON.
func mapType(expr ast.Expr, structs map[string]*ast.StructType) (typ string, refs []string, err error) {
	switch t := expr.(type) {
	case *ast.Ident:
		switch t.Name {
		case "string":
			return "string", nil, nil
		case "bool":
			return "boolean", nil, nil
		case "int", "int8", "int16", "int32", "int64",
			"uint", "uint8", "uint16", "uint32", "uint64",
			"float32", "float64":
			return "number", nil, nil
		}
		if _, ok := structs[t.Name]; ok {
			n := tsName(t.Name)
			return n, []string{t.Name}, nil
		}
		return "", nil, fmt.Errorf("unsupported type %q", t.Name)
	case *ast.StarExpr:
		// Pointer -> the inner type; the field is already optional (every property is).
		return mapType(t.X, structs)
	case *ast.ArrayType:
		// []byte marshals to a base64 string in Go's encoding/json.
		if id, ok := t.Elt.(*ast.Ident); ok && (id.Name == "byte" || id.Name == "uint8") {
			return "string", nil, nil
		}
		elem, refs, err := mapType(t.Elt, structs)
		if err != nil {
			return "", nil, err
		}
		return elem + "[]", refs, nil
	case *ast.SelectorExpr:
		// time.Time marshals to an RFC3339 string in Go's encoding/json.
		if pkg, ok := t.X.(*ast.Ident); ok && pkg.Name == "time" && t.Sel.Name == "Time" {
			return "string", nil, nil
		}
		return "", nil, fmt.Errorf("unsupported qualified type .%s", t.Sel.Name)
	default:
		return "", nil, fmt.Errorf("unsupported type expression %T", expr)
	}
}

// jsonFieldName returns the JSON key for a struct field (from its `json` tag,
// falling back to the Go field name) and whether to skip it (tag "-").
func jsonFieldName(f *ast.Field) (name string, skip bool) {
	goName := f.Names[0].Name
	if f.Tag == nil {
		return goName, false
	}
	tag := reflect.StructTag(strings.Trim(f.Tag.Value, "`"))
	jt := tag.Get("json")
	if jt == "" {
		return goName, false
	}
	first := strings.Split(jt, ",")[0]
	switch first {
	case "-":
		return "", true
	case "":
		return goName, false
	default:
		return first, false
	}
}

// tsName upper-cases the first letter so an unexported Go wire struct
// (e.g. calendarEventOut) becomes an idiomatic TS interface (CalendarEventOut),
// matching the Kotlin class names one-to-one.
func tsName(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// ---------------------------------------------------------------------------
// Rendering
// ---------------------------------------------------------------------------

func render(ifaces []tsIface, srcDir string) string {
	src := srcDir
	if !strings.HasPrefix(src, "gateway-go/") {
		src = "gateway-go/" + src
	}

	var b strings.Builder
	fmt.Fprintf(&b, "// Code generated by ts-models-gen. DO NOT EDIT.\n")
	fmt.Fprintf(&b, "// Source: %s (structs marked //%s)\n", src, wireMarker)
	fmt.Fprintf(&b, "// Regenerate: pnpm gen:wire\n")
	fmt.Fprintf(&b, "//\n")
	fmt.Fprintf(&b, "// The gateway omits empty JSON fields, so every property is optional.\n\n")

	for i, iface := range ifaces {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "export interface %s {\n", iface.name)
		for _, f := range iface.fields {
			fmt.Fprintf(&b, "  %s?: %s\n", f.name, f.typ)
		}
		fmt.Fprintf(&b, "}\n")
	}

	return b.String()
}

// ---------------------------------------------------------------------------
// Small helpers
// ---------------------------------------------------------------------------

func arg(i int) string {
	if i >= len(os.Args) {
		fail("missing value for %s", os.Args[i-1])
	}
	return os.Args[i]
}

func fail(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "ts-models-gen: "+format+"\n", a...)
	os.Exit(1)
}

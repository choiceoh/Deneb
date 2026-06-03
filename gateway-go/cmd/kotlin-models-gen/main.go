// kotlin-models-gen generates Kotlin @Serializable data classes for the
// native client from the Go miniapp wire structs, so the client and the
// gateway share a single source of truth for RPC response shapes.
//
// A Go struct opts in by carrying a `//deneb:wire` directive in its doc
// comment. The generator parses the handler package's AST, emits one
// Kotlin data class per opted-in struct, and transitively includes any
// struct types those structs reference (so marking the root is enough).
//
// Usage (from gateway-go/):
//
//	go run cmd/kotlin-models-gen/main.go \
//	    -src internal/runtime/rpc/handler/handlerminiapp \
//	    -out ../client-android/app/composeApp/src/commonMain/kotlin/com/inspiredandroid/kai/deneb/generated/MiniappWireTypes.kt \
//	    -pkg com.inspiredandroid.kai.deneb.generated
//
// Add -check to compare against the committed file without writing (CI
// drift gate; mirrors tool-schemas-check). Or via Makefile: make kotlin-models.
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

// wireMarker is the doc-comment directive that opts a struct into Kotlin
// generation. Placed on its own line in the struct's doc comment.
const wireMarker = "deneb:wire"

func main() {
	var srcDir, outFile, pkg string
	var check bool
	for i := 1; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "-src":
			i++
			srcDir = arg(i)
		case "-out":
			i++
			outFile = arg(i)
		case "-pkg":
			i++
			pkg = arg(i)
		case "-check":
			check = true
		default:
			fail("unknown flag %q", os.Args[i])
		}
	}
	if srcDir == "" || outFile == "" || pkg == "" {
		fail("usage: kotlin-models-gen -src DIR -out FILE -pkg KOTLIN_PKG [-check]")
	}

	structs, marked, err := parseStructs(srcDir)
	if err != nil {
		fail("parse %s: %v", srcDir, err)
	}
	if len(marked) == 0 {
		fail("no structs marked //%s in %s", wireMarker, srcDir)
	}

	classes, err := buildClasses(structs, marked)
	if err != nil {
		fail("%v", err)
	}

	src := render(classes, pkg, srcDir)

	if check {
		existing, err := os.ReadFile(outFile)
		if err != nil {
			fail("read %s for check: %v (run `make kotlin-models`)", outFile, err)
		}
		if !bytes.Equal(existing, []byte(src)) {
			fail("%s is out of sync with Go wire structs — run `make kotlin-models` and commit", outFile)
		}
		fmt.Printf("ok: %s up to date (%d types)\n", outFile, len(classes))
		return
	}

	if err := os.MkdirAll(filepath.Dir(outFile), 0o755); err != nil {
		fail("mkdir: %v", err)
	}
	if err := os.WriteFile(outFile, []byte(src), 0o644); err != nil { //nolint:gosec // G306 — generated source, needs read access for the Kotlin build
		fail("write %s: %v", outFile, err)
	}
	fmt.Printf("wrote %s (%d types)\n", outFile, len(classes))
}

// ---------------------------------------------------------------------------
// Parsing
// ---------------------------------------------------------------------------

// parseStructs returns every package-level struct in srcDir keyed by Go
// name, plus the subset whose doc comment carries the //deneb:wire marker.
func parseStructs(srcDir string) (structs map[string]*ast.StructType, marked []string, err error) {
	// Parse each non-test .go file directly. We avoid parser.ParseDir (deprecated
	// in Go 1.25 and build-tag-unaware); a plain ReadDir + ParseFile is all we need
	// since the handler package has no build-tagged files.
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

// hasMarker scans the raw comment lines for the directive. Note: we must
// NOT use CommentGroup.Text(), which strips directive-style comments
// (anything matching //word:word) — exactly the shape of our marker.
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
// Model building (Go struct -> Kotlin class)
// ---------------------------------------------------------------------------

type kotField struct {
	name string // Kotlin property name (== JSON key)
	typ  string // Kotlin type, e.g. "String", "List<CalendarAttendeeOut>", "CalendarConferenceOut?"
	def  string // default expression, e.g. `""`, `emptyList()`, `null`
}

type kotClass struct {
	name   string
	fields []kotField
}

// buildClasses resolves the marked roots and everything they reference
// (transitively) into Kotlin classes. Marking the root struct is enough;
// referenced wire structs are pulled in automatically so no field can
// silently drop out of the shared contract.
func buildClasses(structs map[string]*ast.StructType, roots []string) ([]kotClass, error) {
	done := map[string]bool{}
	queue := append([]string(nil), roots...)
	var out []kotClass

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

		cls := kotClass{name: kotName(name)}
		for _, f := range st.Fields.List {
			if len(f.Names) != 1 {
				return nil, fmt.Errorf("%s: embedded or multi-name fields are unsupported", name)
			}
			jsonName, skip := jsonFieldName(f)
			if skip {
				continue
			}
			typ, def, refs, err := mapType(f.Type, structs)
			if err != nil {
				return nil, fmt.Errorf("%s.%s: %w", name, f.Names[0].Name, err)
			}
			cls.fields = append(cls.fields, kotField{name: jsonName, typ: typ, def: def})
			queue = append(queue, refs...)
		}
		out = append(out, cls)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out, nil
}

// mapType translates a Go field type into a Kotlin type + default value,
// and reports any package struct types it references (for transitivity).
// Unsupported types (maps, time.Time, interfaces, ...) return an error so
// the marker only ever lands on cleanly-translatable structs.
func mapType(expr ast.Expr, structs map[string]*ast.StructType) (typ, def string, refs []string, err error) {
	switch t := expr.(type) {
	case *ast.Ident:
		switch t.Name {
		case "string":
			return "String", `""`, nil, nil
		case "bool":
			return "Boolean", "false", nil, nil
		case "int", "int8", "int16", "int32", "uint", "uint8", "uint16", "uint32":
			return "Int", "0", nil, nil
		case "int64", "uint64":
			return "Long", "0L", nil, nil
		case "float32", "float64":
			return "Double", "0.0", nil, nil
		}
		if _, ok := structs[t.Name]; ok {
			cls := kotName(t.Name)
			return cls, cls + "()", []string{t.Name}, nil
		}
		return "", "", nil, fmt.Errorf("unsupported type %q", t.Name)
	case *ast.StarExpr:
		// Pointer -> nullable. The inner default is irrelevant (defaults null).
		inner, _, refs, err := mapType(t.X, structs)
		if err != nil {
			return "", "", nil, err
		}
		return inner + "?", "null", refs, nil
	case *ast.ArrayType:
		// []byte marshals to a base64 string in Go's encoding/json.
		if id, ok := t.Elt.(*ast.Ident); ok && (id.Name == "byte" || id.Name == "uint8") {
			return "String", `""`, nil, nil
		}
		elem, _, refs, err := mapType(t.Elt, structs)
		if err != nil {
			return "", "", nil, err
		}
		return "List<" + elem + ">", "emptyList()", refs, nil
	default:
		return "", "", nil, fmt.Errorf("unsupported type expression %T", expr)
	}
}

// jsonFieldName returns the JSON key for a struct field (from its `json`
// tag, falling back to the Go field name) and whether to skip it (tag "-").
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

// kotName upper-cases the first letter so an unexported Go wire struct
// (e.g. calendarEventOut) becomes an idiomatic Kotlin class (CalendarEventOut).
func kotName(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// ---------------------------------------------------------------------------
// Rendering
// ---------------------------------------------------------------------------

func render(classes []kotClass, pkg, srcDir string) string {
	src := srcDir
	if !strings.HasPrefix(src, "gateway-go/") {
		src = "gateway-go/" + src
	}

	var b strings.Builder
	fmt.Fprintf(&b, "// Code generated by kotlin-models-gen. DO NOT EDIT.\n")
	fmt.Fprintf(&b, "// Source: %s (structs marked //%s)\n", src, wireMarker)
	fmt.Fprintf(&b, "// Regenerate: make kotlin-models\n\n")
	fmt.Fprintf(&b, "package %s\n\n", pkg)
	fmt.Fprintf(&b, "import kotlinx.serialization.Serializable\n\n")

	for i, cls := range classes {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "@Serializable\n")
		fmt.Fprintf(&b, "data class %s(\n", cls.name)
		for _, f := range cls.fields {
			fmt.Fprintf(&b, "    val %s: %s = %s,\n", f.name, f.typ, f.def)
		}
		fmt.Fprintf(&b, ")\n")
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
	fmt.Fprintf(os.Stderr, "kotlin-models-gen: "+format+"\n", a...)
	os.Exit(1)
}

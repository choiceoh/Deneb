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
//	    -out ../client-android/app/composeApp/src/commonMain/kotlin/ai/deneb/deneb/generated/MiniappWireTypes.kt \
//	    -pkg ai.deneb.deneb.generated
//
// Add -check to compare against the committed file without writing (CI
// drift gate; mirrors tool-schemas-check). Or via Makefile: make kotlin-models.
package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/codegen/gowire"
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

	structs, marked, err := gowire.ParseStructs(srcDir, wireMarker)
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

		cls := kotClass{name: gowire.ExportedName(name)}
		for _, f := range st.Fields.List {
			if len(f.Names) != 1 {
				return nil, fmt.Errorf("%s: embedded or multi-name fields are unsupported", name)
			}
			jsonName, skip := gowire.JSONFieldName(f)
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
			cls := gowire.ExportedName(t.Name)
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
	case *ast.SelectorExpr:
		// time.Time marshals to an RFC3339 string in Go's encoding/json, which
		// the client already decodes as a String. Other qualified types (from
		// imported packages) stay unsupported so markers only land on clean structs.
		if pkg, ok := t.X.(*ast.Ident); ok && pkg.Name == "time" && t.Sel.Name == "Time" {
			return "String", `""`, nil, nil
		}
		return "", "", nil, fmt.Errorf("unsupported qualified type .%s", t.Sel.Name)
	default:
		return "", "", nil, fmt.Errorf("unsupported type expression %T", expr)
	}
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
	// @Immutable: these are decode-once, val-only DTOs that are never mutated, so
	// Compose can treat them as stable and skip recomposition when an equal value
	// is re-emitted — the same promise the hand-written DenebDomainTypes carry.
	fmt.Fprintf(&b, "import androidx.compose.runtime.Immutable\n")
	fmt.Fprintf(&b, "import kotlinx.serialization.Serializable\n\n")

	for i, cls := range classes {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "@Immutable\n")
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

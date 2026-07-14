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
//	    -test-out ../client-android/app/composeApp/src/commonTest/kotlin/ai/deneb/deneb/generated/MiniappWireDescriptorContractTest.kt \
//	    -field-test-out ../client-android/app/composeApp/src/commonTest/kotlin/ai/deneb/deneb/generated/MiniappWireFieldBoundaryContractTest.kt \
//	    -null-test-out ../client-android/app/composeApp/src/commonTest/kotlin/ai/deneb/deneb/generated/MiniappWireNullCompatibilityTest.kt \
//	    -value-test-out ../client-android/app/composeApp/src/commonTest/kotlin/ai/deneb/deneb/generated/MiniappWireValueContractTest.kt \
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

// genFlags holds the parsed command-line flags.
type genFlags struct {
	srcDir           string
	outFile          string
	testOutFile      string
	fieldTestOutFile string
	nullTestOutFile  string
	valueTestOutFile string
	pkg              string
	check            bool
}

func parseFlags() genFlags {
	var f genFlags
	stringFlags := map[string]*string{
		"-src":            &f.srcDir,
		"-out":            &f.outFile,
		"-test-out":       &f.testOutFile,
		"-field-test-out": &f.fieldTestOutFile,
		"-null-test-out":  &f.nullTestOutFile,
		"-value-test-out": &f.valueTestOutFile,
		"-pkg":            &f.pkg,
	}
	for i := 1; i < len(os.Args); i++ {
		if os.Args[i] == "-check" {
			f.check = true
			continue
		}
		target, ok := stringFlags[os.Args[i]]
		if !ok {
			fail("unknown flag %q", os.Args[i])
		}
		i++
		*target = arg(i)
	}
	if f.srcDir == "" || f.outFile == "" || f.pkg == "" {
		fail("usage: kotlin-models-gen -src DIR -out FILE [-test-out FILE] [-field-test-out FILE] [-null-test-out FILE] [-value-test-out FILE] -pkg KOTLIN_PKG [-check]")
	}
	return f
}

// genOutput pairs a target path with its rendered content.
type genOutput struct {
	path    string
	content string
}

// buildOutputs renders the model file plus each requested test file, in the
// original emission order (out, test-out, field-test-out, null-test-out,
// value-test-out). Renderers are unchanged, so output stays byte-identical.
func buildOutputs(f genFlags, classes []kotClass) []genOutput {
	outputs := []genOutput{{f.outFile, render(classes, f.pkg, f.srcDir)}}
	if f.testOutFile != "" {
		outputs = append(outputs, genOutput{f.testOutFile, renderContractTests(classes, f.pkg, f.srcDir)})
	}
	if f.fieldTestOutFile != "" {
		outputs = append(outputs, genOutput{f.fieldTestOutFile, renderFieldContractTests(classes, f.pkg, f.srcDir)})
	}
	if f.nullTestOutFile != "" {
		outputs = append(outputs, genOutput{f.nullTestOutFile, renderNullContractTests(classes, f.pkg, f.srcDir)})
	}
	if f.valueTestOutFile != "" {
		outputs = append(outputs, genOutput{f.valueTestOutFile, renderValueContractTests(classes, f.pkg, f.srcDir)})
	}
	return outputs
}

func main() {
	f := parseFlags()

	structs, marked, err := gowire.ParseStructs(f.srcDir, wireMarker)
	if err != nil {
		fail("parse %s: %v", f.srcDir, err)
	}
	if len(marked) == 0 {
		fail("no structs marked //%s in %s", wireMarker, f.srcDir)
	}

	classes, err := buildClasses(structs, marked)
	if err != nil {
		fail("%v", err)
	}

	outputs := buildOutputs(f, classes)

	if f.check {
		for _, out := range outputs {
			checkGenerated(out.path, out.content)
		}
		fmt.Printf("ok: %s up to date (%d types)\n", f.outFile, len(classes))
		return
	}

	for _, out := range outputs {
		writeGenerated(out.path, out.content)
	}
	fmt.Printf("wrote %s (%d types)\n", f.outFile, len(classes))
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
	fmt.Fprintf(&b, "// Code generated by gateway-go/cmd/kotlin-models-gen/main.go; DO NOT EDIT.\n")
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

// renderContractTests emits one data-driven compatibility contract for every
// generated wire type. Keeping this inventory beside the model generator makes
// adding a Go //deneb:wire struct update both production DTOs and their native
// compatibility proof in the same regeneration step.
func renderContractTests(classes []kotClass, pkg, srcDir string) string {
	src := srcDir
	if !strings.HasPrefix(src, "gateway-go/") {
		src = "gateway-go/" + src
	}

	var b strings.Builder
	fmt.Fprintf(&b, "// Code generated by gateway-go/cmd/kotlin-models-gen/main.go; DO NOT EDIT.\n")
	fmt.Fprintf(&b, "// Source: %s (structs marked //%s)\n", src, wireMarker)
	fmt.Fprintf(&b, "// Regenerate: make kotlin-models\n\n")
	fmt.Fprintf(&b, "package %s\n\n", pkg)
	b.WriteString(`import kotlinx.serialization.ExperimentalSerializationApi
import kotlinx.serialization.KSerializer
import kotlinx.serialization.descriptors.SerialDescriptor
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.decodeFromJsonElement
import kotlinx.serialization.json.encodeToJsonElement
import kotlinx.serialization.json.jsonObject
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertTrue

/**
 * Exhaustive compatibility guards for generated Kotlin wire models.
 *
 * Every case comes from the same Go //deneb:wire inventory as MiniappWireTypes.
 * The shared verifier keeps the contract readable without cloning the same test
 * body for every DTO.
 */
@OptIn(ExperimentalSerializationApi::class)
class MiniappWireDescriptorContractTest {
    private val json = Json {
        ignoreUnknownKeys = true
        isLenient = true
        coerceInputValues = true
    }

    private data class WireContract(
        val name: String,
        val verify: () -> Unit,
    )

    private fun SerialDescriptor.elementNames(): List<String> = (0 until elementsCount).map(::getElementName)

    private fun <T> contract(
        name: String,
        serializer: KSerializer<T>,
        empty: T,
        fields: List<String>,
    ): WireContract = WireContract(name) {
        verifyContract(name, serializer, empty, fields)
    }

    private val contracts = listOf(
`)
	for _, cls := range classes {
		fmt.Fprintf(&b, "        contract(\n")
		fmt.Fprintf(&b, "            name = %q,\n", cls.name)
		fmt.Fprintf(&b, "            serializer = %s.serializer(),\n", cls.name)
		fmt.Fprintf(&b, "            empty = %s(),\n", cls.name)
		b.WriteString("            fields = listOf(")
		for i, field := range cls.fields {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "%q", field.name)
		}
		b.WriteString("),\n        ),\n")
	}
	b.WriteString(`    )

    @Test
    fun generatedWireModelsKeepBackwardCompatibleDefaults() {
        contracts.forEach { contract -> contract.verify() }
    }

    private fun <T> verifyContract(
        name: String,
        serializer: KSerializer<T>,
        empty: T,
        expectedFields: List<String>,
    ) {
        val descriptor = serializer.descriptor
        assertEquals(name, descriptor.serialName.substringAfterLast('.'), "$name serial name")
        assertEquals(expectedFields, descriptor.elementNames(), "$name fields")
        assertTrue(
            (0 until descriptor.elementsCount).all(descriptor::isElementOptional),
            "$name fields must remain optional",
        )

        val decodedEmpty = json.decodeFromString(serializer, "{}")
        assertEquals(empty, decodedEmpty, "$name empty payload")

        val withFutureFields = json.decodeFromString(
            serializer,
            """{"futureField":{"nested":[1,true,null]},"futureFlag":true}""",
        )
        assertEquals(empty, withFutureFields, "$name unknown-field tolerance")

        val encoded = json.encodeToJsonElement(serializer, empty)
        assertTrue(encoded is JsonObject, "$name encoded shape")
        assertTrue(encoded.jsonObject.isEmpty(), "$name defaults must stay omitted")
        assertEquals(empty, json.decodeFromJsonElement(serializer, encoded), "$name round trip")
    }
}
`)
	return b.String()
}

// renderFieldContractTests proves each generated property in isolation. The
// model type determines a boundary value, an incompatible wire shape, and the
// observable round-trip contract; all cases share one verifier so this remains
// exhaustive without thousands of hand-copied test bodies.
func renderFieldContractTests(classes []kotClass, pkg, srcDir string) string {
	src := srcDir
	if !strings.HasPrefix(src, "gateway-go/") {
		src = "gateway-go/" + src
	}

	var b strings.Builder
	fmt.Fprintf(&b, "// Code generated by gateway-go/cmd/kotlin-models-gen/main.go; DO NOT EDIT.\n")
	fmt.Fprintf(&b, "// Source: %s (structs marked //%s)\n", src, wireMarker)
	fmt.Fprintf(&b, "// Regenerate: make kotlin-models\n\n")
	fmt.Fprintf(&b, "package %s\n\n", pkg)
	b.WriteString(`import kotlinx.serialization.KSerializer
import kotlinx.serialization.SerializationException
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonArray
import kotlinx.serialization.json.decodeFromJsonElement
import kotlinx.serialization.json.encodeToJsonElement
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertIs

/**
 * Field-isolated boundary contracts generated from every mini-app wire DTO.
 *
 * The generator selects type-appropriate boundary and wrong-shape values. New
 * fields therefore gain compatibility coverage in the same change that emits
 * their production model.
 */
class MiniappWireFieldBoundaryContractTest {
    private val json = Json {
        ignoreUnknownKeys = true
        coerceInputValues = true
        encodeDefaults = true
    }

    private enum class Expectation {
        Exact,
        Object,
        ObjectList,
    }

    private data class FieldContract(
        val name: String,
        val verify: () -> Unit,
    )

    private val boundaryText = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
    private val stringList = buildJsonArray {
        add(boundaryText)
        add(JsonPrimitive(""))
        add(boundaryText)
        add(JsonPrimitive("끝\n값"))
    }
    private val objectList = buildJsonArray {
        repeat(3) { add(JsonObject(emptyMap())) }
    }

    private fun <T> fieldContract(
        name: String,
        serializer: KSerializer<T>,
        field: String,
        valid: JsonElement,
        invalid: JsonElement,
        expectation: Expectation,
    ): FieldContract = FieldContract(name) {
        verifyField(name, serializer, field, valid, invalid, expectation)
    }

    private val contracts = listOf(
`)
	for _, cls := range classes {
		for _, field := range cls.fields {
			value := contractValueFor(field.typ)
			fmt.Fprintf(&b, "        fieldContract(\n")
			fmt.Fprintf(&b, "            name = %q,\n", cls.name+"."+field.name)
			fmt.Fprintf(&b, "            serializer = %s.serializer(),\n", cls.name)
			fmt.Fprintf(&b, "            field = %q,\n", field.name)
			fmt.Fprintf(&b, "            valid = %s,\n", value.valid)
			fmt.Fprintf(&b, "            invalid = %s,\n", value.invalid)
			fmt.Fprintf(&b, "            expectation = Expectation.%s,\n", value.expectation)
			b.WriteString("        ),\n")
		}
	}
	b.WriteString(`    )

    @Test
    fun everyGeneratedFieldPreservesItsBoundaryAndRejectsWrongShape() {
        contracts.forEach { contract -> contract.verify() }
    }

    private fun <T> verifyField(
        name: String,
        serializer: KSerializer<T>,
        field: String,
        valid: JsonElement,
        invalid: JsonElement,
        expectation: Expectation,
    ) {
        val decoded = json.decodeFromJsonElement(serializer, JsonObject(mapOf(field to valid)))
        val encoded = json.encodeToJsonElement(serializer, decoded).jsonObject.getValue(field)
        when (expectation) {
            Expectation.Exact -> assertEquals(valid, encoded, "$name boundary value")

            Expectation.Object -> assertIs<JsonObject>(encoded, "$name object shape")

            Expectation.ObjectList -> {
                val values = encoded.jsonArray
                assertEquals(3, values.size, "$name collection cardinality")
                values.forEach { assertIs<JsonObject>(it, "$name nested object shape") }
            }
        }
        assertFailsWith<SerializationException>("$name must reject an incompatible wire shape") {
            json.decodeFromJsonElement(serializer, JsonObject(mapOf(field to invalid)))
        }
    }
}
`)
	return b.String()
}

type contractValue struct {
	valid       string
	invalid     string
	expectation string
}

func contractValueFor(kotlinType string) contractValue {
	typ := strings.TrimSuffix(kotlinType, "?")
	switch typ {
	case "String":
		return contractValue{"boundaryText", "JsonObject(emptyMap())", "Exact"}
	case "Boolean":
		return contractValue{"JsonPrimitive(true)", "JsonPrimitive(1)", "Exact"}
	case "Int":
		return contractValue{"JsonPrimitive(Int.MAX_VALUE)", `JsonPrimitive("not-an-int")`, "Exact"}
	case "Long":
		return contractValue{"JsonPrimitive(Long.MAX_VALUE)", `JsonPrimitive("not-a-long")`, "Exact"}
	case "Double":
		return contractValue{"JsonPrimitive(-12345.6789)", `JsonPrimitive("not-a-double")`, "Exact"}
	case "List<String>":
		return contractValue{"stringList", `JsonObject(mapOf("not" to JsonPrimitive("a-list")))`, "Exact"}
	}
	if strings.HasPrefix(typ, "List<") {
		return contractValue{"objectList", `JsonObject(mapOf("not" to JsonPrimitive("a-list")))`, "ObjectList"}
	}
	return contractValue{"JsonObject(emptyMap())", `JsonPrimitive("not-an-object")`, "Object"}
}

// renderValueContractTests exercises every field of each generated DTO in one
// non-default payload. It complements the isolated field contracts by proving
// that realistic, fully populated objects preserve all values together and
// remain round-trip stable. The first field also retains the legacy malformed
// payload guard for every DTO.
func renderValueContractTests(classes []kotClass, pkg, srcDir string) string {
	src := srcDir
	if !strings.HasPrefix(src, "gateway-go/") {
		src = "gateway-go/" + src
	}

	var b strings.Builder
	fmt.Fprintf(&b, "// Code generated by gateway-go/cmd/kotlin-models-gen/main.go; DO NOT EDIT.\n")
	fmt.Fprintf(&b, "// Source: %s (structs marked //%s)\n", src, wireMarker)
	fmt.Fprintf(&b, "// Regenerate: make kotlin-models\n\n")
	fmt.Fprintf(&b, "package %s\n\n", pkg)
	b.WriteString(`import kotlinx.serialization.KSerializer
import kotlinx.serialization.SerializationException
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.buildJsonArray
import kotlinx.serialization.json.decodeFromJsonElement
import kotlinx.serialization.json.encodeToJsonElement
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertIs

/**
 * Fully populated value contracts generated from every mini-app wire DTO.
 *
 * Each DTO is decoded with all fields present at once, encoded again, checked
 * by field shape, and decoded a second time. This protects interactions between
 * fields while keeping the wire inventory owned by the Go source of truth.
 */
class MiniappWireValueContractTest {
    private val json = Json {
        ignoreUnknownKeys = true
        isLenient = true
        coerceInputValues = true
        encodeDefaults = true
    }

    private enum class Expectation {
        Exact,
        Object,
        ObjectList,
    }

    private data class FieldValue(
        val name: String,
        val value: JsonElement,
        val expectation: Expectation,
    )

    private data class WireContract(
        val name: String,
        val verify: () -> Unit,
    )

    private val boundaryText = JsonPrimitive("  \u0000 한글 café\n\t\"\\ /?#  ")
    private val stringList = buildJsonArray {
        add(boundaryText)
        add(JsonPrimitive(""))
        add(boundaryText)
        add(JsonPrimitive("끝\n값"))
    }
    private val objectList = buildJsonArray {
        repeat(3) { add(JsonObject(emptyMap())) }
    }

    private fun fieldValue(
        name: String,
        value: JsonElement,
        expectation: Expectation,
    ) = FieldValue(name, value, expectation)

    private fun <T> wireContract(
        name: String,
        serializer: KSerializer<T>,
        fields: List<FieldValue>,
        invalidField: String?,
        invalidValue: JsonElement?,
    ): WireContract = WireContract(name) {
        verifyContract(name, serializer, fields, invalidField, invalidValue)
    }

    private val contracts = listOf(
`)
	for _, cls := range classes {
		fmt.Fprintf(&b, "        wireContract(\n")
		fmt.Fprintf(&b, "            name = %q,\n", cls.name)
		fmt.Fprintf(&b, "            serializer = %s.serializer(),\n", cls.name)
		if len(cls.fields) == 0 {
			b.WriteString("            fields = emptyList(),\n")
			b.WriteString("            invalidField = null,\n")
			b.WriteString("            invalidValue = null,\n")
		} else {
			b.WriteString("            fields = listOf(\n")
			for _, field := range cls.fields {
				value := contractValueFor(field.typ)
				b.WriteString("                fieldValue(\n")
				fmt.Fprintf(&b, "                    name = %q,\n", field.name)
				fmt.Fprintf(&b, "                    value = %s,\n", value.valid)
				fmt.Fprintf(&b, "                    expectation = Expectation.%s,\n", value.expectation)
				b.WriteString("                ),\n")
			}
			b.WriteString("            ),\n")
			first := contractValueFor(cls.fields[0].typ)
			fmt.Fprintf(&b, "            invalidField = %q,\n", cls.fields[0].name)
			fmt.Fprintf(&b, "            invalidValue = %s,\n", first.invalid)
		}
		b.WriteString("        ),\n")
	}
	b.WriteString(`    )

    @Test
    fun everyGeneratedModelPreservesAllPresentValuesAndRejectsWrongShape() {
        contracts.forEach { contract -> contract.verify() }
    }

    private fun <T> verifyContract(
        name: String,
        serializer: KSerializer<T>,
        fields: List<FieldValue>,
        invalidField: String?,
        invalidValue: JsonElement?,
    ) {
        val input = JsonObject(fields.associate { field -> field.name to field.value })
        val decoded = json.decodeFromJsonElement(serializer, input)
        val encoded = json.encodeToJsonElement(serializer, decoded).jsonObject

        assertEquals(input.keys, encoded.keys, "$name field inventory")
        fields.forEach { field ->
            val actual = encoded.getValue(field.name)
            when (field.expectation) {
                Expectation.Exact -> assertEquals(field.value, actual, "$name.${field.name} value")

                Expectation.Object -> assertIs<JsonObject>(actual, "$name.${field.name} object shape")

                Expectation.ObjectList -> {
                    val values = actual.jsonArray
                    assertEquals(3, values.size, "$name.${field.name} collection cardinality")
                    values.forEach { assertIs<JsonObject>(it, "$name.${field.name} nested object shape") }
                }
            }
        }
        assertEquals(decoded, json.decodeFromJsonElement(serializer, encoded), "$name round trip")

        if (invalidField != null && invalidValue != null) {
            assertFailsWith<SerializationException>("$name must reject an incompatible wire shape") {
                json.decodeFromJsonElement(
                    serializer,
                    JsonObject(mapOf(invalidField to invalidValue)),
                )
            }
        }
    }
}
`)
	return b.String()
}

// renderNullContractTests verifies the rolling-upgrade contract that an
// explicit JSON null is coerced to each generated field's declared default.
// The exhaustive field list is derived from the same classes as production.
func renderNullContractTests(classes []kotClass, pkg, srcDir string) string {
	src := srcDir
	if !strings.HasPrefix(src, "gateway-go/") {
		src = "gateway-go/" + src
	}

	var b strings.Builder
	fmt.Fprintf(&b, "// Code generated by gateway-go/cmd/kotlin-models-gen/main.go; DO NOT EDIT.\n")
	fmt.Fprintf(&b, "// Source: %s (structs marked //%s)\n", src, wireMarker)
	fmt.Fprintf(&b, "// Regenerate: make kotlin-models\n\n")
	fmt.Fprintf(&b, "package %s\n\n", pkg)
	b.WriteString(`import kotlinx.serialization.KSerializer
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.decodeFromJsonElement
import kotlin.test.Test
import kotlin.test.assertEquals

/**
 * Explicit-null compatibility for every generated Go/Kotlin wire field.
 *
 * Go may emit nil slices and optional structs as JSON null. Each case is
 * generated with its model, so rolling client/gateway upgrades retain the
 * declared Kotlin default without a hand-maintained parallel inventory.
 */
class MiniappWireNullCompatibilityTest {
    private val json = Json {
        ignoreUnknownKeys = true
        isLenient = true
        coerceInputValues = true
    }

    private data class NullContract(
        val name: String,
        val verify: () -> Unit,
    )

    private fun <T> nullContract(
        name: String,
        serializer: KSerializer<T>,
        empty: T,
        field: String,
    ): NullContract = NullContract(name) {
        val decoded = json.decodeFromJsonElement(
            serializer,
            JsonObject(mapOf(field to JsonNull)),
        )
        assertEquals(empty, decoded, "$name explicit null")
    }

    private val contracts = listOf(
`)
	for _, cls := range classes {
		for _, field := range cls.fields {
			fmt.Fprintf(&b, "        nullContract(\n")
			fmt.Fprintf(&b, "            name = %q,\n", cls.name+"."+field.name)
			fmt.Fprintf(&b, "            serializer = %s.serializer(),\n", cls.name)
			fmt.Fprintf(&b, "            empty = %s(),\n", cls.name)
			fmt.Fprintf(&b, "            field = %q,\n", field.name)
			b.WriteString("        ),\n")
		}
	}
	b.WriteString(`    )

    @Test
    fun everyGeneratedFieldCoercesExplicitNullToItsDeclaredDefault() {
        contracts.forEach { contract -> contract.verify() }
    }
}
`)
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

func checkGenerated(path, want string) {
	existing, err := os.ReadFile(path)
	if err != nil {
		fail("read %s for check: %v (run `make kotlin-models`)", path, err)
	}
	if !bytes.Equal(existing, []byte(want)) {
		fail("%s is out of sync with Go wire structs — run `make kotlin-models` and commit", path)
	}
}

func writeGenerated(path, content string) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fail("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil { //nolint:gosec // G306 — generated source, needs read access for the Kotlin build
		fail("write %s: %v", path, err)
	}
}

func fail(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "kotlin-models-gen: "+format+"\n", a...)
	os.Exit(1)
}

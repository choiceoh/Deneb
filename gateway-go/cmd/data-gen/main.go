// data-gen generates Go variable declarations from JSON data files.
//
// Universal code generator for data-driven Go code. Reads a JSON file
// containing variable definitions with type metadata and generates
// corresponding Go source code.
//
// Usage (from gateway-go/):
//
//	go run cmd/data-gen/main.go \
//	    -json internal/pipeline/chat/tool_classification.json \
//	    -out  internal/pipeline/chat/tool_classification_gen.go
//
// Or via Makefile: make data-gen
//
// JSON format:
//
//	{
//	  "_gen": {
//	    "package": "<go-package-name>",
//	    "imports": ["<import-path>", ...],
//	    "build_tags": "<tags>"
//	  },
//	  "<varName>": {
//	    "_type": "<go-type>",
//	    "_doc": "<comment>",
//	    "_value_map": { "short": "GoExpr" },
//	    "_values": { ... }
//	  }
//	}
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

func main() {
	os.Exit(runDataGen(os.Args[1:], os.Stdout, os.Stderr))
}

const dataGenUsage = "usage: data-gen -json FILE -out FILE"

func runDataGen(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("data-gen", flag.ContinueOnError)
	flags.SetOutput(stderr)
	jsonFile := flags.String("json", "", "JSON source file")
	outFile := flags.String("out", "", "Output Go file")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *jsonFile == "" || *outFile == "" {
		fmt.Fprintln(stderr, dataGenUsage)
		return 1
	}

	plan, err := loadGenerationPlan(*jsonFile)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	if err := writeGeneratedOutput(*outFile, generate(plan), stderr); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "wrote %s (%d vars, %d entries)\n", *outFile, len(plan.variables), plan.entryCount)
	return 0
}

func loadGenerationPlan(jsonFile string) (generationPlan, error) {
	data, err := os.ReadFile(jsonFile)
	if err != nil {
		return generationPlan{}, err
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return generationPlan{}, err
	}
	return prepareGeneration(doc, sourceCommentPath(jsonFile)), nil
}

func sourceCommentPath(jsonFile string) string {
	sourcePath := jsonFile
	if abs, err := filepath.Abs(jsonFile); err == nil {
		if idx := strings.Index(abs, "/gateway-go/"); idx >= 0 {
			sourcePath = abs[idx+1:]
		}
	}
	return sourcePath
}

// writeGeneratedOutput owns the format-and-write boundary. Formatting remains
// best effort for compatibility: invalid Go is written verbatim after a
// warning so generator authors can inspect the faulty source.
func writeGeneratedOutput(outFile, source string, stderr io.Writer) error {
	formatted, err := format.Source([]byte(source))
	if err != nil {
		fmt.Fprintf(stderr, "gofmt warning: %v\n", err)
		formatted = []byte(source)
	}
	return os.WriteFile(outFile, formatted, 0o600) //nolint:gosec // G306 — generated source file, needs read access
}

// ---------------------------------------------------------------------------
// Input preparation
// ---------------------------------------------------------------------------

type generationPlan struct {
	sourcePath string
	config     generatorConfig
	variables  []variableDefinition
	entryCount int
}

type generatorConfig struct {
	packageName string
	imports     []string
	buildTags   string
}

type variableDefinition struct {
	name     string
	typeName string
	doc      string
	valueMap map[string]string
	values   any
}

func prepareGeneration(doc map[string]any, sourcePath string) generationPlan {
	plan := generationPlan{
		sourcePath: sourcePath,
		config:     prepareGeneratorConfig(doc),
	}
	var names []string
	for name, raw := range doc {
		if strings.HasPrefix(name, "_") {
			continue
		}
		if _, ok := raw.(map[string]any); ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	plan.variables = make([]variableDefinition, 0, len(names))
	for _, name := range names {
		raw, ok := doc[name].(map[string]any)
		if !ok {
			continue
		}
		definition := prepareVariableDefinition(name, raw)
		plan.variables = append(plan.variables, definition)
		plan.entryCount += countGeneratedEntries(definition.values)
	}
	return plan
}

func prepareGeneratorConfig(doc map[string]any) generatorConfig {
	config := generatorConfig{packageName: "main"}
	gen, _ := doc["_gen"].(map[string]any)
	if gen == nil {
		return config
	}
	if value, ok := gen["package"].(string); ok {
		config.packageName = value
	}
	if values, ok := gen["imports"].([]any); ok {
		config.imports = make([]string, 0, len(values))
		for _, value := range values {
			text, _ := value.(string)
			config.imports = append(config.imports, text)
		}
	}
	if value, ok := gen["build_tags"].(string); ok {
		config.buildTags = value
	}
	return config
}

func prepareVariableDefinition(name string, raw map[string]any) variableDefinition {
	definition := variableDefinition{name: name, typeName: "map[string]string", values: raw["_values"]}
	if value, ok := raw["_type"].(string); ok {
		definition.typeName = value
	}
	definition.doc, _ = raw["_doc"].(string)
	if values, ok := raw["_value_map"].(map[string]any); ok {
		definition.valueMap = make(map[string]string, len(values))
		for key, value := range values {
			definition.valueMap[key], _ = value.(string)
		}
	}
	return definition
}

func countGeneratedEntries(values any) int {
	switch value := values.(type) {
	case map[string]any:
		return len(value)
	case []any:
		return len(value)
	default:
		return 0
	}
}

// ---------------------------------------------------------------------------
// Code generation
// ---------------------------------------------------------------------------

func generate(plan generationPlan) string {
	var b strings.Builder
	writeGenerationHeader(&b, plan)
	for _, definition := range plan.variables {
		writeVariableDefinition(&b, definition)
	}
	return b.String()
}

func writeGenerationHeader(b *strings.Builder, plan generationPlan) {
	if plan.config.buildTags != "" {
		fmt.Fprintf(b, "//go:build %s\n\n", plan.config.buildTags)
	}
	b.WriteString("// Code generated by data-gen. DO NOT EDIT.\n")
	fmt.Fprintf(b, "// Source: %s\n", plan.sourcePath)
	b.WriteString("// Regenerate: make data-gen\n\n")
	fmt.Fprintf(b, "package %s\n\n", plan.config.packageName)
	if len(plan.config.imports) > 0 {
		if len(plan.config.imports) == 1 {
			fmt.Fprintf(b, "import %s\n\n", goStr(plan.config.imports[0]))
		} else {
			b.WriteString("import (\n")
			for _, imp := range plan.config.imports {
				fmt.Fprintf(b, "\t%s\n", goStr(imp))
			}
			b.WriteString(")\n\n")
		}
	}
}

func writeVariableDefinition(b *strings.Builder, definition variableDefinition) {
	if definition.doc != "" {
		for _, line := range strings.Split(definition.doc, "\n") {
			fmt.Fprintf(b, "// %s\n", line)
		}
	}
	keyType, valueType := parseMapType(definition.typeName)
	if keyType != "" {
		writeMapDefinition(b, definition, keyType, valueType)
		return
	}
	if elementType := parseSliceType(definition.typeName); elementType != "" {
		writeSliceDefinition(b, definition, elementType)
	}
}

func writeMapDefinition(b *strings.Builder, definition variableDefinition, keyType, valueType string) {
	fmt.Fprintf(b, "var %s = %s{\n", definition.name, definition.typeName)
	if values, ok := definition.values.(map[string]any); ok {
		for _, key := range sortedKeys(values) {
			fmt.Fprintf(b, "\t%s: %s,\n", renderKey(key, keyType), renderValue(values[key], valueType, definition.valueMap))
		}
	}
	b.WriteString("}\n\n")
}

func writeSliceDefinition(b *strings.Builder, definition variableDefinition, elementType string) {
	values, ok := definition.values.([]any)
	if !ok {
		return
	}
	fmt.Fprintf(b, "var %s = %s{\n", definition.name, definition.typeName)
	for _, value := range values {
		fmt.Fprintf(b, "\t%s,\n", renderValue(value, elementType, definition.valueMap))
	}
	b.WriteString("}\n\n")
}

// ---------------------------------------------------------------------------
// Value rendering
// ---------------------------------------------------------------------------

func renderKey(key, keyType string) string {
	if keyType == "string" {
		return goStr(key)
	}
	return key
}

func renderValue(val any, goType string, valueMap map[string]string) string {
	// Check value_map shorthand first.
	if len(valueMap) > 0 {
		if s, ok := val.(string); ok {
			if replacement, ok := valueMap[s]; ok {
				return replacement
			}
		}
	}

	// Slice type -> recurse into elements.
	if elemType := parseSliceType(goType); elemType != "" {
		if arr, ok := val.([]any); ok {
			items := make([]string, len(arr))
			for i, item := range arr {
				items[i] = renderValue(item, elemType, valueMap)
			}
			return goType + "{" + strings.Join(items, ", ") + "}"
		}
		// Single value -> wrap in slice.
		return goType + "{" + renderValue(val, elemType, valueMap) + "}"
	}

	// Scalar types.
	switch goType {
	case "string":
		s, _ := val.(string)
		return goStr(s)
	case "bool":
		if b, ok := val.(bool); ok && b {
			return "true"
		}
		return "false"
	case "float64":
		if f, ok := val.(float64); ok {
			if f == float64(int64(f)) {
				return fmt.Sprintf("%d.0", int64(f))
			}
			return fmt.Sprintf("%v", f)
		}
		return fmt.Sprintf("%v", val)
	case "int":
		if f, ok := val.(float64); ok {
			return fmt.Sprintf("%d", int64(f))
		}
		return fmt.Sprintf("%v", val)
	case "struct{}":
		return "{}"
	default:
		// Custom type (e.g. auth.Scope) — use as-is.
		return fmt.Sprintf("%v", val)
	}
}

// ---------------------------------------------------------------------------
// Type parsing
// ---------------------------------------------------------------------------

var mapTypeRe = regexp.MustCompile(`^map\[([^\]]+)\](.+)$`)

func parseMapType(t string) (keyType, valType string) {
	m := mapTypeRe.FindStringSubmatch(t)
	if m == nil {
		return "", t
	}
	return m[1], m[2]
}

func parseSliceType(t string) string {
	if strings.HasPrefix(t, "[]") {
		return t[2:]
	}
	return ""
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func goStr(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return `"` + s + `"`
}

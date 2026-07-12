package main

import (
	"go/ast"
	"go/parser"
	"strings"
	"testing"
)

func mustExpr(t *testing.T, src string) ast.Expr {
	t.Helper()
	expr, err := parser.ParseExpr(src)
	if err != nil {
		t.Fatal(err)
	}
	return expr
}

func TestMapTypeCoversWireShapes(t *testing.T) {
	structs := map[string]*ast.StructType{"child": {}}
	tests := []struct {
		src, typ, def string
	}{
		{"string", "String", `""`},
		{"int64", "Long", "0L"},
		{"[]byte", "String", `""`},
		{"[]child", "List<Child>", "emptyList()"},
		{"*child", "Child?", "null"},
		{"time.Time", "String", `""`},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			typ, def, _, err := mapType(mustExpr(t, tt.src), structs)
			if err != nil || typ != tt.typ || def != tt.def {
				t.Fatalf("mapType = %q, %q, %v", typ, def, err)
			}
		})
	}
	if _, _, _, err := mapType(mustExpr(t, "map[string]string"), structs); err == nil {
		t.Fatal("map type must be rejected")
	}
}

func TestRenderContract(t *testing.T) {
	got := render([]kotClass{{name: "Sample", fields: []kotField{{name: "value", typ: "String", def: `""`}}}}, "ai.deneb", "internal/wire")
	for _, want := range []string{"DO NOT EDIT", "package ai.deneb", "data class Sample(", `val value: String = ""`} {
		if !strings.Contains(got, want) {
			t.Errorf("render missing %q", want)
		}
	}
}

func TestRenderContractTestsUsesSharedVerifierAndEveryClass(t *testing.T) {
	classes := []kotClass{
		{name: "Alpha", fields: []kotField{{name: "value", typ: "String", def: `""`}}},
		{name: "Beta", fields: []kotField{{name: "items", typ: "List<String>", def: "emptyList()"}}},
	}
	got := renderContractTests(classes, "ai.deneb", "internal/wire")
	for _, want := range []string{
		"DO NOT EDIT",
		"class MiniappWireDescriptorContractTest",
		"fun generatedWireModelsKeepBackwardCompatibleDefaults()",
		`name = "Alpha"`,
		`fields = listOf("value")`,
		`name = "Beta"`,
		`fields = listOf("items")`,
		"verifyContract(name, serializer, empty, fields)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("renderContractTests missing %q", want)
		}
	}
	if gotCount := strings.Count(got, "private fun <T> verifyContract("); gotCount != 1 {
		t.Fatalf("shared verifier count = %d, want 1", gotCount)
	}
}

func TestRenderFieldContractTestsCoversEveryFieldWithTypedValues(t *testing.T) {
	classes := []kotClass{{
		name: "Sample",
		fields: []kotField{
			{name: "title", typ: "String", def: `""`},
			{name: "count", typ: "Long?", def: "null"},
			{name: "children", typ: "List<Child>", def: "emptyList()"},
			{name: "child", typ: "Child", def: "Child()"},
		},
	}}
	got := renderFieldContractTests(classes, "ai.deneb", "internal/wire")
	for _, want := range []string{
		"DO NOT EDIT",
		"fun everyGeneratedFieldPreservesItsBoundaryAndRejectsWrongShape()",
		`name = "Sample.title"`,
		"valid = boundaryText",
		`name = "Sample.count"`,
		"valid = JsonPrimitive(Long.MAX_VALUE)",
		`name = "Sample.children"`,
		"expectation = Expectation.ObjectList",
		`name = "Sample.child"`,
		"expectation = Expectation.Object",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("renderFieldContractTests missing %q", want)
		}
	}
	if gotCount := strings.Count(got, "private fun <T> verifyField("); gotCount != 1 {
		t.Fatalf("shared field verifier count = %d, want 1", gotCount)
	}
}

func TestContractValueForSupportedWireShapes(t *testing.T) {
	tests := map[string]string{
		"String":          "Exact",
		"Boolean":         "Exact",
		"Int":             "Exact",
		"Long?":           "Exact",
		"Double":          "Exact",
		"List<String>":    "Exact",
		"List<Child>":     "ObjectList",
		"NestedWireModel": "Object",
	}
	for typ, want := range tests {
		t.Run(typ, func(t *testing.T) {
			if got := contractValueFor(typ).expectation; got != want {
				t.Fatalf("contractValueFor(%q).expectation = %q, want %q", typ, got, want)
			}
		})
	}
}

func TestRenderNullContractTestsCoversEveryField(t *testing.T) {
	classes := []kotClass{{
		name: "Sample",
		fields: []kotField{
			{name: "title", typ: "String", def: `""`},
			{name: "items", typ: "List<String>", def: "emptyList()"},
		},
	}}
	got := renderNullContractTests(classes, "ai.deneb", "internal/wire")
	for _, want := range []string{
		"DO NOT EDIT",
		"fun everyGeneratedFieldCoercesExplicitNullToItsDeclaredDefault()",
		`name = "Sample.title"`,
		`field = "title"`,
		`name = "Sample.items"`,
		`field = "items"`,
		"JsonObject(mapOf(field to JsonNull))",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("renderNullContractTests missing %q", want)
		}
	}
	if gotCount := strings.Count(got, "private fun <T> nullContract("); gotCount != 1 {
		t.Fatalf("shared null verifier count = %d, want 1", gotCount)
	}
}

func TestRenderValueContractTestsPopulatesEveryFieldAndKeepsOneVerifier(t *testing.T) {
	classes := []kotClass{
		{
			name: "Sample",
			fields: []kotField{
				{name: "title", typ: "String", def: `""`},
				{name: "count", typ: "Long?", def: "null"},
				{name: "children", typ: "List<Child>", def: "emptyList()"},
				{name: "child", typ: "Child", def: "Child()"},
			},
		},
		{name: "Empty"},
	}
	got := renderValueContractTests(classes, "ai.deneb", "internal/wire")
	for _, want := range []string{
		"DO NOT EDIT",
		"class MiniappWireValueContractTest",
		"fun everyGeneratedModelPreservesAllPresentValuesAndRejectsWrongShape()",
		`name = "Sample"`,
		`name = "title"`,
		"value = boundaryText",
		`name = "count"`,
		"value = JsonPrimitive(Long.MAX_VALUE)",
		`name = "children"`,
		"expectation = Expectation.ObjectList",
		`name = "child"`,
		"expectation = Expectation.Object",
		`invalidField = "title"`,
		"invalidValue = JsonObject(emptyMap())",
		`name = "Empty"`,
		"fields = emptyList()",
		"verifyContract(name, serializer, fields, invalidField, invalidValue)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("renderValueContractTests missing %q", want)
		}
	}
	if gotCount := strings.Count(got, "private fun <T> verifyContract("); gotCount != 1 {
		t.Fatalf("shared value verifier count = %d, want 1", gotCount)
	}
}

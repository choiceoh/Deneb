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

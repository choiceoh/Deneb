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
	tests := map[string]string{
		"string":    "string",
		"int64":     "number",
		"[]byte":    "string",
		"[]child":   "Child[]",
		"*child":    "Child",
		"time.Time": "string",
	}
	for src, want := range tests {
		t.Run(src, func(t *testing.T) {
			got, _, err := mapType(mustExpr(t, src), structs)
			if err != nil || got != want {
				t.Fatalf("mapType = %q, %v", got, err)
			}
		})
	}
	if _, _, err := mapType(mustExpr(t, "map[string]string"), structs); err == nil {
		t.Fatal("map type must be rejected")
	}
}

func TestRenderContract(t *testing.T) {
	got := render([]tsIface{{name: "Sample", fields: []tsField{{name: "value", typ: "string"}}}}, "internal/wire")
	for _, want := range []string{"DO NOT EDIT", "export interface Sample {", "value?: string"} {
		if !strings.Contains(got, want) {
			t.Errorf("render missing %q", want)
		}
	}
}

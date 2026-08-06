package main

import (
	"strings"
	"testing"
)

func TestGenerateOrdersSchemasAndEmitsBudgets(t *testing.T) {
	tools := []map[string]any{{
		"name":       "read_file",
		"max_output": float64(1200),
		"required":   []any{"path"},
		"anyOf":      []any{map[string]any{"required": []any{"path"}}},
		"properties": map[string]any{
			"path":  map[string]any{"description": "file path", "type": "string", "minLength": float64(1)},
			"limit": map[string]any{"maximum": float64(100), "default": float64(20), "type": "integer"},
		},
	}}

	got := generate(tools, "tools", "schemas.json")
	for _, want := range []string{
		"func ReadFileToolSchema()", `"required": []string{"path"}`,
		`"default": 20`, `"maximum": 100`, `"minLength": 1`,
		`"anyOf": []any{`, `"read_file": 1200`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated output missing %q\n%s", want, got)
		}
	}
	limit := strings.Index(got, `"limit":`)
	path := strings.Index(got, `"path":`)
	if limit < 0 || path < 0 || limit > path {
		t.Fatal("properties are not emitted deterministically")
	}
}

func TestEmittersPreserveSchemaValueTypes(t *testing.T) {
	if got := camelCase("skill_lifecycle_status"); got != "SkillLifecycleStatus" {
		t.Fatalf("camelCase = %q", got)
	}
	if got := emitValue(float64(2.5)); got != "2.5" {
		t.Fatalf("emitValue float = %q", got)
	}
	if got := emitFieldValue("additionalProperties", false, 1); got != "false" {
		t.Fatalf("additionalProperties = %q", got)
	}
	keys := orderedFieldKeys(map[string]any{"description": "d", "type": "string", "enum": []any{"a"}})
	if strings.Join(keys, ",") != "type,description,enum" {
		t.Fatalf("ordered keys = %v", keys)
	}
}

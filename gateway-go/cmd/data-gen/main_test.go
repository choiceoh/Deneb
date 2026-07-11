package main

import (
	"strings"
	"testing"
)

func TestGenerateIsDeterministicAndHonorsMetadata(t *testing.T) {
	doc := map[string]any{
		"_gen": map[string]any{
			"package":    "sample",
			"imports":    []any{"time"},
			"build_tags": "tools",
		},
		"Zed": map[string]any{
			"_type":   "map[string]int",
			"_values": map[string]any{"b": float64(2), "a": float64(1)},
		},
		"Alpha": map[string]any{
			"_type":   "[]string",
			"_values": []any{"first", "second"},
		},
	}

	got := generate(doc, "data/sample.json")
	for _, want := range []string{
		"//go:build tools", "package sample", `import "time"`,
		"var Alpha = []string{", `"first"`, `"second"`, "var Zed = map[string]int{",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated output missing %q\n%s", want, got)
		}
	}
	if strings.Index(got, `"a": 1`) > strings.Index(got, `"b": 2`) {
		t.Fatal("map keys are not sorted")
	}
	if strings.Index(got, "var Alpha") > strings.Index(got, "var Zed") {
		t.Fatal("variables are not sorted")
	}
}

func TestTypeAndValueRendering(t *testing.T) {
	if k, v := parseMapType("map[string][]int"); k != "string" || v != "[]int" {
		t.Fatalf("parseMapType = %q, %q", k, v)
	}
	if got := parseSliceType("[][]string"); got != "[]string" {
		t.Fatalf("parseSliceType = %q", got)
	}
	if got := renderValue([]any{float64(1), float64(2)}, "[]int", nil); got != "[]int{1, 2}" {
		t.Fatalf("renderValue slice = %q", got)
	}
	if got := renderValue("read", "custom.Scope", map[string]string{"read": "ScopeRead"}); got != "ScopeRead" {
		t.Fatalf("renderValue mapped = %q", got)
	}
	if got := goStr("line\n\"quoted\""); got != `"line\n\"quoted\""` {
		t.Fatalf("goStr = %q", got)
	}
}

// Package jsonschema derives strict JSON Schemas from Go types via reflection.
//
// It is the Go analogue of generating a JSON Schema from a Pydantic model: a
// structured-output call's schema becomes the Go struct it unmarshals into, so
// there is no hand-written schema literal that can drift out of sync with the
// type. Pair For[T] (the request-side schema, e.g. for an OpenAI
// response_format json_schema) with a json.Unmarshal into the same T (the
// reply-side validation) for a fully type-driven round-trip.
//
// Conventions:
//   - The object is "strict": every included field is required and
//     additionalProperties is false — matching OpenAI strict structured outputs,
//     which vLLM guided decoding honors.
//   - Field names follow the `json:"..."` tag exactly as encoding/json would (so
//     the schema and the unmarshal agree); `json:"-"` fields are omitted, and
//     anonymous embedded structs (without a json name) are flattened.
//   - An `enum:"a,b,c"` struct tag constrains a string field to that set
//     (comma-separated; members are taken verbatim, so a trailing comma keeps an
//     intended empty member, e.g. `enum:"x,y,"` allows "").
//
// Supported field kinds: struct, string, bool, integer/unsigned, float, slice/
// array (a []byte becomes a base64 string, matching encoding/json), and pointers
// to any of those. The output is byte-stable across runs (map keys marshal
// sorted), so For[T] is safe to compute once into a package var.
//
// Fail-fast: For PANICS (at init time, since callers compute into package vars)
// on a type it cannot faithfully represent — a non-struct root, a map/interface
// field, or any type that customizes its own JSON via json.Marshaler /
// encoding.TextMarshaler (e.g. time.Time, json.RawMessage), whose output is
// opaque to reflection. A loud startup failure naming the field is far safer
// than a silently-wrong schema that would constrain the model to the wrong shape
// and lose data on unmarshal. Use a plain field (e.g. an RFC3339 string for a
// time) or extend the generator.
package jsonschema

import (
	"encoding"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

var (
	jsonMarshalerType = reflect.TypeFor[json.Marshaler]()
	textMarshalerType = reflect.TypeFor[encoding.TextMarshaler]()
)

// For returns the OpenAI strict structured-output envelope for type T:
//
//	{"name": name, "strict": true, "schema": <schema of T>}
//
// Use it as llm.ResponseFormat{Type: "json_schema", JSONSchema: For[T](name)}.
// T must be a struct (the structured-output root must be a JSON object); see the
// package doc for the fail-fast rules.
func For[T any](name string) json.RawMessage {
	env := map[string]any{
		"name":   name,
		"strict": true,
		"schema": rootSchema(reflect.TypeFor[T]()),
	}
	raw, _ := json.Marshal(env) // a map of known-good values cannot fail to marshal
	return raw
}

// rootSchema builds the schema for a top-level type and enforces the object
// root that strict structured outputs require.
func rootSchema(t reflect.Type) map[string]any {
	s := schemaForType(t, "")
	if s["type"] != "object" {
		panic(fmt.Sprintf("jsonschema: the structured-output root must be a struct (a JSON object); got a %v root", s["type"]))
	}
	return s
}

func schemaForType(t reflect.Type, path string) map[string]any {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	// Types that customize their own JSON encoding produce output reflection
	// can't infer — refuse rather than emit a silently-wrong schema.
	if implementsCustomMarshaler(t) {
		panic(fmt.Sprintf("jsonschema: %s has type %s which customizes its JSON (json.Marshaler/encoding.TextMarshaler); its shape is opaque to reflection — use a plain field (e.g. an RFC3339 string for a time) or extend the generator", fieldDesc(path), t))
	}
	switch t.Kind() {
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.Slice, reflect.Array:
		// encoding/json encodes a byte slice/array as a base64 string.
		if t.Elem().Kind() == reflect.Uint8 {
			return map[string]any{"type": "string"}
		}
		return map[string]any{"type": "array", "items": schemaForType(t.Elem(), path+"[]")}
	case reflect.Struct:
		return structSchema(t, path)
	default:
		// map, interface, chan, func, complex — not representable as a strict
		// structured-output schema.
		panic(fmt.Sprintf("jsonschema: %s has unsupported kind %s; strict structured outputs need struct/string/bool/number/array fields (maps and interfaces are not representable)", fieldDesc(path), t.Kind()))
	}
}

func structSchema(t reflect.Type, path string) map[string]any {
	props := map[string]any{}
	var required []string
	collectFields(t, path, props, &required)
	return map[string]any{
		"type":                 "object",
		"properties":           props,
		"required":             dedupStrings(required),
		"additionalProperties": false,
	}
}

// collectFields walks t's exported fields into props/required, flattening
// anonymous (embedded) structs the way encoding/json does so the schema matches
// the unmarshal.
func collectFields(t reflect.Type, path string, props map[string]any, required *[]string) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name := strings.Split(f.Tag.Get("json"), ",")[0]
		if name == "-" {
			continue // json:"-" — never serialized
		}
		// Embedded struct with no json name → flatten its promoted fields into the
		// parent. Checked BEFORE the exported gate below: encoding/json promotes the
		// exported fields of an embedded struct even when the embedded type itself
		// is unexported (its StructField.IsExported() is false).
		if f.Anonymous && name == "" {
			ft := f.Type
			for ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				collectFields(ft, path, props, required)
				continue
			}
		}
		if !f.IsExported() {
			continue
		}
		if name == "" {
			name = f.Name // no tag → encoding/json uses the field name verbatim
		}
		fieldSchema := schemaForType(f.Type, childPath(path, name))
		if enum := enumValues(f); len(enum) > 0 && fieldSchema["type"] == "string" {
			fieldSchema["enum"] = enum
		}
		props[name] = fieldSchema
		*required = append(*required, name)
	}
}

// enumValues parses an `enum:"a,b,c"` struct tag. Empty tag → nil. Members are
// taken verbatim (not trimmed) so an intended empty member (trailing comma) is
// preserved.
func enumValues(f reflect.StructField) []string {
	tag := f.Tag.Get("enum")
	if tag == "" {
		return nil
	}
	return strings.Split(tag, ",")
}

// implementsCustomMarshaler reports whether t (or *t) customizes its JSON via
// json.Marshaler or encoding.TextMarshaler — output reflection cannot infer.
func implementsCustomMarshaler(t reflect.Type) bool {
	if t.Implements(jsonMarshalerType) || t.Implements(textMarshalerType) {
		return true
	}
	pt := reflect.PointerTo(t)
	return pt.Implements(jsonMarshalerType) || pt.Implements(textMarshalerType)
}

// dedupStrings returns in with duplicate entries removed, preserving first-seen
// order. Guards the rare embedded-field name collision from emitting a field
// twice in `required`.
func dedupStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func childPath(parent, name string) string {
	if parent == "" {
		return name
	}
	return parent + "." + name
}

func fieldDesc(path string) string {
	if path == "" {
		return "the root type"
	}
	return fmt.Sprintf("field %q", path)
}

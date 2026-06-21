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
//     anonymous embedded structs are flattened.
//   - An `enum:"a,b,c"` struct tag constrains a string field to that set
//     (comma-separated; members are taken verbatim, so a trailing comma keeps an
//     intended empty member, e.g. `enum:"x,y,"` allows "").
//
// The output is byte-stable across runs (map keys marshal sorted), so For[T] is
// safe to compute once into a package var.
package jsonschema

import (
	"encoding/json"
	"reflect"
	"strings"
)

// For returns the OpenAI strict structured-output envelope for type T:
//
//	{"name": name, "strict": true, "schema": <schema of T>}
//
// Use it as llm.ResponseFormat{Type: "json_schema", JSONSchema: For[T](name)}.
// T should be a struct so the JSON root is an object.
func For[T any](name string) json.RawMessage {
	env := map[string]any{
		"name":   name,
		"strict": true,
		"schema": schemaForType(reflect.TypeFor[T]()),
	}
	raw, _ := json.Marshal(env) // a map of known-good values cannot fail to marshal
	return raw
}

// Schema returns just the bare JSON Schema for T, without the name/strict
// envelope — for callers that want the raw schema (e.g. vLLM's
// structured_outputs.json field).
func Schema[T any]() json.RawMessage {
	raw, _ := json.Marshal(schemaForType(reflect.TypeFor[T]()))
	return raw
}

func schemaForType(t reflect.Type) map[string]any {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil {
		return map[string]any{}
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
		return map[string]any{"type": "array", "items": schemaForType(t.Elem())}
	case reflect.Struct:
		return structSchema(t)
	default:
		// Maps, interfaces, channels, etc. — emit a permissive (constraint-free)
		// node rather than an invalid schema. Not used by our structured outputs.
		return map[string]any{}
	}
}

func structSchema(t reflect.Type) map[string]any {
	props := map[string]any{}
	required := []string{}
	collectFields(t, props, &required)
	return map[string]any{
		"type":                 "object",
		"properties":           props,
		"required":             required,
		"additionalProperties": false,
	}
}

// collectFields walks t's exported fields into props/required, flattening
// anonymous (embedded) structs the way encoding/json does so the schema matches
// the unmarshal.
func collectFields(t reflect.Type, props map[string]any, required *[]string) {
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
				collectFields(ft, props, required)
				continue
			}
		}
		if !f.IsExported() {
			continue
		}
		if name == "" {
			name = f.Name // no tag → encoding/json uses the field name verbatim
		}
		fieldSchema := schemaForType(f.Type)
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

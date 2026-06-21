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
//     anonymous embedded structs (without a json name) are flattened. The common
//     embedding cases match encoding/json (untagged flatten, shallower-shadowing,
//     tagged-embed-as-named); the one unsupported corner is two embedded structs
//     at the SAME depth promoting the SAME json name, which encoding/json drops as
//     ambiguous but this generator includes — avoid it in structured-output types
//     (write explicit fields).
//   - An `enum:"a,b,c"` struct tag constrains a string field to that set
//     (comma-separated; members are taken verbatim, so a trailing comma keeps an
//     intended empty member, e.g. `enum:"x,y,"` allows "").
//
// Supported field kinds, matching encoding/json: struct, string, bool,
// integer/unsigned/uintptr, float, json.Number (a JSON number), a byte SLICE
// []byte (a base64 string), other slices/arrays — including a byte ARRAY [N]byte,
// which encoding/json encodes as a number array, not base64 — and pointers to
// any of those. The output is byte-stable across runs (map keys marshal sorted),
// so For[T] is safe to compute once into a package var.
//
// Fail-fast: For PANICS (at init time, since callers compute into package vars)
// on a type it cannot faithfully represent — a non-struct root, a map/interface
// field, a cyclic/self-referential type, or any type with a custom JSON decoder
// (json.Unmarshaler / encoding.TextUnmarshaler — e.g. time.Time, json.RawMessage,
// big.Int), whose accepted shape is opaque to reflection. A loud startup failure
// naming the field is far safer than a silently-wrong schema
// that would constrain the model to the wrong shape and lose data on unmarshal.
// Use a plain field (e.g. an RFC3339 string for a time) or extend the generator.
package jsonschema

import (
	"encoding"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

var (
	jsonUnmarshalerType = reflect.TypeFor[json.Unmarshaler]()
	textUnmarshalerType = reflect.TypeFor[encoding.TextUnmarshaler]()
	jsonNumberType      = reflect.TypeFor[json.Number]()
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
	s := schemaForType(t, "", map[reflect.Type]bool{})
	if s["type"] != "object" {
		panic(fmt.Sprintf("jsonschema: the structured-output root must be a struct (a JSON object); got a %v root", s["type"]))
	}
	return s
}

// schemaForType maps a Go type to its JSON Schema node. seen holds the struct
// types on the current recursion PATH so a cyclic type fails fast (a catchable
// panic) instead of recursing into an unrecoverable stack/heap exhaustion.
func schemaForType(t reflect.Type, path string, seen map[reflect.Type]bool) map[string]any {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	// json.Number is a string-kind type that encoding/json encodes as a bare
	// number — handle it before the string case below.
	if t == jsonNumberType {
		return map[string]any{"type": "number"}
	}
	// A type with a custom JSON decoder accepts a shape reflection can't infer —
	// refuse rather than emit a silently-wrong structural schema. The schema
	// constrains the model's OUTPUT, which is unmarshaled into T, so the governing
	// interface is json.Unmarshaler / encoding.TextUnmarshaler (NOT Marshaler).
	// This catches time.Time, json.RawMessage, big.Int/Float/Rat, net.IP, uuid, etc.
	if customizesJSON(t) {
		panic(fmt.Sprintf("jsonschema: %s has type %s with a custom JSON decoder (json.Unmarshaler/encoding.TextUnmarshaler); its shape is opaque to reflection — use a plain field (e.g. an RFC3339 string for a time, a number for a big.Int) or extend the generator", fieldDesc(path), t))
	}
	switch t.Kind() {
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.Slice:
		// A byte SLICE encodes as a base64 string; any other slice as an array.
		if t.Elem().Kind() == reflect.Uint8 {
			return map[string]any{"type": "string"}
		}
		return map[string]any{"type": "array", "items": schemaForType(t.Elem(), path+"[]", seen)}
	case reflect.Array:
		// A byte ARRAY [N]byte is NOT base64 — encoding/json encodes any array,
		// bytes included, as a JSON array of its elements.
		return map[string]any{"type": "array", "items": schemaForType(t.Elem(), path+"[]", seen)}
	case reflect.Struct:
		return structSchema(t, path, seen)
	default:
		// map, interface, chan, func, complex — not representable as a strict
		// structured-output schema.
		panic(fmt.Sprintf("jsonschema: %s has unsupported kind %s; strict structured outputs need struct/string/bool/number/array fields (maps and interfaces are not representable)", fieldDesc(path), t.Kind()))
	}
}

func structSchema(t reflect.Type, path string, seen map[reflect.Type]bool) map[string]any {
	if seen[t] {
		panic(fmt.Sprintf("jsonschema: %s has cyclic/self-referential type %s; a JSON Schema can't express an unbounded type", fieldDesc(path), t))
	}
	seen[t] = true
	defer delete(seen, t)
	props := map[string]any{}
	var required []string
	collectFields(t, path, props, &required, seen)
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
func collectFields(t reflect.Type, path string, props map[string]any, required *[]string, seen map[reflect.Type]bool) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name := strings.Split(f.Tag.Get("json"), ",")[0]
		if name == "-" {
			continue // json:"-" — never serialized
		}
		// Embedded struct with no json name → flatten its promoted fields into the
		// parent. Checked BEFORE the exported gate below: encoding/json promotes the
		// exported fields of an embedded struct even when the embedded type itself
		// is unexported (its StructField.IsExported() is false). The cycle guard
		// applies here too (embedding can form a cycle).
		if f.Anonymous && name == "" {
			ft := f.Type
			for ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				if seen[ft] {
					panic(fmt.Sprintf("jsonschema: %s embeds cyclic/self-referential type %s", fieldDesc(path), ft))
				}
				seen[ft] = true
				collectFields(ft, path, props, required, seen)
				delete(seen, ft)
				continue
			}
		}
		// A tagged anonymous field is a NAMED nested object (encoding/json marshals
		// it under its tag name even when the embedded type is unexported), so it
		// bypasses this gate. Everything else — a regular field OR an untagged
		// anonymous non-struct (the struct case already flattened above) — obeys the
		// export rule: encoding/json promotes an anonymous non-struct field only when
		// it is exported.
		taggedEmbed := f.Anonymous && name != ""
		if !taggedEmbed && !f.IsExported() {
			continue
		}
		if name == "" {
			name = f.Name // no tag → encoding/json uses the field name verbatim
		}
		fieldSchema := schemaForType(f.Type, childPath(path, name), seen)
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

// customizesJSON reports whether json.Unmarshal would use a custom decoder for t
// (json.Unmarshaler or encoding.TextUnmarshaler) instead of decoding structurally
// — making its accepted shape opaque to reflection. reflect.PointerTo(t) covers
// pointer-receiver decoders (the stdlib idiom: time.Time, big.Int, net.IP), which
// json.Unmarshal invokes because its target is addressable. Marshaler is
// deliberately NOT checked: the schema constrains the model's output (unmarshaled
// into t), never a value marshaled FROM t, so a marshal-only type is decoded
// structurally and reflects faithfully.
func customizesJSON(t reflect.Type) bool {
	pt := reflect.PointerTo(t)
	return pt.Implements(jsonUnmarshalerType) || pt.Implements(textUnmarshalerType)
}

// dedupStrings returns in with duplicate entries removed, preserving first-seen
// order. Keeps `required` clean when a field name is shadowed across an embedded
// struct (encoding/json's shallower-wins promotion appends the name twice).
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

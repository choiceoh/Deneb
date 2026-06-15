package jsonutil

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
)

// coerceStringScalars rewrites top-level object members whose JSON value is a
// quoted string but whose destination struct field is numeric or bool, into the
// bare scalar token ("5" → 5, "True" → true), based on v's reflected field types.
// It returns the rewritten JSON and whether anything changed.
//
// Why: LLMs — notably the local main model — routinely emit numeric/boolean tool
// params as quoted strings ({"max":"5"}, {"download":"True"}). Strict json decoding
// rejects those with an UnmarshalTypeError, failing the ENTIRE tool call over a
// benign quirk (observed in prod: gmail/sessions calls retried 3× then gave up).
// UnmarshalInto calls this only after a type-mismatch failure, so correctly-typed
// calls never reach it.
//
// Scope: top-level scalar fields only. Tool params are flat structs, so nested
// objects/arrays (rare) are left untouched. Non-object JSON, unknown keys, and
// values that don't parse as the target scalar are left as-is.
func coerceStringScalars(data []byte, v any) ([]byte, bool) {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return nil, false
	}
	rt := rv.Elem().Type()
	if rt.Kind() != reflect.Struct {
		return nil, false
	}

	var obj map[string]json.RawMessage
	if json.Unmarshal(data, &obj) != nil {
		return nil, false // not a JSON object — nothing to coerce
	}

	kindByTag := scalarKindByJSONTag(rt)
	changed := false
	for key, raw := range obj {
		if len(raw) == 0 || raw[0] != '"' {
			continue // only quoted strings are coercion candidates
		}
		kind, ok := kindByTag[key]
		if !ok {
			continue
		}
		var s string
		if json.Unmarshal(raw, &s) != nil {
			continue
		}
		repl, ok := scalarTokenFor(kind, strings.TrimSpace(s))
		if !ok {
			continue
		}
		obj[key] = json.RawMessage(repl)
		changed = true
	}
	if !changed {
		return nil, false
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return nil, false
	}
	return out, true
}

// scalarKindByJSONTag maps each struct field's JSON key to its reflect.Kind, but
// only for numeric/bool fields (the coercible ones). Embedded structs and slices
// are skipped — tool params are flat.
func scalarKindByJSONTag(rt reflect.Type) map[string]reflect.Kind {
	out := make(map[string]reflect.Kind, rt.NumField())
	for i := range rt.NumField() {
		f := rt.Field(i)
		if !f.IsExported() {
			continue
		}
		if !isCoercibleKind(f.Type.Kind()) {
			continue
		}
		name := strings.Split(f.Tag.Get("json"), ",")[0]
		if name == "" || name == "-" {
			name = f.Name
		}
		out[name] = f.Type.Kind()
	}
	return out
}

// scalarTokenFor returns the bare JSON token for a string value targeting the
// given scalar kind, and whether the value parses cleanly as that kind.
func scalarTokenFor(kind reflect.Kind, s string) (string, bool) {
	switch {
	case isIntKind(kind):
		if _, err := strconv.ParseInt(s, 10, 64); err != nil {
			return "", false
		}
		return s, true
	case isUintKind(kind):
		if _, err := strconv.ParseUint(s, 10, 64); err != nil {
			return "", false
		}
		return s, true
	case kind == reflect.Float32 || kind == reflect.Float64:
		if _, err := strconv.ParseFloat(s, 64); err != nil {
			return "", false
		}
		return s, true
	case kind == reflect.Bool:
		b, err := strconv.ParseBool(s)
		if err != nil {
			return "", false
		}
		return strconv.FormatBool(b), true
	default:
		return "", false
	}
}

func isCoercibleKind(k reflect.Kind) bool {
	return isIntKind(k) || isUintKind(k) || k == reflect.Float32 || k == reflect.Float64 || k == reflect.Bool
}

func isIntKind(k reflect.Kind) bool {
	switch k {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return true
	default:
		return false
	}
}

func isUintKind(k reflect.Kind) bool {
	switch k {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return true
	default:
		return false
	}
}

package jsonschema

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

// decode parses a generated schema into a generic map for assertions.
func decode(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("generated schema is not valid JSON: %v\n%s", err, raw)
	}
	return m
}

// schemaOf returns the inner schema object from a For[T] envelope.
func schemaOf(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	env := decode(t, raw)
	if env["name"] == nil || env["strict"] != true {
		t.Fatalf("envelope missing name/strict: %v", env)
	}
	s, ok := env["schema"].(map[string]any)
	if !ok {
		t.Fatalf("envelope.schema is not an object: %v", env["schema"])
	}
	return s
}

func props(t *testing.T, schema map[string]any) map[string]any {
	t.Helper()
	p, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema.properties missing: %v", schema)
	}
	return p
}

func requiredSet(t *testing.T, schema map[string]any) map[string]bool {
	t.Helper()
	r, ok := schema["required"].([]any)
	if !ok {
		t.Fatalf("schema.required missing: %v", schema)
	}
	out := map[string]bool{}
	for _, v := range r {
		out[v.(string)] = true
	}
	return out
}

func TestFor_Primitives(t *testing.T) {
	type prim struct {
		S string  `json:"s"`
		I int     `json:"i"`
		U uint16  `json:"u"`
		B bool    `json:"b"`
		F float64 `json:"f"`
	}
	s := schemaOf(t, For[prim]("prim"))
	if s["type"] != "object" {
		t.Errorf("root type = %v, want object", s["type"])
	}
	if s["additionalProperties"] != false {
		t.Errorf("additionalProperties = %v, want false", s["additionalProperties"])
	}
	p := props(t, s)
	want := map[string]string{"s": "string", "i": "integer", "u": "integer", "b": "boolean", "f": "number"}
	for field, typ := range want {
		fs, ok := p[field].(map[string]any)
		if !ok {
			t.Errorf("missing property %q", field)
			continue
		}
		if fs["type"] != typ {
			t.Errorf("%s type = %v, want %v", field, fs["type"], typ)
		}
	}
	// Strict: every field required.
	req := requiredSet(t, s)
	for field := range want {
		if !req[field] {
			t.Errorf("%s should be required (strict)", field)
		}
	}
}

func TestFor_JSONTagRenameAndSkip(t *testing.T) {
	type tagged struct {
		Renamed  string `json:"renamed_name"`
		Omit     string `json:"-"`
		WithOpts string `json:"with_opts,omitempty"`
		NoTag    string
		unexp    string //nolint:unused // exercises the unexported-skip path
	}
	s := schemaOf(t, For[tagged]("tagged"))
	p := props(t, s)
	if _, ok := p["renamed_name"]; !ok {
		t.Error("json tag rename not honored")
	}
	if _, ok := p["Omit"]; ok {
		t.Error(`json:"-" field must be omitted`)
	}
	if _, ok := p["with_opts"]; !ok {
		t.Error("name before comma in tag not honored")
	}
	if _, ok := p["NoTag"]; !ok {
		t.Error("untagged exported field should use its Go name (matches encoding/json)")
	}
	if _, ok := p["unexp"]; ok {
		t.Error("unexported field must be omitted")
	}
	// omitempty fields are still required under strict mode.
	if !requiredSet(t, s)["with_opts"] {
		t.Error("omitempty field still required under strict mode")
	}
}

func TestFor_EnumTag(t *testing.T) {
	type withEnum struct {
		Priority string `json:"priority" enum:"high,medium,low"`
		Plain    string `json:"plain"`
		Count    int    `json:"count" enum:"1,2,3"` // enum tag on non-string is ignored
	}
	p := props(t, schemaOf(t, For[withEnum]("e")))
	pr := p["priority"].(map[string]any)
	enum, ok := pr["enum"].([]any)
	if !ok || len(enum) != 3 || enum[0] != "high" || enum[2] != "low" {
		t.Errorf("priority enum = %v, want [high medium low]", pr["enum"])
	}
	if _, ok := p["plain"].(map[string]any)["enum"]; ok {
		t.Error("plain string without enum tag must not get an enum")
	}
	if _, ok := p["count"].(map[string]any)["enum"]; ok {
		t.Error("enum tag on a non-string field must be ignored")
	}
}

func TestFor_EmptyEnumMemberPreserved(t *testing.T) {
	// A trailing comma marks an intended empty member (used so a "not applicable"
	// value can be the empty string under strict mode).
	type withBlank struct {
		Kind string `json:"kind" enum:"a,b,"`
	}
	enum := props(t, schemaOf(t, For[withBlank]("b")))["kind"].(map[string]any)["enum"].([]any)
	if len(enum) != 3 || enum[2] != "" {
		t.Errorf("empty enum member not preserved: %v", enum)
	}
}

func TestFor_NestedAndSlices(t *testing.T) {
	type item struct {
		Index    int  `json:"index"`
		Relevant bool `json:"relevant"`
	}
	type bundle struct {
		Items   []item   `json:"items"`
		Tags    []string `json:"tags"`
		Lead    item     `json:"lead"`
		PtrLead *item    `json:"ptrLead"`
	}
	p := props(t, schemaOf(t, For[bundle]("b")))

	// []item → array of object
	items := p["items"].(map[string]any)
	if items["type"] != "array" {
		t.Fatalf("items type = %v, want array", items["type"])
	}
	itemSchema := items["items"].(map[string]any)
	if itemSchema["type"] != "object" {
		t.Errorf("items.items type = %v, want object", itemSchema["type"])
	}
	if ip := itemSchema["properties"].(map[string]any); ip["index"].(map[string]any)["type"] != "integer" || ip["relevant"].(map[string]any)["type"] != "boolean" {
		t.Errorf("nested item property types wrong: %v", ip)
	}

	// []string → array of string
	tags := p["tags"].(map[string]any)
	if tags["type"] != "array" || tags["items"].(map[string]any)["type"] != "string" {
		t.Errorf("tags = %v, want array of string", tags)
	}

	// nested struct (value and pointer) → object
	if p["lead"].(map[string]any)["type"] != "object" {
		t.Errorf("lead should be object")
	}
	if p["ptrLead"].(map[string]any)["type"] != "object" {
		t.Errorf("pointer-to-struct should unwrap to object")
	}
}

func TestFor_EmbeddedFlattened(t *testing.T) {
	type base struct {
		A string `json:"a"`
	}
	type derived struct {
		base
		B string `json:"b"`
	}
	s := schemaOf(t, For[derived]("d"))
	p := props(t, s)
	if _, ok := p["a"]; !ok {
		t.Error("embedded struct field a should be flattened into parent")
	}
	if _, ok := p["b"]; !ok {
		t.Error("own field b missing")
	}
	if _, ok := p["base"]; ok {
		t.Error("embedded struct must be flattened, not nested under its type name")
	}
	req := requiredSet(t, s)
	if !req["a"] || !req["b"] {
		t.Error("flattened + own fields both required")
	}
}

func TestFor_ByteStable(t *testing.T) {
	type x struct {
		B string `json:"b"`
		A string `json:"a"`
		C string `json:"c"`
	}
	first := For[x]("x")
	for i := 0; i < 5; i++ {
		if !reflect.DeepEqual([]byte(first), []byte(For[x]("x"))) {
			t.Fatal("For[T] output is not byte-stable across calls")
		}
	}
}

func TestFor_ByteSliceIsString(t *testing.T) {
	// encoding/json encodes []byte as a base64 string, so the schema must say
	// string — not array-of-integer (the naive reflection result).
	type withBytes struct {
		B []byte `json:"b"`
	}
	p := props(t, schemaOf(t, For[withBytes]("b")))
	if got := p["b"].(map[string]any)["type"]; got != "string" {
		t.Errorf("[]byte field type = %v, want string (base64)", got)
	}
}

func TestFor_FailsFastOnUnrepresentable(t *testing.T) {
	// A type the generator cannot faithfully express must PANIC (loud init-time
	// failure) rather than emit a silently-wrong schema that loses data on
	// unmarshal. Each of these diverges from encoding/json under naive reflection.
	type withTime struct {
		T time.Time `json:"t"` // json.Marshaler → RFC3339 string, not an object
	}
	type withRaw struct {
		R json.RawMessage `json:"r"` // json.Marshaler → arbitrary embedded JSON
	}
	type withMap struct {
		M map[string]int `json:"m"` // dynamic keys — not a strict object
	}
	type withIface struct {
		V any `json:"v"` // interface — unconstrained
	}
	cases := map[string]func(){
		"time.Time":       func() { For[withTime]("a") },
		"json.RawMessage": func() { For[withRaw]("a") },
		"map":             func() { For[withMap]("a") },
		"interface":       func() { For[withIface]("a") },
		"non-struct root": func() { For[string]("a") },
		"slice root":      func() { For[[]int]("a") },
	}
	for name, fn := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Errorf("For with %s: expected panic, got none", name)
				}
			}()
			fn()
		})
	}
}

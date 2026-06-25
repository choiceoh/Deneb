package chat

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

func TestRepairToolArguments(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		want     string // semantic-equal target when repaired; ignored when repaired==false
		repaired bool
	}{
		{name: "valid object untouched", in: `{"file_path":"a.txt"}`, repaired: false},
		{name: "valid empty object untouched", in: `{}`, repaired: false},
		{name: "valid python-looking string untouched", in: `{"note":"None reported, True story,"}`, repaired: false},
		{name: "markdown fence json", in: "```json\n{\"a\":1}\n```", want: `{"a":1}`, repaired: true},
		{name: "markdown fence bare", in: "```\n{\"a\":1}\n```", want: `{"a":1}`, repaired: true},
		{name: "markdown fence single line", in: "```{\"a\":1}```", want: `{"a":1}`, repaired: true},
		{name: "trailing comma object", in: `{"a":1,}`, want: `{"a":1}`, repaired: true},
		{name: "trailing comma array", in: `{"a":[1,2,]}`, want: `{"a":[1,2]}`, repaired: true},
		{name: "python None", in: `{"a":None}`, want: `{"a":null}`, repaired: true},
		{name: "python True and False", in: `{"a":True,"b":False}`, want: `{"a":true,"b":false}`, repaired: true},
		{name: "combined fence comma literal", in: "```json\n{\"a\":True,\"b\":[1,],}\n```", want: `{"a":true,"b":[1]}`, repaired: true},
		{name: "comma inside string preserved", in: `{"a":"x,",}`, want: `{"a":"x,"}`, repaired: true},
		{name: "literal inside string preserved", in: `{"a":"None",}`, want: `{"a":"None"}`, repaired: true},
		{name: "NoneType stays invalid passthrough", in: `{"a":NoneType}`, repaired: false},
		{name: "unrepairable garbage passthrough", in: `not json`, repaired: false},
		{name: "whitespace passthrough", in: `   `, repaired: false},
		{name: "empty passthrough", in: ``, repaired: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, did := repairToolArguments(json.RawMessage(tc.in))
			if did != tc.repaired {
				t.Fatalf("repaired = %v, want %v (got output %q)", did, tc.repaired, string(got))
			}
			if !tc.repaired {
				if string(got) != tc.in {
					t.Fatalf("passthrough mutated input: got %q, want %q", string(got), tc.in)
				}
				return
			}
			if !json.Valid(got) {
				t.Fatalf("repaired output is not valid JSON: %q", string(got))
			}
			if !jsonSemanticEqual(string(got), tc.want) {
				t.Fatalf("repaired = %q, want equivalent to %q", string(got), tc.want)
			}
		})
	}
}

// TestExecute_RepairsMalformedArgs verifies the repair is wired into the tool
// dispatch path: a tool registered through the registry receives valid,
// repaired JSON even when the model emitted a fenced/Python-literal/trailing-
// comma call.
func TestExecute_RepairsMalformedArgs(t *testing.T) {
	r := NewToolRegistry()
	var gotInput string
	r.Register("echo", func(_ context.Context, input json.RawMessage) (string, error) {
		gotInput = string(input)
		return "ok", nil
	})

	out, err := r.Execute(context.Background(), "echo", json.RawMessage("```json\n{\"x\":True,}\n```"))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if out != "ok" {
		t.Fatalf("Execute output = %q, want ok", out)
	}
	if !json.Valid(json.RawMessage(gotInput)) {
		t.Fatalf("tool received invalid JSON: %q", gotInput)
	}
	if !jsonSemanticEqual(gotInput, `{"x":true}`) {
		t.Fatalf("tool received %q, want equivalent to %q", gotInput, `{"x":true}`)
	}
}

// TestExecute_ValidArgsUntouched guards the invariant that already-valid
// arguments reach the tool byte-for-byte (the repair must never rewrite a
// well-formed call).
func TestExecute_ValidArgsUntouched(t *testing.T) {
	r := NewToolRegistry()
	var gotInput string
	r.Register("echo", func(_ context.Context, input json.RawMessage) (string, error) {
		gotInput = string(input)
		return "ok", nil
	})

	const valid = `{"x":true,"note":"None, True, trailing,"}`
	if _, err := r.Execute(context.Background(), "echo", json.RawMessage(valid)); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if gotInput != valid {
		t.Fatalf("valid args were mutated: got %q, want %q", gotInput, valid)
	}
}

func jsonSemanticEqual(a, b string) bool {
	var va, vb any
	if err := json.Unmarshal([]byte(a), &va); err != nil {
		return false
	}
	if err := json.Unmarshal([]byte(b), &vb); err != nil {
		return false
	}
	return reflect.DeepEqual(va, vb)
}

package tools

import (
	"encoding/json"
	"testing"
)

// flexInt must accept both a JSON number and a quoted number, because LLMs
// emit numeric tool params both ways. Pins the fix for sessions_history /
// sessions_search erroring on `"limit":"10"`.
func TestFlexInt_AcceptsStringAndNumber(t *testing.T) {
	cases := map[string]int{
		`10`:    10,
		`"10"`:  10,
		`" 7 "`: 7,
		`""`:    0,
		`0`:     0,
	}
	for in, want := range cases {
		var f flexInt
		if err := json.Unmarshal([]byte(in), &f); err != nil {
			t.Errorf("Unmarshal(%s) errored: %v", in, err)
			continue
		}
		if f.Int() != want {
			t.Errorf("Unmarshal(%s) = %d, want %d", in, f.Int(), want)
		}
	}
}

func TestFlexInt_RejectsNonNumericString(t *testing.T) {
	var f flexInt
	if err := json.Unmarshal([]byte(`"abc"`), &f); err == nil {
		t.Error(`Unmarshal("abc") should error, got nil`)
	}
}

// A flexInt field embedded in a struct unmarshals from a quoted number — the
// exact shape that was failing in the sessions tool.
func TestFlexInt_InStruct(t *testing.T) {
	var p struct {
		Limit flexInt `json:"limit"`
	}
	if err := json.Unmarshal([]byte(`{"limit":"25"}`), &p); err != nil {
		t.Fatalf("struct unmarshal errored: %v", err)
	}
	if p.Limit.Int() != 25 {
		t.Errorf("Limit = %d, want 25", p.Limit.Int())
	}
}

package tools

import (
	"encoding/json"
	"testing"
)

func TestFlexBool(t *testing.T) {
	cases := map[string]bool{
		`true`:    true,
		`false`:   false,
		`"true"`:  true, // the prod bug: LLM quotes the bool → skill_lifecycle dropped the decision
		`"false"`: false,
		`"1"`:     true,
		`"0"`:     false,
		`""`:      false, // empty string → false
	}
	for in, want := range cases {
		var v flexBool
		if err := json.Unmarshal([]byte(in), &v); err != nil {
			t.Errorf("Unmarshal(%s) error: %v", in, err)
			continue
		}
		if v.Bool() != want {
			t.Errorf("Unmarshal(%s) = %v, want %v", in, v.Bool(), want)
		}
	}

	// The on-the-wire shape that was failing: a quoted execute inside the params.
	var p struct {
		Execute flexBool `json:"execute"`
	}
	if err := json.Unmarshal([]byte(`{"execute":"true"}`), &p); err != nil {
		t.Fatalf(`{"execute":"true"} must parse, got: %v`, err)
	}
	if !p.Execute.Bool() {
		t.Error(`{"execute":"true"} should yield true`)
	}
}

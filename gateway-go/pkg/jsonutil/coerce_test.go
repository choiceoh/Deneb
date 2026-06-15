package jsonutil

import "testing"

func TestUnmarshalInto_CoercesStringScalars(t *testing.T) {
	var p struct {
		Max      int     `json:"max"`
		Download bool    `json:"download"`
		HTML     bool    `json:"html"`
		Ratio    float64 `json:"ratio"`
		Name     string  `json:"name"`
	}
	in := []byte(`{"max":"5","download":"True","html":"false","ratio":"0.7","name":"5"}`)
	if err := UnmarshalInto("t", in, &p); err != nil {
		t.Fatalf("coercion should succeed: %v", err)
	}
	if p.Max != 5 || !p.Download || p.HTML || p.Ratio != 0.7 || p.Name != "5" {
		t.Errorf("got %+v", p)
	}
}

func TestUnmarshalInto_FastPathUnaffected(t *testing.T) {
	var p struct {
		Max      int  `json:"max"`
		Download bool `json:"download"`
	}
	if err := UnmarshalInto("t", []byte(`{"max":5,"download":true}`), &p); err != nil {
		t.Fatalf("fast path: %v", err)
	}
	if p.Max != 5 || !p.Download {
		t.Errorf("got %+v", p)
	}
}

func TestUnmarshalInto_NonCoercibleStillErrors(t *testing.T) {
	var p struct {
		N int `json:"n"`
	}
	if err := UnmarshalInto("t", []byte(`{"n":"abc"}`), &p); err == nil {
		t.Error("a non-numeric string for an int field must still error")
	}
	if err := UnmarshalInto("t", []byte(`{bad json`), &p); err == nil {
		t.Error("malformed JSON must still error")
	}
}

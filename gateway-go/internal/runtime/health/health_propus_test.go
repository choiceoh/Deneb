package health

import (
	"encoding/json"
	"testing"
)

func TestPropusSectionMarshalJSONPreservesFlatWireContract(t *testing.T) {
	section := PropusSection{values: propusValues{
		"system":           "Propus",
		"doctrine_version": "2.2",
	}}

	got, err := json.Marshal(section)
	if err != nil {
		t.Fatalf("marshal Propus section: %v", err)
	}
	const want = `{"doctrine_version":"2.2","system":"Propus"}`
	if string(got) != want {
		t.Fatalf("Propus wire payload = %s, want %s", got, want)
	}
}

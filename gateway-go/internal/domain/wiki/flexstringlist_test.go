package wiki

import (
	"encoding/json"
	"reflect"
	"testing"
)

// The synthesis parse must survive an LLM that emits tags/related as a single
// comma-separated string instead of a JSON array. Previously this failed the
// whole json.Unmarshal ("cannot unmarshal string into Go struct field
// wikiUpdate.tags of type []string") and discarded the entire dream cycle.
func TestWikiUpdateToleratesStringTags(t *testing.T) {
	raw := `[
	  {"action":"create","path":"기술/dgx-spark.md","title":"DGX Spark","tags":"하드웨어, NVIDIA, GB10","related":"프로젝트/deneb"}
	]`
	var updates []wikiUpdate
	if err := json.Unmarshal([]byte(raw), &updates); err != nil {
		t.Fatalf("unmarshal with string tags/related: %v", err)
	}
	if len(updates) != 1 {
		t.Fatalf("got %d updates, want 1", len(updates))
	}
	if got, want := []string(updates[0].Tags), []string{"하드웨어", "NVIDIA", "GB10"}; !reflect.DeepEqual(got, want) {
		t.Errorf("tags = %v, want %v", got, want)
	}
	if got, want := []string(updates[0].Related), []string{"프로젝트/deneb"}; !reflect.DeepEqual(got, want) {
		t.Errorf("related = %v, want %v", got, want)
	}
}

// A proper JSON array must still parse unchanged.
func TestWikiUpdateArrayTagsUnchanged(t *testing.T) {
	raw := `[{"action":"create","path":"a.md","title":"A","tags":["x","y"]}]`
	var updates []wikiUpdate
	if err := json.Unmarshal([]byte(raw), &updates); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got, want := []string(updates[0].Tags), []string{"x", "y"}; !reflect.DeepEqual(got, want) {
		t.Errorf("tags = %v, want %v", got, want)
	}
}

func TestFlexStringListUnmarshal(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"array", `["a","b"]`, []string{"a", "b"}},
		{"comma string", `"a, b, c"`, []string{"a", "b", "c"}},
		{"semicolon string", `"a; b"`, []string{"a", "b"}},
		{"single value keeps inner spaces", `"케이원 일렉트릭"`, []string{"케이원 일렉트릭"}},
		{"empty string", `""`, []string{}},
		{"null", `null`, nil},
		{"empty array", `[]`, []string{}},
		{"surrounding blanks dropped", `" , a , "`, []string{"a"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var f flexStringList
			if err := json.Unmarshal([]byte(tc.in), &f); err != nil {
				t.Fatalf("unmarshal %q: %v", tc.in, err)
			}
			if !reflect.DeepEqual([]string(f), tc.want) {
				t.Errorf("got %#v, want %#v", []string(f), tc.want)
			}
		})
	}
}

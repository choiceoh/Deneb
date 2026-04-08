package skills

import (
	"testing"
)



func TestNormalizeSkillFilter_TrimsAndDeduplicates(t *testing.T) {
	input := []string{"  weather  ", "github", "weather", " coding ", "github"}
	result := NormalizeSkillFilter(input)

	if len(result) != 3 {
		t.Fatalf("got %d: %v, want 3 unique entries", len(result), result)
	}
	// Should be sorted.
	expected := []string{"coding", "github", "weather"}
	for i, s := range result {
		if s != expected[i] {
			t.Errorf("result[%d] = %q, want %q", i, s, expected[i])
		}
	}
}




func TestMatchesSkillFilter_Equal(t *testing.T) {
	a := []string{"weather", "github"}
	b := []string{"github", "weather"}
	if !MatchesSkillFilter(a, b) {
		t.Error("expected true for same elements in different order")
	}
}

func TestMatchesSkillFilter_Different(t *testing.T) {
	a := []string{"weather"}
	b := []string{"github"}
	if MatchesSkillFilter(a, b) {
		t.Error("expected false for different filters")
	}
}



func TestDedupeAndSort(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{"empty", []string{}, []string{}},
		{"no dupes", []string{"b", "a", "c"}, []string{"a", "b", "c"}},
		{"with dupes", []string{"b", "a", "b", "c", "a"}, []string{"a", "b", "c"}},
		{"single", []string{"x"}, []string{"x"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := dedupeAndSort(tc.input)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("got[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

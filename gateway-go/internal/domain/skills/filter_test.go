package skills

import (
	"testing"
)

func TestNormalizeSkillFilter_Nil(t *testing.T) {
	result := NormalizeSkillFilter(nil)
	if result != nil {
		t.Errorf("got %v, want nil for nil input", result)
	}
}

func TestNormalizeSkillFilter_Empty(t *testing.T) {
	result := NormalizeSkillFilter([]string{})
	if len(result) != 0 {
		t.Errorf("got %v, want empty slice", result)
	}
}

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

func TestNormalizeSkillFilter_SkipsEmpty(t *testing.T) {
	input := []string{"", "  ", "valid", ""}
	result := NormalizeSkillFilter(input)

	if len(result) != 1 || result[0] != "valid" {
		t.Errorf("got %v, want [valid]", result)
	}
}

func TestMatchesSkillFilter_BothNil(t *testing.T) {
	if !MatchesSkillFilter(nil, nil) {
		t.Error("expected true for both nil")
	}
}

func TestMatchesSkillFilter_OneNil(t *testing.T) {
	if MatchesSkillFilter(nil, []string{"a"}) {
		t.Error("expected false when one is nil")
	}
	if MatchesSkillFilter([]string{"a"}, nil) {
		t.Error("expected false when one is nil")
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

func TestMatchesSkillFilter_DifferentLength(t *testing.T) {
	a := []string{"a", "b"}
	b := []string{"a"}
	if MatchesSkillFilter(a, b) {
		t.Error("expected false for different lengths")
	}
}

func TestMatchesSkillFilter_EmptySlices(t *testing.T) {
	if !MatchesSkillFilter([]string{}, []string{}) {
		t.Error("expected true for both empty")
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

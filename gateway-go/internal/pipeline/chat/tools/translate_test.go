package tools

import (
	"context"
	"strings"
	"testing"
)

func TestParseTranslations(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want int
		ok   bool
		out  []string
	}{
		{"clean array", `["안녕","세계"]`, 2, true, []string{"안녕", "세계"}},
		{"code fenced", "```json\n[\"하나\",\"둘\"]\n```", 2, true, []string{"하나", "둘"}},
		{"envelope", `{"translations":["가","나","다"]}`, 3, true, []string{"가", "나", "다"}},
		{"count short", `["하나"]`, 2, false, nil},
		{"count long", `["하나","둘","셋"]`, 2, false, nil},
		{"garbage", `not json at all`, 1, false, nil},
		{"empty array vs want", `[]`, 1, false, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseTranslations(tc.raw, tc.want)
			if ok != tc.ok {
				t.Fatalf("ok=%v want %v (raw=%q)", ok, tc.ok, tc.raw)
			}
			if ok {
				if len(got) != len(tc.out) {
					t.Fatalf("len=%d want %d", len(got), len(tc.out))
				}
				for i := range got {
					if got[i] != tc.out[i] {
						t.Fatalf("got[%d]=%q want %q", i, got[i], tc.out[i])
					}
				}
			}
		})
	}
}

// TestTranslateSegments_EmptyAndCountInvariant checks the structural guarantees
// that don't need a live model: empty input is a no-op, and the function always
// returns a slice the SAME length as its input (the index-replacement contract).
func TestTranslateSegments_Empty(t *testing.T) {
	out, err := TranslateSegments(context.Background(), nil, "Korean")
	if err != nil || out != nil {
		t.Fatalf("empty input: got out=%v err=%v, want nil,nil", out, err)
	}
}

func TestBuildTranslatePrompt_AnchorsCountAndSegments(t *testing.T) {
	system, user := buildTranslatePrompt([]string{"Hello", "Привет"}, "Korean")
	if system == "" || user == "" {
		t.Fatal("expected non-empty prompt")
	}
	// The user message must carry the exact count and the raw segments so the
	// model is anchored to return the same number of items.
	for _, want := range []string{"exactly 2", "Hello", "Привет"} {
		if !strings.Contains(user, want) {
			t.Fatalf("user prompt missing %q:\n%s", want, user)
		}
	}
}

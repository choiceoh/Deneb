package pipeline

import "testing"

func TestSplitProviderModel(t *testing.T) {
	tests := []struct {
		in       string
		provider string
		model    string
	}{
		{in: "gpt-5", provider: "", model: "gpt-5"},
		{in: "openai/gpt-5", provider: "openai", model: "gpt-5"},
		{in: "openai/", provider: "openai", model: ""},
	}

	for _, tc := range tests {
		parts := SplitProviderModel(tc.in)
		if parts[0] != tc.provider || parts[1] != tc.model {
			t.Fatalf("SplitProviderModel(%q) = [%q %q], want [%q %q]", tc.in, parts[0], parts[1], tc.provider, tc.model)
		}
	}
}

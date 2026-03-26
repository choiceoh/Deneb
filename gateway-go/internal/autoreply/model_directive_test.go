package autoreply

import "testing"

func TestExtractModelDirective(t *testing.T) {
	tests := []struct {
		name          string
		body          string
		aliases       []string
		wantCleaned   string
		wantModel     string
		wantProfile   string
		wantDirective bool
	}{
		{"no directive", "hello world", nil, "hello world", "", "", false},
		{"model directive", "/model anthropic/claude-3", nil, "", "anthropic/claude-3", "", true},
		{"model with text", "hey /model gpt-4 what's up", nil, "hey what's up", "gpt-4", "", true},
		{"model with profile", "/model anthropic/claude-3:work", nil, "", "anthropic/claude-3", "work", true},
		{"colon syntax", "/model:gpt-4", nil, "", "gpt-4", "", true},
		{"alias", "/gpt", []string{"gpt"}, "", "gpt", "", true},
		{"empty", "", nil, "", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractModelDirective(tt.body, tt.aliases)
			if got.HasDirective != tt.wantDirective {
				t.Errorf("HasDirective = %v, want %v", got.HasDirective, tt.wantDirective)
			}
			if tt.wantModel != "" && got.RawModel != tt.wantModel {
				t.Errorf("RawModel = %q, want %q", got.RawModel, tt.wantModel)
			}
			if got.RawProfile != tt.wantProfile {
				t.Errorf("RawProfile = %q, want %q", got.RawProfile, tt.wantProfile)
			}
		})
	}
}

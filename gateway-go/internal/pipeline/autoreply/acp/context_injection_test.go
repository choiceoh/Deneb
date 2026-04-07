package acp

import "testing"

func TestParseACPSessionKey(t *testing.T) {
	tests := []struct {
		key        string
		wantParent string
		wantAgent  string
	}{
		{"acp:parent-session:sub_agent123", "parent-session", "sub_agent123"},
		{"acp:telegram:user123:sub_coder", "telegram:user123", "sub_coder"},
		{"acp:nested:deep:key:sub_x", "nested:deep:key", "sub_x"},
		{"regular-session", "", ""},
		{"acp:", "", ""},
		{"acp:only", "", ""},
		{"", "", ""},
	}
	for _, tt := range tests {
		parent, agent := parseACPSessionKey(tt.key)
		if parent != tt.wantParent || agent != tt.wantAgent {
			t.Errorf("parseACPSessionKey(%q) = (%q, %q), want (%q, %q)",
				tt.key, parent, agent, tt.wantParent, tt.wantAgent)
		}
	}
}

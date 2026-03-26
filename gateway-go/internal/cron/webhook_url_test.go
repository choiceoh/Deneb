package cron

import "testing"

func TestNormalizeHTTPWebhookURL(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://example.com/hook", "https://example.com/hook"},
		{"http://localhost:8080/api", "http://localhost:8080/api"},
		{"ftp://example.com", ""},
		{"not-a-url", ""},
		{"", ""},
		{"javascript:alert(1)", ""},
		{"https://", ""},
	}
	for _, tt := range tests {
		got := NormalizeHTTPWebhookURL(tt.input)
		if got != tt.want {
			t.Errorf("NormalizeHTTPWebhookURL(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

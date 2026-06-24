package chat

import (
	"strings"
	"testing"
)

func TestToolStreamDetail(t *testing.T) {
	tests := []struct {
		name  string
		tool  string
		input string
		want  string
	}{
		{"mail archive query", "mail_archive", `{"action":"search","query":"from:argo NDA"}`, "from:argo NDA"},
		{"web prefers query over url", "web", `{"query":"탑솔라 아르고에너지","url":"https://x.com"}`, "탑솔라 아르고에너지"},
		{"web falls back to url", "web", `{"url":"https://news.example.com/a"}`, "https://news.example.com/a"},
		{"exec command", "exec", `{"command":"make check","workdir":"/tmp"}`, "make check"},
		{"read path reduced to base name", "read", `{"file_path":"/home/u/deneb/gateway-go/main.go"}`, "main.go"},
		{"wiki query", "wiki", `{"action":"search","query":"아르고 견적"}`, "아르고 견적"},
		{"whitespace collapsed", "exec", "{\"command\":\"ls \\n  -la\"}", "ls -la"},
		{"uncurated tool", "message", `{"text":"비밀 내용"}`, ""},
		{"unknown tool", "frobnicate", `{"query":"x"}`, ""},
		{"missing keys", "mail_archive", `{"action":"list"}`, ""},
		{"non-string value", "mail_archive", `{"query":42}`, ""},
		{"malformed json", "mail_archive", `{"query":`, ""},
		{"empty input", "mail_archive", ``, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toolStreamDetail(tt.tool, []byte(tt.input))
			if got != tt.want {
				t.Errorf("toolStreamDetail(%s) = %q, want %q", tt.tool, got, tt.want)
			}
		})
	}
}

func TestToolStreamDetailTruncates(t *testing.T) {
	long := strings.Repeat("가", 60)
	got := toolStreamDetail("mail_archive", []byte(`{"query":"`+long+`"}`))
	runes := []rune(got)
	if len(runes) != maxToolDetailRunes+1 || !strings.HasSuffix(got, "…") {
		t.Errorf("len = %d runes (%q tail), want %d + ellipsis", len(runes), got[len(got)-4:], maxToolDetailRunes)
	}
}

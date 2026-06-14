package main

import (
	"net/http"
	"testing"
)

func TestIdentifyClient(t *testing.T) {
	mk := func(ua, hdr string) *http.Request {
		r, _ := http.NewRequest("POST", "/", nil)
		if ua != "" {
			r.Header.Set("User-Agent", ua)
		}
		if hdr != "" {
			r.Header.Set("X-Wormhole-Client", hdr)
		}
		return r
	}
	cases := []struct {
		name     string
		ua, hdr  string
		wantKind clientKind
		wantName string
	}{
		{"explicit header wins over UA", "curl/8.0", "deneb-gateway", clientDeneb, "deneb-gateway"},
		{"deneb UA", "Deneb/4.28 (linux)", "", clientDeneb, "Deneb"},
		{"claude code UA", "claude-code/1.2.0", "", clientClaudeCode, "claude-code"},
		{"openai sdk UA", "OpenAI/Python 1.40.0", "", clientOpenAISDK, "OpenAI"},
		{"anthropic sdk UA", "Anthropic/Python 0.30", "", clientAnthropicSDK, "Anthropic"},
		{"curl UA", "curl/8.5.0", "", clientCurl, "curl"},
		{"empty UA", "", "", clientUnknown, "unknown"},
		{"unknown UA", "MyApp/2.0 extra", "", clientUnknown, "MyApp"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := identifyClient(mk(c.ua, c.hdr))
			if got.kind != c.wantKind {
				t.Errorf("kind = %q, want %q", got.kind, c.wantKind)
			}
			if got.name != c.wantName {
				t.Errorf("name = %q, want %q", got.name, c.wantName)
			}
		})
	}
}

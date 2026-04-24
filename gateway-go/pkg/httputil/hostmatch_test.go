package httputil

import "testing"

func TestHostname(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"whitespace", "   ", ""},
		{"plain https", "https://api.openai.com/v1", "api.openai.com"},
		{"bare host", "api.openai.com", "api.openai.com"},
		{"bare host with port", "api.foo.com:8080", "api.foo.com"},
		{"url with port", "https://api.foo.com:8080/path", "api.foo.com"},
		{"url with query", "https://api.foo.com/v1?q=1&k=2", "api.foo.com"},
		{"trailing dot stripped", "https://api.openai.com./v1", "api.openai.com"},
		{"trailing dot bare", "api.openai.com.", "api.openai.com"},
		{"uppercase normalized", "https://API.OpenAI.COM/v1", "api.openai.com"},
		{"ipv4", "http://192.168.1.1:8080/x", "192.168.1.1"},
		{"ipv6 literal", "http://[::1]:8080/x", "::1"},
		{"ipv6 bare", "[::1]:8080", "::1"},
		// url.Parse returns an error for stray control characters; expect empty.
		{"control chars unparseable", "http://exa\x7fmple.com", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Hostname(tc.in)
			if got != tc.want {
				t.Fatalf("Hostname(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestHostMatches(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		url    string
		domain string
		want   bool
	}{
		// --- Hermes acceptance cases ---
		{"subdomain match", "https://api.moonshot.ai/v1", "moonshot.ai", true},
		{"exact apex", "https://moonshot.ai", "moonshot.ai", true},
		{"path injection blocked", "https://evil.com/moonshot.ai/v1", "moonshot.ai", false},
		{"suffix injection blocked", "https://moonshot.ai.evil/v1", "moonshot.ai", false},

		// --- Additional safety cases ---
		{"openai subdomain", "https://api.openai.com/v1", "openai.com", true},
		{"openai apex only", "https://openai.com", "openai.com", true},
		{"openai path injection", "https://evil.com/api.openai.com/v1", "openai.com", false},
		{"openai suffix injection", "https://api.openai.com.evil", "openai.com", false},
		{"openai subdomain-of-subdomain", "https://v1.api.openai.com/x", "openai.com", true},

		// --- Input forms ---
		{"bare host match", "api.openai.com", "openai.com", true},
		{"bare host with port", "api.foo.com:8080", "foo.com", true},
		{"uppercase url", "https://API.OpenAI.COM/v1", "openai.com", true},
		{"uppercase domain", "https://api.openai.com/v1", "OpenAI.Com", true},
		{"trailing dot url", "https://api.openai.com./v1", "openai.com", true},
		{"trailing dot domain", "https://api.openai.com/v1", "openai.com.", true},
		{"both trailing dots", "https://api.openai.com./v1", "openai.com.", true},

		// --- Empty / malformed ---
		{"empty url", "", "openai.com", false},
		{"empty domain", "https://api.openai.com/v1", "", false},
		{"whitespace url", "   ", "openai.com", false},
		{"whitespace domain", "https://api.openai.com/v1", "   ", false},
		{"both empty", "", "", false},

		// --- Near-miss ---
		{"different tld", "https://api.openai.org/v1", "openai.com", false},
		{"partial suffix", "https://fooopenai.com/v1", "openai.com", false},

		// --- Query / path don't contribute to hostname ---
		{"domain hidden in query", "https://evil.com/v1?host=openai.com", "openai.com", false},
		{"domain hidden in path", "https://evil.com/openai.com/api", "openai.com", false},

		// --- IPv6 ---
		{"ipv6 literal match", "http://[::1]:8080/x", "::1", true},
		{"ipv6 no match", "http://[::1]:8080/x", "localhost", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := HostMatches(tc.url, tc.domain)
			if got != tc.want {
				t.Fatalf("HostMatches(%q, %q) = %v, want %v", tc.url, tc.domain, got, tc.want)
			}
		})
	}
}

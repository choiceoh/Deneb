package configresolve

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRouterMeterClassifiesEntriesByTheirUpstreamHost(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	body := `{
	  "listen": ":18800",
	  "token": "shhh",
	  "models": [
	    {"name": "glm-5.3-flash-local", "url": "http://100.125.220.117:8000/v1"},
	    {"name": "glm-5.3-flash-local-low", "url": "http://10.10.10.2:8000/v1"},
	    {"name": "glm-5.3-flash", "url": "https://api.z.ai/api/coding/paas/v4"},
	    {"name": "k3", "url": "https://api.kimi.com/coding/v1"},
	    {"name": "broken", "url": "%%"}
	  ]
	}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv(routerConfigEnv, path)

	baseURL, token, local := RouterMeter(nil)
	if baseURL != "http://127.0.0.1:18800" {
		t.Errorf("baseURL = %q", baseURL)
	}
	if token != "shhh" {
		t.Errorf("token not resolved")
	}
	if !local["glm-5.3-flash-local"] || !local["glm-5.3-flash-local-low"] {
		t.Errorf("a private/CGNAT upstream must count local: %v", local)
	}
	// A target we cannot classify is not credited to the local engine.
	for _, remote := range []string{"glm-5.3-flash", "k3", "broken"} {
		if local[remote] {
			t.Errorf("%q was counted local", remote)
		}
	}
}

func TestRouterMeterIsEmptyWhenUnavailable(t *testing.T) {
	t.Setenv(routerConfigEnv, filepath.Join(t.TempDir(), "absent.json"))
	if u, tok, l := RouterMeter(nil); u != "" || tok != "" || l != nil {
		t.Errorf("missing config gave %q %q %v", u, tok, l)
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	_ = os.WriteFile(bad, []byte(`{"models": [`), 0o600)
	t.Setenv(routerConfigEnv, bad)
	if u, _, _ := RouterMeter(nil); u != "" {
		t.Errorf("unparseable config gave %q", u)
	}
}

func TestRouterBaseURLReachesAWildcardListenOverLoopback(t *testing.T) {
	for in, want := range map[string]string{
		":18800":          "http://127.0.0.1:18800",
		"0.0.0.0:18800":   "http://127.0.0.1:18800",
		"127.0.0.1:18800": "http://127.0.0.1:18800",
		"":                "",
		"18800":           "",
	} {
		if got := routerBaseURL(in); got != want {
			t.Errorf("routerBaseURL(%q) = %q, want %q", in, got, want)
		}
	}
}

package configresolve

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEngineModelsMatchesEntriesByServerNotPathOrName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	body := `{
	  "models": [
	    {"name": "glm-5.3-flash", "url": "http://100.125.220.117:8000/v1"},
	    {"name": "glm-5.3-flash-low", "url": "http://100.125.220.117:8000/v1/"},
	    {"name": "glm-5.3-flash-low", "url": "http://100.125.220.117:8000/v1"},
	    {"name": "other-engine", "url": "http://100.125.220.117:8001/v1"},
	    {"name": "glm-5.3", "url": "https://api.z.ai/api/coding/paas/v4"},
	    {"name": "broken", "url": "%%"},
	    {"name": "", "url": "http://100.125.220.117:8000/v1"}
	  ]
	}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv(routerConfigEnv, path)

	// Configured as the /metrics URL; the router reaches it at /v1.
	got := strings.Join(EngineModels("http://100.125.220.117:8000/metrics"), ",")
	if got != "glm-5.3-flash,glm-5.3-flash-low" {
		t.Fatalf("EngineModels = %q, want the two entries at that server, sorted and de-duplicated", got)
	}
	// Same server, another port: a different engine.
	if got := strings.Join(EngineModels("http://100.125.220.117:8001/metrics"), ","); got != "other-engine" {
		t.Fatalf("EngineModels(:8001) = %q", got)
	}
	if got := EngineModels("http://10.9.9.9:8000/metrics"); got != nil {
		t.Fatalf("EngineModels(unknown server) = %v, want nil", got)
	}
	if got := EngineModels(""); got != nil {
		t.Fatalf("EngineModels(\"\") = %v, want nil", got)
	}
}

func TestEngineModelsIsNilWithoutAUsableConfig(t *testing.T) {
	t.Setenv(routerConfigEnv, filepath.Join(t.TempDir(), "absent.json"))
	if got := EngineModels("http://10.0.0.5:8000/metrics"); got != nil {
		t.Fatalf("absent config: EngineModels = %v, want nil", got)
	}

	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv(routerConfigEnv, bad)
	if got := EngineModels("http://10.0.0.5:8000/metrics"); got != nil {
		t.Fatalf("unparseable config: EngineModels = %v, want nil", got)
	}
}

package configresolve

import (
	"os"
	"path/filepath"
	"testing"
)

func writeWormholeConfig(t *testing.T, body string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv(wormholeConfigEnv, path)
}

func TestMeteredModelsReadsOnlyTheMeteredEntries(t *testing.T) {
	writeWormholeConfig(t, `{
	  "token": "should-never-be-read-here",
	  "models": [
	    {"name": "glm-5.3-flash-local", "url": "http://10.0.0.1:8000/v1"},
	    {"name": "glm-5.3-flash", "url": "https://example.invalid/v1"},
	    {"name": "deepseek-v4-flash-api", "url": "https://example.invalid/v1", "metered": true},
	    {"name": "deepseek-v4-pro-api", "url": "https://example.invalid/v1", "metered": true}
	  ]
	}`)
	got := MeteredModels(nil)
	if len(got) != 2 || !got["deepseek-v4-flash-api"] || !got["deepseek-v4-pro-api"] {
		t.Fatalf("MeteredModels = %v, want exactly the two metered entries", got)
	}
	if got["glm-5.3-flash-local"] || got["glm-5.3-flash"] {
		t.Errorf("an unmetered entry was marked metered: %v", got)
	}
}

// The guard can only ever REMOVE a fallback candidate. A missing or unparseable
// router config must therefore read as "nothing known to be metered" rather
// than failing closed and stranding every chain.
func TestMeteredModelsIsNilWhenUnavailable(t *testing.T) {
	t.Setenv(wormholeConfigEnv, filepath.Join(t.TempDir(), "absent.json"))
	if got := MeteredModels(nil); got != nil {
		t.Errorf("missing config gave %v, want nil", got)
	}
	writeWormholeConfig(t, `{"models": [`)
	if got := MeteredModels(nil); got != nil {
		t.Errorf("unparseable config gave %v, want nil", got)
	}
	writeWormholeConfig(t, `{"models": [{"name": "a"}, {"name": "b"}]}`)
	if got := MeteredModels(nil); got != nil {
		t.Errorf("no metered entries gave %v, want nil", got)
	}
}

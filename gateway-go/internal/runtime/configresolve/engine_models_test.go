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

// The router keys its table by name and the last entry of a name wins, so a
// name is the engine's only when its last entry points there: a cloud twin
// listed after the local entry takes the name's traffic, and a local entry
// listed after a cloud one takes it back — with the last entry's variant.
func TestEngineEntriesJudgeADuplicatedNameByItsLastEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	body := `{
	  "models": [
	    {"name": "glm-5.3-flash", "url": "http://100.125.220.117:8000/v1"},
	    {"name": "glm-5.3-flash", "url": "https://api.z.ai/api/coding/paas/v4"},
	    {"name": "qwen3.8-flash-next", "url": "https://api.z.ai/api/coding/paas/v4"},
	    {"name": "qwen3.8-flash-next", "url": "http://100.125.220.117:8000/v1", "thinkingMode": "off"},
	    {"name": "qwen3.8-flash-next-low", "url": "http://100.125.220.117:8000/v1", "thinkingMode": "on"},
	    {"name": "qwen3.8-flash-next-low", "url": "http://100.125.220.117:8000/v1", "thinkingMode": "off"}
	  ]
	}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv(routerConfigEnv, path)

	got := EngineEntries("http://100.125.220.117:8000/metrics")
	var names []string
	for _, e := range got {
		names = append(names, e.Name)
		if e.ThinkingMode != "off" {
			t.Errorf("%s: thinkingMode = %q, want the last entry's %q", e.Name, e.ThinkingMode, "off")
		}
	}
	if strings.Join(names, ",") != "qwen3.8-flash-next,qwen3.8-flash-next-low" {
		t.Fatalf("EngineEntries = %q, want the names whose last entry is at the engine (not glm-5.3-flash, now the cloud's)", names)
	}
}

// Routing follows the engine's model by pairing entries of the same variant:
// what each asks the engine for, whether it thinks, and whether it takes images.
func TestEngineEntriesCarryTheVariantEachEntryIs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	body := `{
	  "models": [
	    {"name": "glm-5.3-flash", "url": "http://100.125.220.117:8000/v1", "upstreamModel": "glm-5.3-flash", "thinkingMode": "on", "vision": true},
	    {"name": "glm-5.3-flash-low", "url": "http://100.125.220.117:8000/v1", "upstreamModel": "glm-5.3-flash", "thinkingMode": "off"},
	    {"name": "qwen3.8-flash-next", "url": "http://100.125.220.117:8000/v1", "thinkingMode": "on", "vision": false},
	    {"name": "glm-5.3", "url": "https://api.z.ai/api/coding/paas/v4", "upstreamModel": "glm-5.3"}
	  ]
	}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv(routerConfigEnv, path)

	got := EngineEntries("http://100.125.220.117:8000/metrics")
	if len(got) != 3 {
		t.Fatalf("entries = %+v, want the three at the engine", got)
	}
	byName := map[string]EngineEntry{}
	for _, e := range got {
		byName[e.Name] = e
	}
	if e := byName["glm-5.3-flash-low"]; e.UpstreamModel != "glm-5.3-flash" || e.ThinkingMode != "off" || e.Vision != nil {
		t.Errorf("low variant = %+v", e)
	}
	if e := byName["qwen3.8-flash-next"]; e.UpstreamModel != "qwen3.8-flash-next" || e.Vision == nil || *e.Vision {
		t.Errorf("an entry without upstreamModel asks for its own name, and says it takes no images: %+v", e)
	}
	if e := byName["glm-5.3-flash"]; e.Vision == nil || !*e.Vision || e.ThinkingMode != "on" {
		t.Errorf("thinking variant = %+v", e)
	}
}

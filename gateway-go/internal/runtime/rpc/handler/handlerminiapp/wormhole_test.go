package handlerminiapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestWormholeStatus_ClassifiesAndNeverLeaksKeys(t *testing.T) {
	dir := t.TempDir()
	cfgPath := dir + "/config.json"
	writeWHConfig(t, cfgPath, `{
		"listen":":18800","localOnly":false,"effortRouting":true,"auto":["dsv4"],
		"models":[
			{"name":"dsv4","url":"http://127.0.0.1:8000/v1","toggleKwarg":"thinking"},
			{"name":"claude","url":"https://api.anthropic.com/v1","protocol":"anthropic","key":"secret-key-123"}
		]
	}`)
	health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer health.Close()

	h := WormholeMethods(WormholeDeps{ConfigPath: cfgPath, BaseURL: health.URL})["miniapp.wormhole.status"]
	resp := h(authedCtx(), reqWith(t, "miniapp.wormhole.status", map[string]any{}))

	// The upstream key must never appear anywhere in the response.
	if raw, _ := json.Marshal(resp); strings.Contains(string(raw), "secret-key-123") {
		t.Fatal("upstream key leaked into the wormhole status response")
	}

	var out struct {
		Reachable     bool     `json:"reachable"`
		LocalOnly     bool     `json:"localOnly"`
		EffortRouting bool     `json:"effortRouting"`
		Auto          []string `json:"auto"`
		Models        []struct {
			Name     string `json:"name"`
			Protocol string `json:"protocol"`
			Local    bool   `json:"local"`
			Thinking bool   `json:"thinking"`
		} `json:"models"`
	}
	decode(t, resp, &out)
	if !out.Reachable {
		t.Error("reachable should be true (health returned 200)")
	}
	if !out.EffortRouting || out.LocalOnly {
		t.Errorf("flags: effortRouting=%v localOnly=%v, want true/false", out.EffortRouting, out.LocalOnly)
	}
	if len(out.Models) != 2 {
		t.Fatalf("models = %d, want 2", len(out.Models))
	}
	if out.Models[0].Name != "dsv4" || !out.Models[0].Local || !out.Models[0].Thinking {
		t.Errorf("dsv4 row wrong: %+v (want local + thinking)", out.Models[0])
	}
	if out.Models[1].Name != "claude" || out.Models[1].Local || out.Models[1].Protocol != "anthropic" {
		t.Errorf("claude row wrong: %+v (want cloud + anthropic)", out.Models[1])
	}
}

func TestWormholeSetFeature_WritesFlagAndPreservesKeys(t *testing.T) {
	dir := t.TempDir()
	cfgPath := dir + "/config.json"
	writeWHConfig(t, cfgPath, `{"localOnly":false,"models":[{"name":"claude","url":"https://x/v1","key":"sekret"}]}`)

	h := WormholeMethods(WormholeDeps{ConfigPath: cfgPath})["miniapp.wormhole.set_feature"]
	resp := h(authedCtx(), reqWith(t, "miniapp.wormhole.set_feature", map[string]any{"feature": "localOnly", "enabled": true}))
	if !resp.OK {
		t.Fatalf("set_feature failed: %+v", resp)
	}

	b, _ := os.ReadFile(cfgPath)
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m["localOnly"] != true {
		t.Error("localOnly was not written to the config")
	}
	models, _ := m["models"].([]any)
	if len(models) != 1 {
		t.Fatalf("models lost on rewrite: %v", m["models"])
	}
	if m0, _ := models[0].(map[string]any); m0["key"] != "sekret" {
		t.Error("upstream key was NOT preserved through set_feature")
	}
}

func TestWormholeSetFeature_RejectsUnknownFeature(t *testing.T) {
	dir := t.TempDir()
	cfgPath := dir + "/config.json"
	writeWHConfig(t, cfgPath, `{}`)
	h := WormholeMethods(WormholeDeps{ConfigPath: cfgPath})["miniapp.wormhole.set_feature"]
	resp := h(authedCtx(), reqWith(t, "miniapp.wormhole.set_feature", map[string]any{"feature": "evil", "enabled": true}))
	if resp.OK {
		t.Error("an unknown feature must be rejected")
	}
}

func TestModelIsLocal(t *testing.T) {
	f, tr := false, true
	if modelIsLocal(&f, "http://127.0.0.1/v1") {
		t.Error("explicit override false should win over a loopback URL")
	}
	if !modelIsLocal(&tr, "https://api.anthropic.com") {
		t.Error("explicit override true should win over a public URL")
	}
	if !modelIsLocal(nil, "http://127.0.0.1:8000/v1") {
		t.Error("loopback URL should auto-detect as local")
	}
	if modelIsLocal(nil, "https://api.anthropic.com/v1") {
		t.Error("public URL should auto-detect as cloud")
	}
}

func writeWHConfig(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

package handlerminiapp

import (
	"encoding/json"
	"io"
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

func TestWormholeStatus_LiveSurfacesFleetModels(t *testing.T) {
	dir := t.TempDir()
	cfgPath := dir + "/config.json"
	writeWHConfig(t, cfgPath, `{"token":"wh-sekret-9z","models":[{"name":"dsv4","url":"http://127.0.0.1:8000/v1","toggleKwarg":"thinking"}]}`)

	// Fake wormhole whose /status is token-gated and returns one configured + one
	// discovered (fleet) model — the discovered one only exists in the live view.
	wh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/status" {
			if r.Header.Get("Authorization") != "Bearer wh-sekret-9z" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = io.WriteString(w, `{"listen":":18800","localOnly":false,"effortRouting":true,"auto":["dsv4"],
				"models":[
					{"name":"dsv4","protocol":"openai","local":true,"thinking":true,"source":"config"},
					{"name":"qwen","protocol":"openai","local":true,"thinking":false,"source":"fleet"}
				]}`)
			return
		}
		w.WriteHeader(http.StatusOK) // /health
	}))
	defer wh.Close()

	h := WormholeMethods(WormholeDeps{ConfigPath: cfgPath, BaseURL: wh.URL})["miniapp.wormhole.status"]
	resp := h(authedCtx(), reqWith(t, "miniapp.wormhole.status", map[string]any{}))

	// The wormhole token authenticates the gateway→wormhole hop only — it must
	// never appear in the response sent to the native client.
	if raw, _ := json.Marshal(resp); strings.Contains(string(raw), "wh-sekret-9z") {
		t.Fatal("wormhole token leaked into the status response")
	}

	var out struct {
		Reachable bool `json:"reachable"`
		Models    []struct {
			Name   string `json:"name"`
			Source string `json:"source"`
			Local  bool   `json:"local"`
		} `json:"models"`
	}
	decode(t, resp, &out)
	if !out.Reachable {
		t.Error("reachable should be true when /status answers")
	}
	if len(out.Models) != 2 {
		t.Fatalf("models = %d, want 2 (configured + discovered): %+v", len(out.Models), out.Models)
	}
	src := map[string]string{}
	for _, m := range out.Models {
		src[m.Name] = m.Source
	}
	if src["dsv4"] != "config" || src["qwen"] != "fleet" {
		t.Errorf("source labels wrong: %+v (want dsv4=config, qwen=fleet)", src)
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

func TestWormholeModelKeyVar(t *testing.T) {
	dir := t.TempDir()
	cfgPath := dir + "/config.json"
	writeWHConfig(t, cfgPath, `{"models":[
		{"name":"glm","url":"https://api.z.ai/v1","key":"${ZAI_API_KEY}"},
		{"name":"lit","url":"https://x/v1","key":"literal-secret-xyz"},
		{"name":"local","url":"http://127.0.0.1:8000/v1"}
	]}`)
	if v, err := wormholeModelKeyVar(cfgPath, "glm"); err != nil || v != "ZAI_API_KEY" {
		t.Errorf("glm: var=%q err=%v, want ZAI_API_KEY,nil", v, err)
	}
	if _, err := wormholeModelKeyVar(cfgPath, "lit"); err == nil {
		t.Error("a literal key must be rejected (not rotatable via secrets.env)")
	} else if strings.Contains(err.Error(), "literal-secret-xyz") {
		t.Error("error message leaked the literal key value")
	}
	if _, err := wormholeModelKeyVar(cfgPath, "local"); err == nil {
		t.Error("a keyless model must be rejected")
	}
	if _, err := wormholeModelKeyVar(cfgPath, "nope"); err == nil {
		t.Error("a missing model must be rejected")
	}
}

func TestUpsertSecretLine(t *testing.T) {
	dir := t.TempDir()
	p := dir + "/secrets.env"
	if err := upsertSecretLine(p, "MIMO_KEY", "one"); err != nil {
		t.Fatal(err)
	}
	cur, _ := os.ReadFile(p)
	if err := os.WriteFile(p, []byte("# header\n"+string(cur)+"ZAI_API_KEY=keepme\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := upsertSecretLine(p, "MIMO_KEY", "two"); err != nil { // replace, preserve rest
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	s := string(b)
	if !strings.Contains(s, "MIMO_KEY=two") || strings.Contains(s, "MIMO_KEY=one") {
		t.Errorf("MIMO_KEY not replaced: %q", s)
	}
	if !strings.Contains(s, "# header") || !strings.Contains(s, "ZAI_API_KEY=keepme") {
		t.Errorf("other lines/comments not preserved: %q", s)
	}
	if info, _ := os.Stat(p); info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestWormholeSetKey_ErrorPathsNeverLeakOrWrite(t *testing.T) {
	dir := t.TempDir()
	cfgPath := dir + "/config.json"
	writeWHConfig(t, cfgPath, `{"models":[{"name":"lit","url":"https://x/v1","key":"literal-secret-xyz"}]}`)
	h := WormholeMethods(WormholeDeps{ConfigPath: cfgPath})["miniapp.wormhole.set_key"]

	// A literal-key model is rejected (error path — never reaches validation), and
	// the old literal must not appear in the response.
	resp := h(authedCtx(), reqWith(t, "miniapp.wormhole.set_key", map[string]any{"model": "lit", "key": "new"}))
	if resp.OK {
		t.Error("rotating a literal-key model must be rejected")
	}
	if raw, _ := json.Marshal(resp); strings.Contains(string(raw), "literal-secret-xyz") {
		t.Error("error response leaked the literal key value")
	}
	if _, err := os.Stat(dir + "/secrets.env"); err == nil {
		t.Error("secrets.env written despite a rejected rotation")
	}
	if h(authedCtx(), reqWith(t, "miniapp.wormhole.set_key", map[string]any{"model": "ghost", "key": "k"})).OK {
		t.Error("a missing model must be rejected")
	}
	if h(authedCtx(), reqWith(t, "miniapp.wormhole.set_key", map[string]any{"model": "lit"})).OK {
		t.Error("an empty key must be rejected")
	}
}

func writeWHConfig(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

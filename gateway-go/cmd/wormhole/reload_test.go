package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func writeFileT(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestConfigHotReload(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/wormhole.json"
	writeFileT(t, path, `{"localOnly":false,"models":[{"name":"a","url":"http://127.0.0.1/v1"}]}`)
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	rt := newRouter(cfg, path, quietLog())
	if rt.cur().cfg.LocalOnly {
		t.Fatal("initial localOnly should be false")
	}

	// Flip localOnly true and bump mtime so the watcher's mtime check fires.
	writeFileT(t, path, `{"localOnly":true,"models":[{"name":"a","url":"http://127.0.0.1/v1"}]}`)
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}

	if !rt.reloadIfChanged() {
		t.Fatal("expected a reload after the file changed")
	}
	if !rt.cur().cfg.LocalOnly {
		t.Error("localOnly should be true after the hot-reload — the toggle didn't apply live")
	}
}

func TestEffortRoutingOn(t *testing.T) {
	if !(config{}).effortRoutingOn() {
		t.Error("absent effortRouting should default ON")
	}
	f := false
	if (config{EffortRouting: &f}).effortRoutingOn() {
		t.Error("explicit false should be OFF")
	}
	tr := true
	if !(config{EffortRouting: &tr}).effortRoutingOn() {
		t.Error("explicit true should be ON")
	}
}

func TestEffortRoutingDisabled_SuppressesInjection(t *testing.T) {
	var gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = io.WriteString(w, "ok")
	}))
	defer upstream.Close()

	off := false
	rt := quietRouter(config{
		EffortRouting: &off,
		Models:        []modelEntry{{Name: "dsv4", URL: upstream.URL + "/v1", ToggleKwarg: "thinking", UpstreamModel: "dsv4"}},
	})
	srv := httptest.NewServer(rt.handler())
	defer srv.Close()

	_, _ = http.Post(srv.URL+"/v1/chat/completions", "application/json",
		strings.NewReader(`{"model":"dsv4","messages":[{"role":"user","content":"hi"}]}`))
	if strings.Contains(gotBody, "chat_template_kwargs") {
		t.Errorf("effortRouting:false should suppress thinking injection, upstream saw: %s", gotBody)
	}
}

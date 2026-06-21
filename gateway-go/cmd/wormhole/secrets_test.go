package main

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSecretsFileFor(t *testing.T) {
	if got := secretsFileFor(""); got != "" {
		t.Errorf("empty config path: got %q, want \"\"", got)
	}
	if got := secretsFileFor("/home/u/.wormhole/config.json"); got != "/home/u/.wormhole/secrets.env" {
		t.Errorf("got %q, want /home/u/.wormhole/secrets.env", got)
	}
}

func TestLoadSecretsEnv(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "secrets.env")
	content := "# a comment\n\n" +
		"WH_TEST_A=alpha\n" +
		"  WH_TEST_B = beta \n" + // spaces around key and =
		"WH_TEST_C=\"quoted val\"\n" + // double quotes trimmed
		"WH_TEST_D='single'\n" + // single quotes trimmed
		"notakeyline\n" + // no '=' → skipped
		"=novalue\n" // empty key → skipped
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"WH_TEST_A", "WH_TEST_B", "WH_TEST_C", "WH_TEST_D"} {
		t.Cleanup(func() { _ = os.Unsetenv(k) })
	}

	n, err := loadSecretsEnv(p)
	if err != nil {
		t.Fatalf("loadSecretsEnv: %v", err)
	}
	if n != 4 {
		t.Errorf("loaded %d keys, want 4", n)
	}
	want := map[string]string{
		"WH_TEST_A": "alpha",
		"WH_TEST_B": "beta",
		"WH_TEST_C": "quoted val",
		"WH_TEST_D": "single",
	}
	for k, v := range want {
		if got := os.Getenv(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
}

func TestLoadSecretsEnv_MissingIsNoOp(t *testing.T) {
	if n, err := loadSecretsEnv(filepath.Join(t.TempDir(), "absent.env")); err != nil || n != 0 {
		t.Errorf("absent file: n=%d err=%v, want 0,nil", n, err)
	}
	if n, err := loadSecretsEnv(""); err != nil || n != 0 {
		t.Errorf("empty path: n=%d err=%v, want 0,nil", n, err)
	}
}

// TestReloadIfChanged_SecretsRotation proves the live-rotation mechanism: editing
// only the secrets file (config untouched) makes reloadIfChanged re-read secrets and
// re-expand ${VAR} refs — no restart. This is what lets the gateway rotate a dead key.
func TestReloadIfChanged_SecretsRotation(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	secPath := filepath.Join(dir, "secrets.env")
	if err := os.WriteFile(cfgPath, []byte(`{"listen":":0","models":[{"name":"m","url":"https://api.example.com/v1","key":"${WH_ROT_KEY}"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secPath, []byte("WH_ROT_KEY=key-one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Unsetenv("WH_ROT_KEY") })

	if _, err := loadSecretsEnv(secPath); err != nil { // simulate boot load
		t.Fatal(err)
	}
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	rt := newRouter(cfg, cfgPath, slog.New(slog.NewTextHandler(io.Discard, nil)))
	rt.reloadIfChanged() // first call adopts the real config mtime (newRouter seeds it to zero)

	if e, ok := rt.lookup("m"); !ok || e.Key != "key-one" {
		t.Fatalf("initial key = %q (ok=%v), want key-one", e.Key, ok)
	}

	// Rotate the secret; bump mtime explicitly (a fast FS may reuse the modtime).
	if err := os.WriteFile(secPath, []byte("WH_ROT_KEY=key-two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	_ = os.Chtimes(secPath, future, future)

	if !rt.reloadIfChanged() {
		t.Fatal("reloadIfChanged returned false after a secrets-only edit")
	}
	if e, ok := rt.lookup("m"); !ok || e.Key != "key-two" {
		t.Errorf("after rotation key = %q (ok=%v), want key-two — secrets edit did not re-expand", e.Key, ok)
	}
}

func TestSecretsMtimeNanos(t *testing.T) {
	if got := secretsMtimeNanos(""); got != 0 {
		t.Errorf("empty path: got %d, want 0", got)
	}
	if got := secretsMtimeNanos(filepath.Join(t.TempDir(), "absent.env")); got != 0 {
		t.Errorf("absent file: got %d, want 0", got)
	}
	p := filepath.Join(t.TempDir(), "secrets.env")
	if err := os.WriteFile(p, []byte("K=v\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := secretsMtimeNanos(p); got == 0 {
		t.Error("existing file: got 0, want nonzero modtime")
	}
}

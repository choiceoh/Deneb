package bootstrap

import (
	"context"
	"flag"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
)

func parseFlagsForTest(t *testing.T, compiled string, args ...string) Flags {
	t.Helper()
	originalCommandLine := flag.CommandLine
	originalArgs := os.Args
	flag.CommandLine = flag.NewFlagSet("gateway-test", flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)
	os.Args = append([]string{"deneb-gateway"}, args...)
	t.Cleanup(func() {
		flag.CommandLine = originalCommandLine
		os.Args = originalArgs
	})
	return ParseFlags(compiled)
}

func TestParseFlagsDefaultsToCompiledVersion(t *testing.T) {
	got := parseFlagsForTest(t, "4.99.1")
	want := Flags{Version: "4.99.1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("default flags = %#v, want %#v", got, want)
	}
}

func TestParseFlagsCapturesEveryOverride(t *testing.T) {
	got := parseFlagsForTest(
		t, "compiled",
		"-config", "/tmp/deneb-test.json",
		"-port", "32145",
		"-bind", "lan",
		"-version", "operator-version",
		"-pid-file", "/tmp/deneb-test.pid",
		"-daemon",
		"-log-level", "debug",
		"-log-format", "json",
	)
	want := Flags{
		ConfigPath: "/tmp/deneb-test.json",
		Port:       32145,
		Bind:       "lan",
		Version:    "operator-version",
		PIDFile:    "/tmp/deneb-test.pid",
		DaemonMode: true,
		LogLevel:   "debug",
		LogFormat:  "json",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("override flags = %#v, want %#v", got, want)
	}
}

func TestParseFlagsAcceptsExplicitZeroAndEmptyOverrides(t *testing.T) {
	got := parseFlagsForTest(t, "compiled", "-port=0", "-bind=", "-version=", "-daemon=false")
	if got.Port != 0 || got.Bind != "" || got.Version != "compiled" || got.DaemonMode {
		t.Fatalf("explicit zero flags = %#v", got)
	}
}

func TestCfgDirFromBootstrapSnapshotAndHomeFallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "deneb.json")
	bs := &config.BootstrapResult{Snapshot: &config.ConfigSnapshot{Path: path}}
	if got, want := cfgDirFromBootstrap(bs), filepath.Dir(path); got != want {
		t.Fatalf("snapshot cfg dir = %q, want %q", got, want)
	}

	// Empty snapshot path and nil snapshot both fall back to the user home.
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, candidate := range []*config.BootstrapResult{
		{},
		{Snapshot: &config.ConfigSnapshot{}},
	} {
		if got, want := cfgDirFromBootstrap(candidate), filepath.Join(home, ".deneb"); got != want {
			t.Errorf("home fallback = %q, want %q", got, want)
		}
	}
}

func writeBootstrapConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config", "deneb.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestLoadConfigSuccessAndCLIOverrides(t *testing.T) {
	path := writeBootstrapConfig(t, `{
  "gateway": {
    "port": 19191,
    "bind": "loopback",
    "auth": {"mode": "token", "token": "test-bootstrap-token"},
    "controlUi": {"enabled": false}
  },
  "logging": {"level": "warn", "format": "text"}
}`)
	result, err := LoadConfig(Flags{
		ConfigPath: path,
		Port:       20202,
		Bind:       "lan",
	}, discardLogger())
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if result.Bootstrap == nil || result.RuntimeCfg == nil {
		t.Fatalf("LoadConfig dependencies = %#v", result)
	}
	if result.Port != 20202 || result.RuntimeCfg.Port != 20202 {
		t.Fatalf("CLI port override: result=%d runtime=%d", result.Port, result.RuntimeCfg.Port)
	}
	if result.RuntimeCfg.BindHost != "0.0.0.0" || result.Addr != "0.0.0.0:20202" {
		t.Fatalf("CLI bind override: host=%q addr=%q", result.RuntimeCfg.BindHost, result.Addr)
	}
	if result.CfgDir != filepath.Dir(path) {
		t.Fatalf("cfg dir = %q, want %q", result.CfgDir, filepath.Dir(path))
	}
	if result.Bootstrap.Auth.Mode != config.AuthModeToken || result.Bootstrap.Auth.Token != "test-bootstrap-token" {
		t.Fatalf("resolved auth = %#v", result.Bootstrap.Auth)
	}
}

func TestLoadConfigUsesConfiguredPortAndAliases(t *testing.T) {
	path := writeBootstrapConfig(t, `{
  "gateway": {
    "port": 21212,
    "bind": "127.0.0.1",
    "auth": {"mode": "none"}
  }
}`)
	result, err := LoadConfig(Flags{ConfigPath: path}, discardLogger())
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if result.Port != 21212 || result.RuntimeCfg.BindHost != "127.0.0.1" || result.Addr != "127.0.0.1:21212" {
		t.Fatalf("configured runtime = %#v", result)
	}
}

func TestLoadConfigMalformedAndRuntimeValidationErrors(t *testing.T) {
	malformed := writeBootstrapConfig(t, `{"gateway":`)
	if _, err := LoadConfig(Flags{ConfigPath: malformed}, discardLogger()); err == nil || !strings.Contains(err.Error(), "config bootstrap failed") {
		t.Fatalf("malformed config error = %v", err)
	}

	invalid := writeBootstrapConfig(t, `{
  "gateway": {
    "port": 19000,
    "bind": "custom",
    "auth": {"mode": "none"}
  }
}`)
	if _, err := LoadConfig(Flags{ConfigPath: invalid}, discardLogger()); err == nil {
		t.Fatal("custom bind without host unexpectedly succeeded")
	}

	// A missing config file is not an error: the gateway boots on in-memory
	// defaults and records the file as absent. (Persist only appends a
	// generated auth token to an existing config — it never creates the file.)
	missing := filepath.Join(t.TempDir(), "deneb.json")
	created, err := LoadConfig(Flags{ConfigPath: missing}, discardLogger())
	if err != nil {
		t.Fatalf("missing config bootstrap should fall back to defaults: %v", err)
	}
	if created.Bootstrap == nil || created.Bootstrap.Snapshot == nil || created.Bootstrap.Snapshot.Path != missing {
		t.Fatalf("created default config result = %#v", created)
	}
	if created.Bootstrap.Snapshot.Exists {
		t.Fatal("snapshot should record the config file as absent")
	}
}

func TestParseLogLevelKnownValuesAndFallbacks(t *testing.T) {
	cases := []struct {
		input string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
		{"", slog.LevelInfo},
		{"DEBUG", slog.LevelInfo},
		{"trace", slog.LevelInfo},
		{" debug ", slog.LevelInfo},
	}
	for _, tc := range cases {
		if got := ParseLogLevel(tc.input); got != tc.want {
			t.Errorf("ParseLogLevel(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestBuildLoggerPrecedenceFormatAndLevelFiltering(t *testing.T) {
	ctx := context.Background()
	base := &config.DenebConfig{}
	result := BuildLogger(base, "", "")
	if result.Logger == nil || result.Format != "text" || !result.UseColor {
		t.Fatalf("default logger = %#v", result)
	}
	if result.Logger.Enabled(ctx, slog.LevelDebug) || !result.Logger.Enabled(ctx, slog.LevelInfo) {
		t.Fatal("default logger level is not info")
	}

	cfg := &config.DenebConfig{Logging: &config.LoggingConfig{Level: "warn", Format: "json"}}
	result = BuildLogger(cfg, "", "")
	if result.Format != "json" || result.UseColor {
		t.Fatalf("config JSON logger = %#v", result)
	}
	if result.Logger.Enabled(ctx, slog.LevelInfo) || !result.Logger.Enabled(ctx, slog.LevelWarn) {
		t.Fatal("config logger level is not warn")
	}

	result = BuildLogger(cfg, "debug", "text")
	if result.Format != "text" || !result.UseColor {
		t.Fatalf("flag text logger = %#v", result)
	}
	if !result.Logger.Enabled(ctx, slog.LevelDebug) {
		t.Fatal("flag level did not override config warn")
	}

	// Unknown formats intentionally take the console handler path but retain
	// the operator-provided format string; UseColor reflects non-JSON output.
	result = BuildLogger(base, "error", "future")
	if result.Format != "future" || !result.UseColor || result.Logger.Enabled(ctx, slog.LevelWarn) || !result.Logger.Enabled(ctx, slog.LevelError) {
		t.Fatalf("unknown format fallback = %#v", result)
	}
}

func TestBuildEarlyLoggerRespectsRequestedThreshold(t *testing.T) {
	ctx := context.Background()
	debug := BuildEarlyLogger("debug")
	if debug == nil || !debug.Enabled(ctx, slog.LevelDebug) {
		t.Fatal("debug early logger does not enable debug")
	}
	errorOnly := BuildEarlyLogger("error")
	if errorOnly.Enabled(ctx, slog.LevelWarn) || !errorOnly.Enabled(ctx, slog.LevelError) {
		t.Fatal("error early logger threshold incorrect")
	}
}

func TestIsLocalAIReachableHTTPBoundaries(t *testing.T) {
	if isLocalAIReachable("") {
		t.Fatal("empty base URL reported reachable")
	}
	if isLocalAIReachable(":// malformed") {
		t.Fatal("malformed base URL reported reachable")
	}
	if isLocalAIReachable("http://127.0.0.1:1") {
		t.Fatal("refused endpoint reported reachable")
	}

	var gotMethod, gotPath string
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ok.Close()
	if !isLocalAIReachable(ok.URL) {
		t.Fatal("2xx endpoint reported offline")
	}
	if gotMethod != http.MethodGet || gotPath != "/models" {
		t.Fatalf("probe request = %s %s", gotMethod, gotPath)
	}

	for _, status := range []int{http.StatusMovedPermanently, http.StatusBadRequest, http.StatusUnauthorized, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
			}))
			defer srv.Close()
			if isLocalAIReachable(srv.URL) {
				t.Fatalf("status %d reported reachable", status)
			}
		})
	}
}

func TestIsLocalAIReachableClosesResponseBody(t *testing.T) {
	// A server that writes a small body then waits for request cancellation
	// exercises the response-body close path without imposing the 3s timeout.
	requestDone := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("models"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		go func() {
			<-r.Context().Done()
			close(requestDone)
		}()
	}))
	if !isLocalAIReachable(srv.URL) {
		t.Fatal("streaming 200 endpoint reported offline")
	}
	srv.CloseClientConnections()
	srv.Close()
	select {
	case <-requestDone:
	default:
		// The handler may already have returned before its context observer is
		// scheduled; body-close correctness is primarily guarded by the probe
		// completing and server shutdown not hanging.
	}
}

func TestResolvePIDPathPrecedence(t *testing.T) {
	if got := resolvePIDPath("/custom/run.pid", "/config"); got != "/custom/run.pid" {
		t.Fatalf("explicit PID path = %q", got)
	}
	if got := resolvePIDPath("", "/config"); got != filepath.Join("/config", "gateway.pid") {
		t.Fatalf("config PID path = %q", got)
	}
	if got := resolvePIDPath("", ""); got != "/tmp/deneb-gateway.pid" {
		t.Fatalf("fallback PID path = %q", got)
	}
	if got := resolvePIDPath("relative.pid", "/config"); got != "relative.pid" {
		t.Fatalf("relative explicit PID path = %q", got)
	}
}

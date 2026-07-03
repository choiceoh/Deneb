package server

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
)

// TestProviderCatalog_ExpandsEnvRefs pins the ${ENV} expansion contract for the
// registry path: deneb.json keeps provider secrets as ${VAR} references, the
// chat path expands them at call time (run_provider.go), but registry consumers
// (buildClient → llm.NewClient) never expand — so providerCatalog must hand the
// registry RESOLVED values. Regression guard for the wormhole-fronted kimi
// setup where the literal "${WORMHOLE_TOKEN}" was sent as the bearer token and,
// being non-empty, also suppressed the kimi OAuth-token fallback in buildClient.
func TestProviderCatalog_ExpandsEnvRefs(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	writeConfig := func(t *testing.T) {
		t.Helper()
		path := filepath.Join(t.TempDir(), "deneb.json")
		body := `{
  "models": {
    "providers": {
      "kimi": {"baseUrl": "http://127.0.0.1:18800", "apiKey": "${DENEB_TEST_WORMHOLE_TOKEN}"},
      "vllm": {"baseUrl": "http://127.0.0.1:1/v1"}
    }
  }
}`
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}
		t.Setenv("DENEB_CONFIG_PATH", path)
	}
	newRegistry := func(cat map[string]modelrole.ProviderResolved) *modelrole.Registry {
		// The vllm catalog entry's unroutable loopback port keeps registry
		// construction hermetic (default roles probe vLLM discovery; port 1
		// fails instantly instead of dialing a live server).
		return modelrole.NewRegistryWithOptions(logger, modelrole.RegistryOptions{
			MainModel:   "zai/main-model",
			CodingModel: "kimi/kimi-for-coding",
			Providers:   cat,
		})
	}

	t.Run("set env: expanded key reaches ModelConfig", func(t *testing.T) {
		writeConfig(t)
		t.Setenv("DENEB_TEST_WORMHOLE_TOKEN", "wh-secret")

		cat := providerCatalog(logger)
		if got := cat["kimi"].APIKey; got != "wh-secret" {
			t.Fatalf("catalog apiKey = %q, want the expanded env value", got)
		}
		if got := newRegistry(cat).Config(modelrole.RoleCoding).APIKey; got != "wh-secret" {
			t.Errorf("ModelConfig apiKey = %q, want the expanded env value", got)
		}
	})

	t.Run("unset env: empty key keeps the kimi env-callback path enabled", func(t *testing.T) {
		writeConfig(t)
		// ${DENEB_TEST_WORMHOLE_TOKEN} unset → expands to "". KIMI_API_KEY is
		// forced empty so resolveAPIKey's env fallback stays out of the way.
		t.Setenv("DENEB_TEST_WORMHOLE_TOKEN", "")
		t.Setenv("KIMI_API_KEY", "")

		cat := providerCatalog(logger)
		if got := cat["kimi"].APIKey; got != "" {
			t.Fatalf("catalog apiKey = %q, want empty after expansion", got)
		}
		// buildClient installs the kimi OAuth-token callback only when the
		// resolved key is empty — a literal "${…}" here would disable it.
		if got := newRegistry(cat).Config(modelrole.RoleCoding).APIKey; got != "" {
			t.Errorf("ModelConfig apiKey = %q, want empty (enables WithAPIKeyFunc(kimiToken))", got)
		}
	})
}

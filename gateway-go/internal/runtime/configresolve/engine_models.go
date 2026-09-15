package configresolve

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
)

// EngineModels returns the router entries whose upstream is the serving engine
// at engineURL — the models that engine answers for when requests reach it
// through the router.
//
// Matched on host and port, never on the path or the name: the engine is
// configured by its /metrics URL while the router reaches it at /v1, and a
// model's name does not say where it runs (the local engine and its cloud twin
// have answered under the same one).
//
// Nil when the config is absent, unreadable, or lists nothing at that server.
// A liveness gate fed nil can only do nothing, which is the behaviour without
// one. It does not log: the watcher asks on every probe while an engine is
// down, and a broken router config is reported where it is loaded.
func EngineModels(engineURL string) []string {
	target := httputil.HostPort(engineURL)
	if target == "" {
		return nil
	}
	path := strings.TrimSpace(os.Getenv(routerConfigEnv))
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		path = filepath.Join(home, ".wormhole", "config.json")
	}
	raw, err := os.ReadFile(path) //nolint:gosec // operator-owned router config, path from env or $HOME
	if err != nil {
		return nil
	}
	var cfg struct {
		Models []struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"models"`
	}
	if json.Unmarshal(raw, &cfg) != nil {
		return nil
	}
	seen := make(map[string]bool)
	var out []string
	for _, m := range cfg.Models {
		name := strings.TrimSpace(m.Name)
		if name == "" || seen[name] || httputil.HostPort(m.URL) != target {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

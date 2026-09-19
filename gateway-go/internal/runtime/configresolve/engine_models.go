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
	var out []string
	for _, e := range EngineEntries(engineURL) {
		out = append(out, e.Name)
	}
	return out
}

// EngineEntry is one router entry that reaches the serving engine: what the
// router answers to (Name), what it asks the engine for (UpstreamModel), and
// the variant it is — its thinking mode and whether it passes images.
type EngineEntry struct {
	Name          string
	UpstreamModel string
	ThinkingMode  string
	// Vision is the entry's declared image capability; nil when it declares
	// none (the router then decides from its builtin table).
	Vision *bool
}

// EngineEntries returns the router entries whose upstream is the serving
// engine at engineURL, sorted by name. The same matching as EngineModels: host
// and port, never the path or the name. A name listed twice is judged by its
// last entry, because that is the one the router sends it to (wormhole keys its
// table by name, last entry winning): a cloud twin listed after the local entry
// of the same name takes that name's traffic, so the name is not the engine's.
// Nil when the config is absent, unreadable, or lists nothing at that server.
func EngineEntries(engineURL string) []EngineEntry {
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
			Name          string `json:"name"`
			URL           string `json:"url"`
			UpstreamModel string `json:"upstreamModel"`
			ThinkingMode  string `json:"thinkingMode"`
			Vision        *bool  `json:"vision"`
		} `json:"models"`
	}
	if json.Unmarshal(raw, &cfg) != nil {
		return nil
	}
	last := make(map[string]int, len(cfg.Models))
	for i, m := range cfg.Models {
		last[strings.TrimSpace(m.Name)] = i
	}
	var out []EngineEntry
	for i, m := range cfg.Models {
		name := strings.TrimSpace(m.Name)
		if name == "" || last[name] != i || httputil.HostPort(m.URL) != target {
			continue
		}
		upstream := strings.TrimSpace(m.UpstreamModel)
		if upstream == "" {
			upstream = name // the router asks for the entry's own name (wormhole upstreamModelOf)
		}
		out = append(out, EngineEntry{Name: name, UpstreamModel: upstream, ThinkingMode: strings.TrimSpace(m.ThinkingMode), Vision: m.Vision})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

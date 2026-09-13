package configresolve

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// wormholeConfigEnv lets a test or a second deployment point the metered lookup
// at another router config. Empty → ~/.wormhole/config.json, the same file the
// miniapp wormhole surface reads.
const wormholeConfigEnv = "DENEB_WORMHOLE_CONFIG"

// MeteredModels returns the set of model names the wormhole router marks
// pay-per-token.
//
// Why the gateway needs this: wormhole refuses to let an unmetered entry's
// failover chain reach a metered one (cmd/wormhole/validate.go), and that guard
// is the reason the 2026-08 leak — a dead local model quietly billing 1,346
// calls over twelve days through DeepSeek's cloud API — cannot repeat on the
// routed path. Pointing a role straight at the serving engine moves the
// failover decision INTO the gateway and leaves that guard behind. So the
// gateway reads the same flag from the same file rather than keeping a second
// copy that can drift.
//
// Returns nil when the file is missing or unreadable. A nil set means "nothing
// is known to be metered", which is the pre-existing behaviour: the guard can
// only ever remove a candidate, never invent one.
func MeteredModels(logger *slog.Logger) map[string]bool {
	path := strings.TrimSpace(os.Getenv(wormholeConfigEnv))
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
			Name    string `json:"name"`
			Metered bool   `json:"metered"`
		} `json:"models"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		if logger != nil {
			logger.Warn("wormhole config unparseable; metered-fallback guard is off", "path", path, "error", err)
		}
		return nil
	}
	out := make(map[string]bool)
	for _, m := range cfg.Models {
		if name := strings.TrimSpace(m.Name); name != "" && m.Metered {
			out[name] = true
		}
	}
	if len(out) == 0 {
		return nil
	}
	if logger != nil {
		names := make([]string, 0, len(out))
		for n := range out {
			names = append(names, n)
		}
		logger.Info("metered models registered; fallback chains will skip them", "models", strings.Join(names, ","))
	}
	return out
}

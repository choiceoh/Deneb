package plugin

import (
	"strconv"
	"strings"
	"time"
)

func (d *PluginDiscoverer) resolveCacheTTL(env map[string]string) time.Duration {
	if env != nil {
		if _, ok := env["DENEB_DISABLE_PLUGIN_DISCOVERY_CACHE"]; ok {
			return 0
		}
		if raw, ok := env["DENEB_PLUGIN_DISCOVERY_CACHE_MS"]; ok {
			raw = strings.TrimSpace(raw)
			if raw == "" || raw == "0" {
				return 0
			}
			if ms, err := strconv.Atoi(raw); err == nil && ms > 0 {
				return time.Duration(ms) * time.Millisecond
			}
		}
	}
	return defaultDiscoveryCacheMs * time.Millisecond
}

// Wire I/O counter helpers for the monitoring.wire_stats RPC method.
package metrics

// WireStat holds aggregated counters for a single wire.
type WireStat struct {
	Wire      string `json:"wire"`
	CallsOK   int64  `json:"callsOk"`
	CallsErr  int64  `json:"callsErr"`
	BytesOut  int64  `json:"bytesOut"`
	BytesIn   int64  `json:"bytesIn,omitempty"`
}

// WireStatsSnapshot returns a map of wire name → WireStat from the global
// WireCallsTotal and WireBytesTotal counters.
func WireStatsSnapshot() map[string]*WireStat {
	out := make(map[string]*WireStat)

	get := func(name string) *WireStat {
		s, ok := out[name]
		if !ok {
			s = &WireStat{Wire: name}
			out[name] = s
		}
		return s
	}

	// Read call counters.
	WireCallsTotal.mu.RLock()
	for key, val := range WireCallsTotal.values {
		parts := splitKey(key)
		if len(parts) < 2 {
			continue
		}
		wire, status := parts[0], parts[1]
		s := get(wire)
		v := val.Load()
		switch status {
		case "ok":
			s.CallsOK = v
		case "error":
			s.CallsErr = v
		}
	}
	WireCallsTotal.mu.RUnlock()

	// Read byte counters.
	WireBytesTotal.mu.RLock()
	for key, val := range WireBytesTotal.values {
		parts := splitKey(key)
		if len(parts) < 2 {
			continue
		}
		wire, direction := parts[0], parts[1]
		s := get(wire)
		v := val.Load()
		switch direction {
		case "out":
			s.BytesOut = v
		case "in":
			s.BytesIn = v
		}
	}
	WireBytesTotal.mu.RUnlock()

	return out
}

// splitKey splits a \x00-separated key into parts.
func splitKey(key string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(key); i++ {
		if key[i] == 0 {
			parts = append(parts, key[start:i])
			start = i + 1
		}
	}
	parts = append(parts, key[start:])
	return parts
}

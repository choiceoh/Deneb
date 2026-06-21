package main

import (
	"encoding/json"
	"net/http"
)

// statusModelRow is one routable model in the /status readout: name, wire
// protocol, local/cloud classification, whether it's effort-routable (has a
// thinking toggle), and where it came from ("config" or "fleet"-discovered).
// Names and classification only — never a key.
type statusModelRow struct {
	Name        string `json:"name"`
	Protocol    string `json:"protocol"`
	Local       bool   `json:"local"`
	Thinking    bool   `json:"thinking"`
	Source      string `json:"source"`
	MaxModelLen int    `json:"max_model_len,omitempty"` // backend vLLM context length (local only)
	// KeyHealth is the last upstream-auth probe for a cloud model: "ok" |
	// "auth_failed" (dead/invalid key) | "rate_limited" | "unreachable" | "http_N"
	// | "unchecked". Empty for local (keyless) models. Lets the picker flag a dead
	// key before a request 401s. See keyhealth.go.
	KeyHealth string `json:"keyHealth,omitempty"`
}

// statusOut is wormhole's live operational readout (GET /status): the global
// feature flags plus the full routable model set — configured AND discovered
// alike. It is the source of truth the Deneb gateway's management tab renders,
// richer than the OpenAI-standard /v1/models because it carries protocol,
// thinking, and source per model. Token-gated; returns no keys.
type statusOut struct {
	Listen        string           `json:"listen"`
	LocalOnly     bool             `json:"localOnly"`
	EffortRouting bool             `json:"effortRouting"`
	Auto          []string         `json:"auto"`
	Models        []statusModelRow `json:"models"`
}

// status serves GET /status: the live, rich operational view for the gateway's
// management tab. Token-gated like the model endpoints (it enumerates the routing
// table); it never returns upstream keys. Built from the live snapshot + the
// discovered fleet set, so it reflects hot-reloaded toggles and freshly launched
// SparkFleet models without a restart.
func (rt *router) status(w http.ResponseWriter, r *http.Request) {
	if !rt.authed(w, r) {
		return
	}
	s := rt.cur()
	windows := rt.windows.Load()
	out := statusOut{
		Listen:        s.cfg.Listen,
		LocalOnly:     s.cfg.LocalOnly,
		EffortRouting: s.cfg.effortRoutingOn(),
		Auto:          s.cfg.Auto,
		Models:        make([]statusModelRow, 0, len(s.cfg.Models)),
	}
	window := func(name string) int {
		if windows != nil {
			return (*windows)[name]
		}
		return 0
	}
	health := rt.keyHealth.Load()
	healthLabel := func(e modelEntry) string {
		st := keyHealthState{}
		if health != nil {
			st = (*health)[e.Name]
		}
		return st.label(!e.isLocal())
	}
	for _, e := range s.cfg.Models {
		out.Models = append(out.Models, statusRow(e, "config", window(e.Name), healthLabel(e)))
	}
	if f := rt.fleet.Load(); f != nil {
		for name, e := range *f {
			if _, shadowed := s.models[name]; shadowed {
				continue // a configured model of the same name already covers it
			}
			out.Models = append(out.Models, statusRow(e, "fleet", window(name), healthLabel(e)))
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// statusRow projects a modelEntry into a (keyless) status row tagged with its
// source, the backend's discovered context window (0 = unknown / cloud), and the
// last cloud-key-health label ("" for local).
func statusRow(e modelEntry, source string, maxModelLen int, keyHealth string) statusModelRow {
	return statusModelRow{
		Name:        e.Name,
		Protocol:    e.protocol(),
		Local:       e.isLocal(),
		Thinking:    e.ToggleKwarg != "",
		Source:      source,
		MaxModelLen: maxModelLen,
		KeyHealth:   keyHealth,
	}
}

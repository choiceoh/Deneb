package observe

import (
	"context"
	"encoding/json"
	"io"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
)

// EngineFleet is the serving engine's own account of who holds the fleet and
// how much it has served since it started, read from its root status GET.
//
// The fleet lock is what decides whether the engine exists at all: a kernel
// campaign or a deploy that takes the fleet stops the engine, and from the
// gateway's side that reads as "connection refused" with no cause attached.
// The owner string is the cause.
type EngineFleet struct {
	// Served is requests completed since this process started; Steps its
	// model steps. Both reset with the process, so a low number after a high
	// one is a restart, not a quiet hour.
	Served int64 `json:"served"`
	Steps  int64 `json:"steps"`
	// Parked is conversations the engine holds resident for resumption.
	Parked int `json:"parked"`

	// Owner is the fleet lock holder as the engine reports it (for example
	// "production/deploy/2011680"); Draining and HandedOver name a successor
	// when a handover is in progress or finished. FleetKnown is false when the
	// engine reported no fleet block at all.
	Owner      string `json:"owner,omitempty"`
	Draining   string `json:"draining,omitempty"`
	HandedOver string `json:"handedOver,omitempty"`
	FleetKnown bool   `json:"fleetKnown"`
}

// FetchEngineFleet reads the engine's root status document from the same
// server that serves metricsURL. Same private-host rule as the scraper; ok is
// false for anything but a well-formed answer from an owned host.
func FetchEngineFleet(ctx context.Context, metricsURL string) (EngineFleet, bool) {
	if !httputil.IsPrivateHost(httputil.Hostname(metricsURL)) {
		return EngineFleet{}, false
	}
	rootURL := strings.TrimSuffix(strings.TrimRight(metricsURL, "/"), "/metrics") + "/"
	client := httputil.NewClient(engineScrapeTimeout)
	body, ok := getBody(ctx, client, rootURL)
	if !ok {
		return EngineFleet{}, false
	}
	defer body.Close()

	var payload struct {
		Engine string `json:"engine"`
		Served int64  `json:"served"`
		Steps  int64  `json:"steps"`
		Parked int    `json:"parked"`
		Fleet  *struct {
			Owner      string `json:"owner"`
			Draining   string `json:"draining"`
			HandedOver string `json:"handed_over"`
		} `json:"fleet"`
	}
	if json.NewDecoder(io.LimitReader(body, 1<<20)).Decode(&payload) != nil {
		return EngineFleet{}, false
	}
	if payload.Engine == "" {
		return EngineFleet{}, false // answered, but not an engine's status document
	}
	out := EngineFleet{Served: payload.Served, Steps: payload.Steps, Parked: payload.Parked}
	if payload.Fleet != nil {
		out.FleetKnown = true
		out.Owner = strings.TrimSpace(payload.Fleet.Owner)
		out.Draining = strings.TrimSpace(payload.Fleet.Draining)
		out.HandedOver = strings.TrimSpace(payload.Fleet.HandedOver)
	}
	return out, true
}

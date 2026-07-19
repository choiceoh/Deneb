// sidecar_health_watch.go — wires sidecar dependency probes into the notify
// heartbeat (see runtime/notify/notify_deps.go for the alerting semantics).
//
// Motivation (2026-07-17/18): the embedding sidecar (then BGE-M3) died cleanly and
// stayed down for 33 hours; the embedding client logged "server unhealthy"
// every batch but nothing reached the operator. This wiring closes that class:
// the heartbeat now watches every local dependency the gateway relies on.
package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	runtimenotify "github.com/choiceoh/deneb/gateway-go/internal/runtime/notify"
)

// sidecarProbeTimeout caps one liveness probe. Loopback services answer in
// single-digit ms; 3s matches the heartbeat's own self-poll budget.
const sidecarProbeTimeout = 3 * time.Second

// registerSidecarHealthWatch installs dependency checks on the notify service:
//
//   - embedding: the embedding client's existing background health prober
//     (30s cadence) — no extra traffic, just surfaced. Watches whatever
//     DENEB_EMBEDDING_URL points at (Nemotron adapter since 2026-07-18).
//   - every DISTINCT loopback model-provider endpoint (in practice the
//     wormhole router on 127.0.0.1:18800): a liveness GET where ANY HTTP
//     response — auth errors included — means the process is alive, and only
//     a transport failure means down. Derived from the configured role
//     models, so a moved port or an additional local front needs no code
//     change, and installs without wormhole get no phantom alerts.
func (s *Server) registerSidecarHealthWatch() {
	if s.notify == nil {
		return
	}
	var checks []runtimenotify.DepCheck

	if s.embeddingClient != nil {
		embed := s.embeddingClient
		checks = append(checks, runtimenotify.DepCheck{
			Name: "embedding",
			Check: func() error {
				if embed.IsHealthy() {
					return nil
				}
				return errors.New("embedding server unhealthy")
			},
		})
	}

	for _, base := range s.loopbackProviderBases() {
		checks = append(checks, runtimenotify.DepCheck{
			Name:  base.name,
			Check: livenessCheck(base.url),
		})
	}

	if len(checks) > 0 {
		s.notify.SetDependencyChecks(checks)
	}
}

type loopbackBase struct {
	name string
	url  string
}

// loopbackProviderBases collects the distinct loopback base URLs among the
// configured role models. Remote providers (cloud APIs, the srv1/srv2 vLLM
// fleet) are excluded — their health is the fallback chain's business; a LOCAL
// front dying is this box's operational fault and the operator's to fix.
func (s *Server) loopbackProviderBases() []loopbackBase {
	if s.modelRegistry == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []loopbackBase
	for role := range s.modelRegistry.ConfiguredModels() {
		base := s.modelRegistry.BaseURL(role)
		if base == "" || seen[base] {
			continue
		}
		seen[base] = true
		u, err := url.Parse(base)
		if err != nil {
			continue
		}
		host := u.Hostname()
		if host != "127.0.0.1" && host != "localhost" && host != "::1" {
			continue
		}
		out = append(out, loopbackBase{name: u.Host, url: base})
	}
	return out
}

// livenessCheck builds a probe that treats any HTTP response as alive: a local
// router answering 401/404 is running; only a transport error (connection
// refused, timeout) is a down signal.
func livenessCheck(base string) func() error {
	client := &http.Client{Timeout: sidecarProbeTimeout}
	return func() error {
		ctx, cancel := context.WithTimeout(context.Background(), sidecarProbeTimeout)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base, http.NoBody)
		if err != nil {
			return fmt.Errorf("probe request: %w", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("unreachable: %w", err)
		}
		_ = resp.Body.Close()
		return nil
	}
}

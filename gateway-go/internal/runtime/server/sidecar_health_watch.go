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
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/artifact"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/document"
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
//   - the ASR and OCR sidecars, wherever they are configured to live. These
//     are watched even when REMOTE, which is the one place this file departs
//     from the loopback-only rule below — and the reason is that they have no
//     fallback chain. A model provider that dies falls through to the next one;
//     a dead ASR sidecar just makes transcription fail. On 2026-07-26 the ASR
//     container was found to have restarted 18,325 times, silently, because
//     nothing watched it: it was remote, so the rule excluded it, and it had no
//     fallback, so nothing else complained either.
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

	// No fallback ⇒ must be watched, local or not. ASR left this club with
	// the Gemini cutover (#4422): the sidecar is now itself the fallback, so
	// it is watched only while it is the primary transcription path —
	// alerting an operator about a dead fallback they run on purpose is
	// noise (live 2026-08-03).
	sidecars := []struct{ name, base string }{
		{"ocr", document.OCRBaseURL()},
	}
	if artifact.ASRSidecarIsPrimary() {
		sidecars = append(sidecars, struct{ name, base string }{"asr", artifact.ASRBaseURL()})
	}
	for _, sc := range sidecars {
		if strings.TrimSpace(sc.base) == "" {
			continue
		}
		checks = append(checks, runtimenotify.DepCheck{
			Name:  sc.name,
			Check: livenessCheck(sc.base),
		})
	}

	for _, base := range s.loopbackProviderBases() {
		checks = append(checks, runtimenotify.DepCheck{
			Name:  base.name,
			Check: livenessCheck(base.url),
		})
	}

	if len(checks) > 0 {
		// Down-state survives restarts here — without it every deploy
		// re-pushed the "down" alert for an outage the operator already
		// knew about (15 duplicate ASR alerts across a single 41h fault).
		stateFile := filepath.Join(config.ResolveStateDir(), "notify-dep-down.json")
		s.notify.SetDependencyChecks(checks, stateFile)
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

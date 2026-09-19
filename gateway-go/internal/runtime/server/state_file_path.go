package server

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
)

// stateFilePath resolves a server-owned store under the Deneb STATE dir
// (DENEB_STATE_DIR, default ~/.deneb) — the boundary the transcripts, polaris,
// cron store, agent-logs and workspace already use (#4831, #4833, #5124).
//
// The session sidecars (models, labels, pins, list pins, focus, repo bindings)
// used to be built from $HOME. Dev, live-test and puppet gateways run with
// DENEB_STATE_DIR=/tmp/deneb-<instance>-dev-state but the real $HOME, so they
// read and wrote the operator's sidecars while the transcripts those sidecars
// describe already lived in the dev state dir. Measured 2026-09-19: production
// session-labels.json carried four test conversations (client:main:titletest-…,
// client:cygnus:phasetest, …) with no production transcript behind them. Each
// gateway also flushes the copy it loaded at startup, so a dev flush could drop
// titles and pins production wrote after the dev gateway started, and a dev
// model override or focus on a shared key (client:main) came back in production
// on its next restart. Production is unchanged: the unit pins
// DENEB_STATE_DIR=$HOME/.deneb, which is also the default.
func stateFilePath(name string) (string, error) {
	stateDir := strings.TrimSpace(config.ResolveStateDir())
	if stateDir == "" {
		return "", errors.New("deneb state dir unresolved")
	}
	return filepath.Join(stateDir, name), nil
}

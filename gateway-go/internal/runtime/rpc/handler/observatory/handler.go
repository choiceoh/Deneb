// Package observatory (handler) exposes the self-improvement-loop liveness
// digest over RPC — the same observatory.Snapshot the in-process watchdog
// alerts on (server_observatory_watchdog.go), now pullable on demand.
//
// One read-only method, registered twice (the handler/observe idiom):
//
//   - observatory.snapshot          — in-process callers (chat tool, cron)
//   - miniapp.observatory.snapshot  — remote adapters through the client-token
//     gate (native dashboard, external CLI). Before this mirror the digest was
//     unreachable over the wire: handleMiniappRPC confines remote callers to
//     miniapp.*, and only the 30-minute watchdog ever read the snapshot.
//
// The package name intentionally matches ai/observatory (same idiom as the
// observe handler): local symbols (Deps, Methods) are unqualified, and the
// `observatory.` prefix refers to the imported core.
package observatory

import (
	"context"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/observatory"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// Deps carries the digest input. StateDir is a getter so the handler reads
// the state dir resolved at call time (config.ResolveStateDir in production
// wiring), never a boot-time copy.
type Deps struct {
	StateDir func() string
}

// Methods returns the in-process observatory.* handler map.
func Methods(deps Deps) map[string]rpcutil.HandlerFunc {
	return methodsWithPrefix(deps, "observatory.")
}

// MiniappMethods returns the same handler under miniapp.observatory.* so
// client-token holders can reach the digest over HTTP.
func MiniappMethods(deps Deps) map[string]rpcutil.HandlerFunc {
	return methodsWithPrefix(deps, "miniapp.observatory.")
}

func methodsWithPrefix(deps Deps, p string) map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		p + "snapshot": snapshotHandler(deps),
	}
}

// snapshotHandler returns the structured report plus its Markdown digest —
// the report for programmatic consumers, the digest for surfaces that just
// want to render the operator view.
func snapshotHandler(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		stateDir := ""
		if deps.StateDir != nil {
			stateDir = deps.StateDir()
		}
		rep := observatory.Snapshot(stateDir, time.Now())
		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
			"report":   rep,
			"markdown": rep.Markdown(),
		})
		return resp
	}
}

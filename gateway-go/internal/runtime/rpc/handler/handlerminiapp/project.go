// project.go — miniapp.project.* RPC handlers.
//
//   miniapp.project.digests — each active project's latest-progress digest
//                             (headline + a few bullets + any imminent due),
//                             newest first, for the "프로젝트 진행상황" 모아보기 screen.
//
// The digests are produced offline by the wiki dream cycle (one LLM roll-up per
// cycle, see domain/wiki/project_digest.go) and persisted per project by
// ProjectDigestStore. This handler is a thin read: it never calls an LLM, so the
// screen loads instantly from disk.

package handlerminiapp

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// ProjectDeps wires the project handler's read store. A nil Store skips
// registration (the screen is simply absent until the dreamer has run and a
// store is wired).
type ProjectDeps struct {
	Store *ProjectDigestStore
}

// ProjectDigestRow is one project's latest-progress card for the native client.
// UpdatedAtMs is the digest's generation time in epoch millis (0 when unknown);
// the client renders it as a "as of" date. Marked //deneb:wire so the Kotlin
// type is generated from this one source of truth.
//
//deneb:wire
type ProjectDigestRow struct {
	Project     string   `json:"project"`
	Headline    string   `json:"headline"`
	Bullets     []string `json:"bullets,omitempty"`
	Due         string   `json:"due,omitempty"`
	UpdatedAtMs int64    `json:"updatedAtMs,omitempty"`
}

// ProjectDigestsOut is the miniapp.project.digests response: every stored
// project digest, newest first.
//
//deneb:wire
type ProjectDigestsOut struct {
	Digests []ProjectDigestRow `json:"digests"`
}

// ProjectMethods returns the miniapp.project.* handler map. Returns nil when no
// store is wired so method_registry.go can skip registration cleanly.
func ProjectMethods(deps ProjectDeps) map[string]rpcutil.HandlerFunc {
	if deps.Store == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"miniapp.project.digests": projectDigests(deps),
	}
}

func projectDigests(deps ProjectDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		recs, err := deps.Store.list()
		if err != nil {
			return rpcerr.WrapUnavailable("project digests unavailable", err).Response(req.ID)
		}
		rows := make([]ProjectDigestRow, 0, len(recs))
		for _, r := range recs {
			rows = append(rows, ProjectDigestRow{
				Project:     r.Project,
				Headline:    r.Headline,
				Bullets:     r.Bullets,
				Due:         r.Due,
				UpdatedAtMs: timeToMillis(r.UpdatedAt),
			})
		}
		return rpcutil.RespondOK(req.ID, ProjectDigestsOut{Digests: rows})
	}
}

// sessions.go — miniapp.sessions.recent RPC handler.
//
// Wraps session.Manager.List() and trims/sorts the result for the Mini
// App's "recent sessions" card. We deliberately do not call
// sessions.list (which exists for the broader RPC surface) — the Mini App
// only needs a small projection of each session and we want to keep the
// browser-origin response shape stable independently of the wider list
// method.

package handlerminiapp

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// SessionsLister is the subset of *session.Manager the handler needs.
// Tests provide a fake; production wires the real Manager.
type SessionsLister interface {
	List() []*session.Session
}

// SessionsDeps holds the manager. Required at boot (the gateway always
// has a session manager) so no lazy factory needed — straight assignment.
type SessionsDeps struct {
	Manager SessionsLister
}

const (
	defaultSessionsLimit = 10
	maxSessionsLimit     = 100
)

// SessionsMethods returns the miniapp.sessions.* handler map.
func SessionsMethods(deps SessionsDeps) map[string]rpcutil.HandlerFunc {
	if deps.Manager == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"miniapp.sessions.recent": sessionsRecent(deps),
	}
}

func sessionsRecent(deps SessionsDeps) rpcutil.HandlerFunc {
	type params struct {
		Limit   int    `json:"limit,omitempty"`
		Channel string `json:"channel,omitempty"`
	}
	type rowOut struct {
		Key         string `json:"key"`
		Kind        string `json:"kind,omitempty"`
		Status      string `json:"status,omitempty"`
		Channel     string `json:"channel,omitempty"`
		Model       string `json:"model,omitempty"`
		Label       string `json:"label,omitempty"`
		UpdatedAtMs int64  `json:"updatedAtMs,omitempty"`
		StartedAtMs *int64 `json:"startedAtMs,omitempty"`
		RuntimeMs   *int64 `json:"runtimeMs,omitempty"`
		TotalTokens *int64 `json:"totalTokens,omitempty"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		var p params
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return rpcerr.InvalidParams(err).Response(req.ID)
			}
		}
		limit := p.Limit
		if limit <= 0 {
			limit = defaultSessionsLimit
		}
		if limit > maxSessionsLimit {
			limit = maxSessionsLimit
		}

		sessions := deps.Manager.List()

		// Filter by channel if requested.
		if p.Channel != "" {
			filtered := sessions[:0]
			for _, s := range sessions {
				if s.Channel == p.Channel {
					filtered = append(filtered, s)
				}
			}
			sessions = filtered
		}

		// Sort newest-first by UpdatedAt (UnixMilli). Sessions whose
		// UpdatedAt is zero fall to the back so they don't pollute the
		// fresh top of the list.
		sort.SliceStable(sessions, func(i, j int) bool {
			return sessions[i].UpdatedAt > sessions[j].UpdatedAt
		})
		if len(sessions) > limit {
			sessions = sessions[:limit]
		}

		out := make([]rowOut, 0, len(sessions))
		for _, s := range sessions {
			out = append(out, rowOut{
				Key:         s.Key,
				Kind:        string(s.Kind),
				Status:      string(s.Status),
				Channel:     s.Channel,
				Model:       s.Model,
				Label:       s.Label,
				UpdatedAtMs: s.UpdatedAt,
				StartedAtMs: s.StartedAt,
				RuntimeMs:   s.RuntimeMs,
				TotalTokens: s.TotalTokens,
			})
		}
		return rpcutil.RespondOK(req.ID, map[string]any{
			"sessions": out,
			"count":    len(out),
		})
	}
}

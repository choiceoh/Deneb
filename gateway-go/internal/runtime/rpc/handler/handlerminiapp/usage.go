package handlerminiapp

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/core/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/core/rpcerr"
	runtimesession "github.com/choiceoh/deneb/gateway-go/internal/domain/session"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/minibind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// UsageStat is one labeled row of token usage in a breakdown — a model, a role,
// or a work-type — summed over the requested window.
//
//deneb:wire
type UsageStat struct {
	Name            string `json:"name"`
	Runs            int    `json:"runs"`
	InputTokens     int64  `json:"inputTokens"`
	OutputTokens    int64  `json:"outputTokens"`
	CacheReadTokens int64  `json:"cacheReadTokens"`
}

// UsageStatsResult is the miniapp.usage.stats response: token usage over the
// last `Days` days, broken down three ways — by the model the provider reported
// serving, by the role the run requested, and by work-type (heartbeat,
// phone-event, chat, …).
// Each list is sorted by total (input+output) tokens descending.
//
//deneb:wire
type UsageStatsResult struct {
	Days              int         `json:"days"`
	TotalInputTokens  int64       `json:"totalInputTokens"`
	TotalOutputTokens int64       `json:"totalOutputTokens"`
	ByModel           []UsageStat `json:"byModel"`
	ByRole            []UsageStat `json:"byRole"`
	ByWorkType        []UsageStat `json:"byWorkType"`
}

// UsageDeps holds the lazy dependencies for the token-usage RPC.
type UsageDeps struct {
	// AgentLog resolves the agent-log writer (the token-usage source). Lazy
	// because the writer is built in the session phase, after early registration.
	AgentLog func() *agentlog.Writer
	// RoleForRequest maps a run's requested model string (a role name like
	// "submain"/"coding", a raw model id, or "" for the default) to a role slug
	// (main/submain/coding/lightweight/tiny/fallback/vision). Wired from the model
	// registry. nil → "" folds to "main" and any other string is reported as-is.
	RoleForRequest func(requested string) string
}

// UsageMethods returns the Mini App token-usage RPC handlers.
func UsageMethods(deps UsageDeps) map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"miniapp.usage.stats": usageStats(deps),
	}
}

func usageStats(deps UsageDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if minibind.Identity(ctx) == nil {
			return rpcerr.New(protocol.ErrUnauthorized, "miniapp.usage.stats requires client identity context").Response(req.ID)
		}
		var p struct {
			Days int `json:"days"`
		}
		if len(req.Params) > 0 {
			_ = json.Unmarshal(req.Params, &p)
		}
		days := p.Days
		if days <= 0 {
			days = 1
		}
		if days > 31 {
			days = 31
		}

		var alog *agentlog.Writer
		if deps.AgentLog != nil {
			alog = deps.AgentLog()
		}
		if alog == nil {
			return rpcutil.RespondOK(req.ID, UsageStatsResult{
				Days:       days,
				ByModel:    []UsageStat{},
				ByRole:     []UsageStat{},
				ByWorkType: []UsageStat{},
			})
		}

		since := time.Now().Add(-time.Duration(days) * 24 * time.Hour).UnixMilli()

		// By model (the model the provider reported serving — an endpoint may
		// alias a pinned id to newer weights, so the requested id can name a
		// model that never ran).
		byModel := make([]UsageStat, 0)
		for _, m := range alog.AggregateByServedModel(since) {
			name := m.Model
			if m.Provider != "" {
				name = m.Provider + "/" + m.Model
			}
			byModel = append(byModel, UsageStat{
				Name:            name,
				Runs:            m.Runs,
				InputTokens:     m.InputTokens,
				OutputTokens:    m.OutputTokens,
				CacheReadTokens: m.CacheReadTokens,
			})
		}

		// By role (fold the requested-model stats through RoleForRequest, so
		// roles sharing one model — e.g. coding vs submain on glm — separate).
		roleAcc := map[string]*UsageStat{}
		for _, r := range alog.AggregateByRequestedModel(since) {
			role := r.RequestedModel
			switch {
			case deps.RoleForRequest != nil:
				role = deps.RoleForRequest(r.RequestedModel)
			case role == "":
				role = "main"
			}
			accUsage(roleAcc, role, r.Runs, r.InputTokens, r.OutputTokens, r.CacheReadTokens)
		}

		// By work-type (fold the per-session stats through the key classifier).
		workAcc := map[string]*UsageStat{}
		var totalIn, totalOut int64
		for _, s := range alog.AggregateBySession(since) {
			bucket := runtimesession.WorkTypeForKey(s.Session)
			accUsage(workAcc, bucket, s.Runs, s.InputTokens, s.OutputTokens, s.CacheReadTokens)
			totalIn += s.InputTokens
			totalOut += s.OutputTokens
		}

		return rpcutil.RespondOK(req.ID, UsageStatsResult{
			Days:              days,
			TotalInputTokens:  totalIn,
			TotalOutputTokens: totalOut,
			ByModel:           byModel,
			ByRole:            usageSlice(roleAcc),
			ByWorkType:        usageSlice(workAcc),
		})
	}
}

// accUsage adds one aggregated row into a name→stat accumulator.
func accUsage(acc map[string]*UsageStat, name string, runs int, in, out, cache int64) {
	s := acc[name]
	if s == nil {
		s = &UsageStat{Name: name}
		acc[name] = s
	}
	s.Runs += runs
	s.InputTokens += in
	s.OutputTokens += out
	s.CacheReadTokens += cache
}

// usageSlice flattens an accumulator into a slice sorted by total tokens desc.
func usageSlice(acc map[string]*UsageStat) []UsageStat {
	out := make([]UsageStat, 0, len(acc))
	for _, s := range acc {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool {
		ti := out[i].InputTokens + out[i].OutputTokens
		tj := out[j].InputTokens + out[j].OutputTokens
		if ti != tj {
			return ti > tj
		}
		return out[i].Name < out[j].Name
	})
	return out
}

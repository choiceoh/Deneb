// calendar_proposals.go — miniapp.calendar.proposals.* RPC handlers (the bell).
//
//	miniapp.calendar.proposals.list   — pending calendar proposals
//	miniapp.calendar.proposals.accept — create a local event from a proposal
//	miniapp.calendar.proposals.reject — dismiss a proposal (never re-proposed)
//
// Proposals are schedule-worthy items mail analysis surfaced (see
// server/mail_calendar.go). Nothing is auto-added: the operator accepts a
// proposal, which lands it in the local calendar store.

package handlerminiapp

import (
	"context"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/calprop"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/localcal"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// CalProposals is the subset of *calprop.Store the handlers use.
type CalProposals interface {
	ListPending() ([]calprop.Proposal, error)
	Get(id string) (*calprop.Proposal, error)
	Decide(id string, status calprop.Status, calendarEventID string) (*calprop.Proposal, error)
}

// calendarProposalOut is the wire shape for a pending calendar proposal, shared
// with the native client via codegen so the bell list can't drift.
//
//deneb:wire
type calendarProposalOut struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	Start         string `json:"start"` // RFC3339 (timed) or "2006-01-02" (all-day)
	AllDay        bool   `json:"allDay"`
	Kind          string `json:"kind"` // "meeting" | "deadline"
	SourceSubject string `json:"sourceSubject,omitempty"`
	SourceFrom    string `json:"sourceFrom,omitempty"`
}

func proposalOut(p calprop.Proposal) calendarProposalOut {
	return calendarProposalOut{
		ID:            p.ID,
		Title:         p.Title,
		Start:         p.Start,
		AllDay:        p.AllDay,
		Kind:          p.Kind,
		SourceSubject: p.SourceSubject,
		SourceFrom:    p.SourceFrom,
	}
}

// calendarProposalsList returns the pending calendar proposals (the bell list).
func calendarProposalsList(deps CalendarDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		if deps.Proposals == nil {
			return rpcerr.Unavailable("calendar proposals unavailable").Response(req.ID)
		}
		ps, err := deps.Proposals.ListPending()
		if err != nil {
			return rpcerr.WrapUnavailable("calendar proposals list failed", err).Response(req.ID)
		}
		out := make([]calendarProposalOut, 0, len(ps))
		for _, p := range ps {
			out = append(out, proposalOut(p))
		}
		return rpcutil.RespondOK(req.ID, map[string]any{"proposals": out})
	}
}

// calendarProposalsAccept creates a local calendar event from a pending proposal
// and marks it accepted (storing the created event id).
func calendarProposalsAccept(deps CalendarDeps) rpcutil.HandlerFunc {
	type params struct {
		ID string `json:"id"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		if deps.Proposals == nil || deps.Local == nil {
			return rpcerr.Unavailable("calendar proposals unavailable").Response(req.ID)
		}
		p, errResp := rpcutil.DecodeParams[params](req)
		if errResp != nil {
			return errResp
		}
		if strings.TrimSpace(p.ID) == "" {
			return rpcerr.MissingParam("id").Response(req.ID)
		}
		prop, err := deps.Proposals.Get(p.ID)
		if err != nil {
			return rpcerr.WrapUnavailable("calendar proposal lookup failed", err).Response(req.ID)
		}
		if prop == nil {
			return rpcerr.NotFound("proposal " + rpcutil.TruncateForError(p.ID)).Response(req.ID)
		}
		if prop.Status != calprop.StatusPending {
			return rpcerr.ValidationFailed("이미 처리된 제안입니다.").Response(req.ID)
		}
		start, end, allDay, perr := proposalTimes(prop)
		if perr != nil {
			return rpcerr.ValidationFailed("제안의 날짜를 해석할 수 없습니다.").Response(req.ID)
		}
		ev, cerr := deps.Local.Create(localcal.CreateInput{
			Summary: prop.Title,
			Start:   start,
			End:     end,
			AllDay:  allDay,
		})
		if cerr != nil {
			return rpcerr.WrapUnavailable("calendar create failed", cerr).Response(req.ID)
		}
		updated, derr := deps.Proposals.Decide(p.ID, calprop.StatusAccepted, ev.ID)
		if derr != nil {
			return rpcerr.WrapUnavailable("calendar proposal update failed", derr).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, map[string]any{
			"ok":       true,
			"eventId":  ev.ID,
			"proposal": proposalOut(*updated),
		})
	}
}

// calendarProposalsReject marks a proposal rejected so it is never re-proposed.
func calendarProposalsReject(deps CalendarDeps) rpcutil.HandlerFunc {
	type params struct {
		ID string `json:"id"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		if deps.Proposals == nil {
			return rpcerr.Unavailable("calendar proposals unavailable").Response(req.ID)
		}
		p, errResp := rpcutil.DecodeParams[params](req)
		if errResp != nil {
			return errResp
		}
		if strings.TrimSpace(p.ID) == "" {
			return rpcerr.MissingParam("id").Response(req.ID)
		}
		updated, err := deps.Proposals.Decide(p.ID, calprop.StatusRejected, "")
		if err != nil {
			return rpcerr.WrapUnavailable("calendar proposal update failed", err).Response(req.ID)
		}
		if updated == nil {
			return rpcerr.NotFound("proposal " + rpcutil.TruncateForError(p.ID)).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, map[string]any{"ok": true, "proposal": proposalOut(*updated)})
	}
}

// proposalTimes resolves a proposal's Start string into event start/end times.
// All-day → [date 00:00, +24h); timed → [start, +1h).
func proposalTimes(p *calprop.Proposal) (start, end time.Time, allDay bool, err error) {
	if p.AllDay {
		d, perr := time.ParseInLocation("2006-01-02", p.Start, time.Local)
		if perr != nil {
			return time.Time{}, time.Time{}, false, perr
		}
		return d, d.Add(24 * time.Hour), true, nil
	}
	t, perr := time.Parse(time.RFC3339, p.Start)
	if perr != nil {
		return time.Time{}, time.Time{}, false, perr
	}
	return t, t.Add(time.Hour), false, nil
}

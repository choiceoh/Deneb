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
	ClaimForAccept(id string) (*calprop.Proposal, bool, error)
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
		// Claim the proposal before creating any event: a single atomic
		// pending→accepted transition, so a fast double-tap on 수락 (two concurrent
		// accept RPCs) can't both pass the pending check and create duplicate
		// events. The loser gets claimed=false and creates nothing.
		prop, claimed, err := deps.Proposals.ClaimForAccept(p.ID)
		if err != nil {
			return rpcerr.WrapUnavailable("calendar proposal lookup failed", err).Response(req.ID)
		}
		if prop == nil {
			return rpcerr.NotFound("proposal " + rpcutil.TruncateForError(p.ID)).Response(req.ID)
		}
		if !claimed {
			return rpcerr.ValidationFailed("이미 처리된 제안입니다.").Response(req.ID)
		}
		start, end, allDay, perr := proposalTimes(prop)
		if perr != nil {
			_, _ = deps.Proposals.Decide(p.ID, calprop.StatusPending, "") // release the claim
			return rpcerr.ValidationFailed("제안의 날짜를 해석할 수 없습니다.").Response(req.ID)
		}
		ev, cerr := deps.Local.Create(localcal.CreateInput{
			Summary: prop.Title,
			Start:   start,
			End:     end,
			AllDay:  allDay,
			// Carry the proposal's provenance onto the event so it stays a
			// first-class linked object: the agent can brief on it and follow it
			// back to the originating mail (the bell used to drop all of this).
			Source:      proposalEventSource(prop.Source),
			SourceLabel: prop.SourceSubject,
			Kind:        prop.Kind,
		})
		if cerr != nil {
			_, _ = deps.Proposals.Decide(p.ID, calprop.StatusPending, "") // release so it can be retried
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

// proposalEventSource turns a proposal's dedup Source ("mail:<msgID>|<title>")
// into the clean machine link stored on the event ("mail:<msgID>"), dropping the
// per-title suffix that only kept proposals unique. Returned unchanged when it
// carries no "|" suffix.
func proposalEventSource(source string) string {
	source = strings.TrimSpace(source)
	if i := strings.IndexByte(source, '|'); i >= 0 {
		return strings.TrimSpace(source[:i])
	}
	return source
}

// proposalTimes resolves a proposal's Start string into event start/end times.
// All-day events anchor at local NOON (not midnight) so the date survives the
// store's RFC3339 round-trip — a local-midnight instant serializes with a TZ
// offset that can roll back to the previous day in UTC, which showed a 6/20
// proposal as a 6/19 event. The AllDay flag drives whole-day rendering, so the
// time-of-day is cosmetic. Timed → [start, +1h).
func proposalTimes(p *calprop.Proposal) (start, end time.Time, allDay bool, err error) {
	if p.AllDay {
		d, perr := time.ParseInLocation("2006-01-02", p.Start, time.Local)
		if perr != nil {
			return time.Time{}, time.Time{}, false, perr
		}
		start = time.Date(d.Year(), d.Month(), d.Day(), 12, 0, 0, 0, time.Local)
		return start, start.Add(time.Hour), true, nil
	}
	t, perr := time.Parse(time.RFC3339, p.Start)
	if perr != nil {
		return time.Time{}, time.Time{}, false, perr
	}
	return t, t.Add(time.Hour), false, nil
}

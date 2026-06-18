package handlerminiapp

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// SelfImprovementCodingDeps wires the native "자가개선 코딩" settings section to
// Propus' deferred self-correction queue. These rows are not skills and not
// lifecycle log events; they are unapplied coding hypotheses for batch review.
type SelfImprovementCodingDeps struct {
	RecentCandidates func(status string, limit int) ([]genesis.SelfCorrectionCandidateRecord, error)
}

// SelfCorrectionCandidate is one pending deferred correction from the
// append-only self-correction queue.
//
//deneb:wire
type SelfCorrectionCandidate struct {
	ID             string   `json:"id"`
	Status         string   `json:"status,omitempty"`
	Scope          string   `json:"scope,omitempty"`
	SkillName      string   `json:"skillName,omitempty"`
	SessionKey     string   `json:"sessionKey,omitempty"`
	Title          string   `json:"title,omitempty"`
	Candidate      string   `json:"candidate,omitempty"`
	Evidence       string   `json:"evidence,omitempty"`
	Reason         string   `json:"reason,omitempty"`
	TargetFiles    []string `json:"targetFiles,omitempty"`
	ProposedChange string   `json:"proposedChange,omitempty"`
	Risk           string   `json:"risk,omitempty"`
	Source         string   `json:"source,omitempty"`
	Reviewer       string   `json:"reviewer,omitempty"`
	ReviewNote     string   `json:"reviewNote,omitempty"`
	CreatedAt      int64    `json:"createdAt,omitempty"`
	UpdatedAt      int64    `json:"updatedAt,omitempty"`
}

// SelfImprovementCodingListResponse is the miniapp.self_improvement_coding.list
// payload.
//
//deneb:wire
type SelfImprovementCodingListResponse struct {
	Candidates []SelfCorrectionCandidate `json:"candidates"`
	Count      int                       `json:"count"`
}

func SelfImprovementCodingMethods(deps SelfImprovementCodingDeps) map[string]rpcutil.HandlerFunc {
	if deps.RecentCandidates == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"miniapp.self_improvement_coding.list": selfImprovementCodingList(deps),
	}
}

func selfImprovementCodingList(deps SelfImprovementCodingDeps) rpcutil.HandlerFunc {
	type params struct {
		Limit  int    `json:"limit"`
		Status string `json:"status"`
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
		if p.Limit <= 0 || p.Limit > lifecycleScanLimit {
			p.Limit = 60
		}
		status := strings.TrimSpace(p.Status)
		if status == "" {
			status = genesis.SelfCorrectionStatusProposed
		}
		recs, err := deps.RecentCandidates(status, p.Limit)
		if err != nil {
			return rpcerr.WrapUnavailable("self-improvement coding queue unavailable", err).Response(req.ID)
		}
		candidates := make([]SelfCorrectionCandidate, 0, len(recs))
		for _, rec := range recs {
			candidates = append(candidates, selfCorrectionCandidate(rec))
		}
		return rpcutil.RespondOK(req.ID, SelfImprovementCodingListResponse{
			Candidates: candidates,
			Count:      len(candidates),
		})
	}
}

func selfCorrectionCandidate(rec genesis.SelfCorrectionCandidateRecord) SelfCorrectionCandidate {
	return SelfCorrectionCandidate{
		ID:             rec.ID,
		Status:         rec.Status,
		Scope:          rec.Scope,
		SkillName:      rec.SkillName,
		SessionKey:     rec.SessionKey,
		Title:          truncateDetail(rec.Title, lifecycleTextMaxRunes),
		Candidate:      truncateDetail(rec.Candidate, lifecycleTextMaxRunes),
		Evidence:       truncateDetail(rec.Evidence, lifecycleTextMaxRunes),
		Reason:         truncateDetail(rec.Reason, lifecycleTextMaxRunes),
		TargetFiles:    rec.TargetFiles,
		ProposedChange: truncateDetail(rec.ProposedChange, lifecycleTextMaxRunes),
		Risk:           truncateDetail(rec.Risk, lifecycleTextMaxRunes),
		Source:         rec.Source,
		Reviewer:       rec.Reviewer,
		ReviewNote:     truncateDetail(rec.ReviewNote, lifecycleTextMaxRunes),
		CreatedAt:      rec.CreatedAt,
		UpdatedAt:      rec.UpdatedAt,
	}
}

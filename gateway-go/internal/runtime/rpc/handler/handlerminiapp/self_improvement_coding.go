package handlerminiapp

import (
	"context"
	"encoding/json"
	"fmt"
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
	// Funnel reports capture-side activity (optional — zero summary when absent).
	Funnel func() genesis.SelfCorrectionFunnelSummary
	// LastNudgeAtMs reports when the heartbeat self-coding review lane last
	// fired (optional; 0 = never).
	LastNudgeAtMs func() int64
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
	EvidenceKinds  []string `json:"evidenceKinds,omitempty"`
	ReviewActions  []string `json:"reviewActions,omitempty"`
	CreatedAt      int64    `json:"createdAt,omitempty"`
	UpdatedAt      int64    `json:"updatedAt,omitempty"`
}

// SelfImprovementCodingStatusCount summarizes the deferred coding queue by
// review status so native can show the queue as a lifecycle surface, not only a
// one-off pending list.
//
//deneb:wire
type SelfImprovementCodingStatusCount struct {
	Status string `json:"status"`
	Count  int    `json:"count"`
}

// SelfImprovementCodingFunnel explains why the queue is (or is not) receiving
// candidates: last capture/review/rejection timestamps, how many recent evolve
// rejections were even eligible for promotion, and when the heartbeat review
// lane last fired. An empty queue with a live upstream reads "consumed", not
// "broken" — that distinction is this struct's whole job.
//
//deneb:wire
type SelfImprovementCodingFunnel struct {
	LastCaptureAt          int64 `json:"lastCaptureAt,omitempty"`
	LastReviewAt           int64 `json:"lastReviewAt,omitempty"`
	Rejections7d           int   `json:"rejections7d,omitempty"`
	PromotableRejections7d int   `json:"promotableRejections7d,omitempty"`
	LastRejectionAt        int64 `json:"lastRejectionAt,omitempty"`
	LastNudgeAt            int64 `json:"lastNudgeAt,omitempty"`
}

// SelfImprovementCodingListResponse is the miniapp.self_improvement_coding.list
// payload.
//
//deneb:wire
type SelfImprovementCodingListResponse struct {
	Candidates []SelfCorrectionCandidate `json:"candidates"`
	Count      int                       `json:"count"`
	// StatusCounts is computed over the latest queue window across all statuses.
	StatusCounts []SelfImprovementCodingStatusCount `json:"statusCounts,omitempty"`
	// Funnel is the capture-side health summary for the header line.
	Funnel SelfImprovementCodingFunnel `json:"funnel"`
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
		status, err := normalizeSelfImprovementCodingStatus(p.Status)
		if err != nil {
			return rpcerr.InvalidParams(err).Response(req.ID)
		}
		recs, err := deps.RecentCandidates(status, p.Limit)
		if err != nil {
			return rpcerr.WrapUnavailable("self-improvement coding queue unavailable", err).Response(req.ID)
		}
		allRecs, err := deps.RecentCandidates("", lifecycleScanLimit)
		if err != nil {
			return rpcerr.WrapUnavailable("self-improvement coding queue unavailable", err).Response(req.ID)
		}
		candidates := make([]SelfCorrectionCandidate, 0, len(recs))
		for _, rec := range recs {
			candidates = append(candidates, selfCorrectionCandidate(rec))
		}
		return rpcutil.RespondOK(req.ID, SelfImprovementCodingListResponse{
			Candidates:   candidates,
			Count:        len(candidates),
			StatusCounts: selfImprovementCodingStatusCounts(allRecs),
			Funnel:       selfImprovementCodingFunnel(deps),
		})
	}
}

func selfImprovementCodingFunnel(deps SelfImprovementCodingDeps) SelfImprovementCodingFunnel {
	var out SelfImprovementCodingFunnel
	if deps.Funnel != nil {
		f := deps.Funnel()
		out = SelfImprovementCodingFunnel{
			LastCaptureAt:          f.LastCaptureAt,
			LastReviewAt:           f.LastReviewAt,
			Rejections7d:           f.Rejections7d,
			PromotableRejections7d: f.PromotableRejections7d,
			LastRejectionAt:        f.LastRejectionAt,
		}
	}
	if deps.LastNudgeAtMs != nil {
		out.LastNudgeAt = deps.LastNudgeAtMs()
	}
	return out
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
		EvidenceKinds:  selfCorrectionEvidenceKinds(rec),
		ReviewActions:  selfCorrectionReviewActions(rec),
		CreatedAt:      rec.CreatedAt,
		UpdatedAt:      rec.UpdatedAt,
	}
}

func selfCorrectionEvidenceKinds(rec genesis.SelfCorrectionCandidateRecord) []string {
	out := make([]string, 0, 6)
	if strings.TrimSpace(rec.SessionKey) != "" {
		out = append(out, "session")
	}
	if strings.TrimSpace(rec.Evidence) != "" {
		out = append(out, "evidence")
	}
	if len(rec.TargetFiles) > 0 {
		out = append(out, "target_files")
	}
	if strings.TrimSpace(rec.Risk) != "" {
		out = append(out, "risk")
	}
	if strings.TrimSpace(rec.Reviewer) != "" || strings.TrimSpace(rec.ReviewNote) != "" {
		out = append(out, "review")
	}
	if len(out) == 0 {
		out = append(out, "needs_evidence")
	}
	return out
}

func selfCorrectionReviewActions(rec genesis.SelfCorrectionCandidateRecord) []string {
	out := make([]string, 0, 5)
	if strings.TrimSpace(rec.SessionKey) != "" {
		out = append(out, "open_session")
	}
	if len(rec.TargetFiles) > 0 {
		out = append(out, "inspect_target_files")
	}
	if strings.TrimSpace(rec.Evidence) == "" {
		out = append(out, "add_evidence")
	}
	if strings.TrimSpace(rec.Risk) == "" {
		out = append(out, "assess_risk")
	}
	out = append(out, "run_focused_validation", "mark_review_status")
	return out
}

func normalizeSelfImprovementCodingStatus(status string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "":
		return genesis.SelfCorrectionStatusProposed, nil
	case "all", "*":
		return "", nil
	case "pending", "proposed", "open":
		return genesis.SelfCorrectionStatusProposed, nil
	case "accept", "accepted":
		return genesis.SelfCorrectionStatusAccepted, nil
	case "reject", "rejected":
		return genesis.SelfCorrectionStatusRejected, nil
	case "supersede", "superseded":
		return genesis.SelfCorrectionStatusSuperseded, nil
	case "apply", "applied":
		return genesis.SelfCorrectionStatusApplied, nil
	default:
		return "", fmt.Errorf("unknown self-improvement coding status %q", status)
	}
}

func selfImprovementCodingStatusCounts(recs []genesis.SelfCorrectionCandidateRecord) []SelfImprovementCodingStatusCount {
	counts := map[string]int{
		"all":                                  len(recs),
		genesis.SelfCorrectionStatusProposed:   0,
		genesis.SelfCorrectionStatusAccepted:   0,
		genesis.SelfCorrectionStatusApplied:    0,
		genesis.SelfCorrectionStatusRejected:   0,
		genesis.SelfCorrectionStatusSuperseded: 0,
	}
	for _, rec := range recs {
		status := strings.TrimSpace(rec.Status)
		if status == "" {
			status = genesis.SelfCorrectionStatusProposed
		}
		if _, ok := counts[status]; ok {
			counts[status]++
		}
	}
	order := []string{
		genesis.SelfCorrectionStatusProposed,
		genesis.SelfCorrectionStatusAccepted,
		genesis.SelfCorrectionStatusApplied,
		genesis.SelfCorrectionStatusRejected,
		genesis.SelfCorrectionStatusSuperseded,
		"all",
	}
	out := make([]SelfImprovementCodingStatusCount, 0, len(order))
	for _, status := range order {
		out = append(out, SelfImprovementCodingStatusCount{Status: status, Count: counts[status]})
	}
	return out
}

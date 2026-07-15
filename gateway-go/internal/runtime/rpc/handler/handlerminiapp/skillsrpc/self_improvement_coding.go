package skillsrpc

import (
	"context"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/core/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	rsilifecycle "github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/lifecycle"
	handlerminiapp "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/minibind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
	"github.com/choiceoh/deneb/gateway-go/pkg/textutil"
)

// Wire type aliases — see handlerminiapp/self_improvement_coding_contract.go.
type (
	SelfCorrectionCandidate             = handlerminiapp.SelfCorrectionCandidate
	SelfImprovementCodingStatusCount    = handlerminiapp.SelfImprovementCodingStatusCount
	SelfImprovementCodingFunnel         = handlerminiapp.SelfImprovementCodingFunnel
	SelfImprovementCodingListResponse   = handlerminiapp.SelfImprovementCodingListResponse
	SelfImprovementCodingRecordResponse = handlerminiapp.SelfImprovementCodingRecordResponse
)

// SelfImprovementCodingDeps wires the native "자가개선 코딩" settings section to
// Propus' deferred self-correction queue. These rows are not skills and not
// lifecycle log events; they are unapplied coding hypotheses for batch review.
type SelfImprovementCodingDeps struct {
	RecentCandidates func(status string, limit int) ([]genesis.SelfCorrectionCandidateRecord, error)
	// RecordCandidate appends one propose-only candidate through the genesis
	// tracker — the queue's single writer, which enforces the forbidden-surface
	// list at record time. Optional: without it the record method is absent.
	RecordCandidate func(genesis.SelfCorrectionCandidateRecord) (genesis.SelfCorrectionCandidateRecord, error)
	// RecordDispatch appends one authoritative delivery event (started -> PR ->
	// merged -> deployed -> watched/rolled back). Optional for read-only servers.
	RecordDispatch func(genesis.SelfCorrectionCandidateRecord) (genesis.SelfCorrectionCandidateRecord, error)
	// Funnel reports capture-side activity (optional — zero summary when absent).
	Funnel func() genesis.SelfCorrectionFunnelSummary
	// LastNudgeAtMs reports when the heartbeat self-coding review lane last
	// fired (optional; 0 = never).
	LastNudgeAtMs func() int64
}

// SelfImprovementCodingMethods registers self-improvement coding RPC handlers.
func SelfImprovementCodingMethods(deps SelfImprovementCodingDeps) map[string]rpcutil.HandlerFunc {
	if deps.RecentCandidates == nil {
		return nil
	}
	methods := map[string]rpcutil.HandlerFunc{
		"miniapp.self_improvement_coding.list": selfImprovementCodingList(deps),
	}
	if deps.RecordCandidate != nil {
		methods["miniapp.self_improvement_coding.record"] = selfImprovementCodingRecord(deps)
	}
	if deps.RecordDispatch != nil {
		methods["miniapp.self_improvement_coding.dispatch"] = selfImprovementCodingDispatch(deps)
	}
	return methods
}

// selfImprovementCodingDispatch records one deterministic delivery transition.
// The tracker validates both candidate existence and the delivery FSM; callers
// cannot skip directly from queued to merged or close a deployment as applied
// before the rollback watch passes.
func selfImprovementCodingDispatch(deps SelfImprovementCodingDeps) rpcutil.HandlerFunc {
	type params struct {
		ID            string `json:"id"`
		DispatchPhase string `json:"dispatchPhase"`
		AttemptID     string `json:"attemptId"`
		Branch        string `json:"branch"`
		PRNumber      int    `json:"prNumber"`
		PRURL         string `json:"prUrl"`
		CommitSHA     string `json:"commitSha"`
		DeployHead    string `json:"deployHead"`
		OutcomeNote   string `json:"outcomeNote"`
		ReturnCode    *int   `json:"returnCode"`
		Ahead         *int   `json:"ahead"`
		PRState       string `json:"prState"`
	}
	return minibind.Bind[params](func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
		if strings.TrimSpace(p.ID) == "" {
			return rpcerr.MissingParam("id").Response(req.ID)
		}
		if strings.TrimSpace(p.AttemptID) == "" {
			return rpcerr.MissingParam("attemptId").Response(req.ID)
		}
		phase := strings.TrimSpace(p.DispatchPhase)
		if phase == "" {
			if p.ReturnCode == nil && strings.TrimSpace(p.PRState) == "" {
				return rpcerr.MissingParam("dispatchPhase or result facts").Response(req.ID)
			}
			returnCode := 0
			if p.ReturnCode != nil {
				returnCode = *p.ReturnCode
			}
			classified, err := rsilifecycle.ClassifyDispatchResult(rsilifecycle.DispatchFacts{
				ReturnCode: returnCode,
				Ahead:      p.Ahead,
				PRState:    p.PRState,
			})
			if err != nil {
				return rpcerr.WrapValidationFailed("self-correction result facts rejected", err).Response(req.ID)
			}
			phase = string(classified)
		}
		_, err := deps.RecordDispatch(genesis.SelfCorrectionCandidateRecord{
			ID:            p.ID,
			DispatchPhase: phase,
			AttemptID:     p.AttemptID,
			Branch:        p.Branch,
			PRNumber:      p.PRNumber,
			PRURL:         p.PRURL,
			CommitSHA:     p.CommitSHA,
			DeployHead:    p.DeployHead,
			OutcomeNote:   p.OutcomeNote,
		})
		if err != nil {
			return rpcerr.WrapValidationFailed("self-correction dispatch rejected", err).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, map[string]any{"ok": true, "id": strings.TrimSpace(p.ID), "dispatchPhase": phase})
	})
}

// selfImprovementCodingRecord files one PROPOSE-ONLY self-correction candidate.
// There is intentionally no status parameter: every record lands as proposed and
// can only move through the reviewed lifecycle (accept/reject/apply) elsewhere,
// so no RPC caller can inject a pre-approved edit.
func selfImprovementCodingRecord(deps SelfImprovementCodingDeps) rpcutil.HandlerFunc {
	type params struct {
		Scope          string   `json:"scope"`
		SkillName      string   `json:"skillName"`
		Title          string   `json:"title"`
		Candidate      string   `json:"candidate"`
		Evidence       string   `json:"evidence"`
		Reason         string   `json:"reason"`
		TargetFiles    []string `json:"targetFiles"`
		ProposedChange string   `json:"proposedChange"`
		Risk           string   `json:"risk"`
		Source         string   `json:"source"`
	}
	return minibind.Bind[params](func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
		// Provenance is mandatory: source-less rows are indistinguishable from
		// legacy captures (observed 2026-07-04) and can never graduate through
		// the dispatch allowlist.
		if strings.TrimSpace(p.Source) == "" {
			return rpcerr.MissingParam("source").Response(req.ID)
		}
		if strings.TrimSpace(p.Title) == "" && strings.TrimSpace(p.Candidate) == "" && strings.TrimSpace(p.ProposedChange) == "" {
			return rpcerr.InvalidParams(fmt.Errorf("candidate needs title, candidate, or proposedChange")).Response(req.ID)
		}
		rec, err := deps.RecordCandidate(genesis.SelfCorrectionCandidateRecord{
			Scope:          p.Scope,
			SkillName:      p.SkillName,
			Title:          p.Title,
			Candidate:      p.Candidate,
			Evidence:       p.Evidence,
			Reason:         p.Reason,
			TargetFiles:    p.TargetFiles,
			ProposedChange: p.ProposedChange,
			Risk:           p.Risk,
			Source:         p.Source,
		})
		if err != nil {
			// Includes record-time forbidden-surface rejections — the candidate
			// was refused, not lost; surface the cause verbatim to the caller.
			return rpcerr.WrapValidationFailed("self-correction record rejected", err).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, SelfImprovementCodingRecordResponse{
			OK:        true,
			Candidate: selfCorrectionCandidate(rec),
		})
	})
}

func selfImprovementCodingList(deps SelfImprovementCodingDeps) rpcutil.HandlerFunc {
	type params struct {
		Limit            int      `json:"limit"`
		Status           string   `json:"status"`
		DispatchableOnly bool     `json:"dispatchableOnly"`
		ExcludeIDs       []string `json:"excludeIds"`
	}
	return minibind.BindOptional[params](func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
		if p.Limit <= 0 || p.Limit > lifecycleScanLimit {
			p.Limit = 60
		}
		allRecs, err := deps.RecentCandidates("", lifecycleScanLimit)
		if err != nil {
			return rpcerr.WrapUnavailable("self-improvement coding queue unavailable", err).Response(req.ID)
		}
		var recs []genesis.SelfCorrectionCandidateRecord
		if p.DispatchableOnly {
			if selected, ok := genesis.SelectSelfCorrectionDispatchCandidate(allRecs, p.ExcludeIDs); ok {
				recs = []genesis.SelfCorrectionCandidateRecord{selected}
			}
		} else {
			status, normalizeErr := normalizeSelfImprovementCodingStatus(p.Status)
			if normalizeErr != nil {
				return rpcerr.InvalidParams(normalizeErr).Response(req.ID)
			}
			recs = filterSelfImprovementCodingRecords(allRecs, status, p.Limit)
		}
		candidates := make([]SelfCorrectionCandidate, 0, len(recs))
		for _, rec := range recs {
			candidates = append(candidates, selfCorrectionCandidate(rec))
		}
		response := SelfImprovementCodingListResponse{
			Candidates: candidates,
			Count:      len(candidates),
		}
		if !p.DispatchableOnly {
			response.StatusCounts = selfImprovementCodingStatusCounts(allRecs)
			response.Funnel = selfImprovementCodingFunnel(deps)
		}
		return rpcutil.RespondOK(req.ID, response)
	})
}

func filterSelfImprovementCodingRecords(
	records []genesis.SelfCorrectionCandidateRecord,
	status string,
	limit int,
) []genesis.SelfCorrectionCandidateRecord {
	out := make([]genesis.SelfCorrectionCandidateRecord, 0, min(limit, len(records)))
	for _, record := range records {
		if status != "" && strings.TrimSpace(record.Status) != status {
			continue
		}
		out = append(out, record)
		if len(out) >= limit {
			break
		}
	}
	return out
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
			Proposed7d:             f.Proposed7d,
			Verdicted7d:            f.Verdicted7d,
			Applied7d:              f.Applied7d,
			ConversionRate:         f.ConversionRate,
			MeanTimeToVerdictMs:    f.MeanTimeToVerdictMs,
			Reopens7d:              f.Reopens7d,
			PendingCount:           f.PendingCount,
			OldestPendingAgeMs:     f.OldestPendingAgeMs,
			Dispatched7d:           f.Dispatched7d,
			WatchPassed7d:          f.WatchPassed7d,
			RolledBack7d:           f.RolledBack7d,
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
		Title:          textutil.TruncateRunes(rec.Title, lifecycleTextMaxRunes, "…"),
		Candidate:      textutil.TruncateRunes(rec.Candidate, lifecycleTextMaxRunes, "…"),
		Evidence:       textutil.TruncateRunes(rec.Evidence, lifecycleTextMaxRunes, "…"),
		Reason:         textutil.TruncateRunes(rec.Reason, lifecycleTextMaxRunes, "…"),
		TargetFiles:    rec.TargetFiles,
		ProposedChange: textutil.TruncateRunes(rec.ProposedChange, lifecycleTextMaxRunes, "…"),
		Risk:           textutil.TruncateRunes(rec.Risk, lifecycleTextMaxRunes, "…"),
		Source:         rec.Source,
		Surface:        rec.Surface,
		AutoDispatch:   rec.Scope == "code" && genesis.SourceAutoDispatches(rec.Source),
		Reviewer:       rec.Reviewer,
		ReviewNote:     textutil.TruncateRunes(rec.ReviewNote, lifecycleTextMaxRunes, "…"),
		EvidenceKinds:  selfCorrectionEvidenceKinds(rec),
		ReviewActions:  selfCorrectionReviewActions(rec),
		DispatchPhase:  rec.DispatchPhase,
		AttemptID:      rec.AttemptID,
		Branch:         rec.Branch,
		PRNumber:       rec.PRNumber,
		PRURL:          rec.PRURL,
		CommitSHA:      rec.CommitSHA,
		DeployHead:     rec.DeployHead,
		OutcomeNote:    textutil.TruncateRunes(rec.OutcomeNote, lifecycleTextMaxRunes, "…"),
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

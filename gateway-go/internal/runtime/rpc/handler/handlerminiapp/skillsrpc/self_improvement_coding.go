package skillsrpc

import (
	"context"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/core/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	rsilifecycle "github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/lifecycle"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/opstranslate"
	miniappcontract "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp/contract"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/minibind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
	"github.com/choiceoh/deneb/gateway-go/pkg/textutil"
)

const (
	selfCorrectionStatusRejected   = string(rsilifecycle.ReviewRejected)
	selfCorrectionStatusSuperseded = string(rsilifecycle.ReviewSuperseded)
)

// Wire type aliases — see handlerminiapp/self_improvement_coding_contract.go.
type (
	SelfCorrectionCandidate             = miniappcontract.SelfCorrectionCandidate
	SelfCorrectionImpactContract        = miniappcontract.SelfCorrectionImpactContract
	SelfCorrectionImpactResult          = miniappcontract.SelfCorrectionImpactResult
	SelfImprovementCodingStatusCount    = miniappcontract.SelfImprovementCodingStatusCount
	SelfImprovementCodingFunnel         = miniappcontract.SelfImprovementCodingFunnel
	SelfImprovementCodingListResponse   = miniappcontract.SelfImprovementCodingListResponse
	SelfImprovementCodingRecordResponse = miniappcontract.SelfImprovementCodingRecordResponse
)

// SelfImprovementCodingDeps wires the native "자가개선 코딩" settings section to
// Propus' deferred self-correction queue. These rows are not skills and not
// lifecycle log events; they are unapplied coding hypotheses for batch review.
type SelfImprovementCodingDeps struct {
	RecentCandidates func(status string, limit int) ([]genesis.SelfCorrectionCandidateRecord, error)
	// NextDispatchCandidate selects across the complete merged ledger. It must
	// not inherit the bounded history used by list/status views.
	NextDispatchCandidate func(excludedIDs []string) (genesis.SelfCorrectionCandidateRecord, bool, error)
	// RecordCandidate appends one propose-only candidate through the genesis
	// tracker — the queue's single writer, which enforces the forbidden-surface
	// list at record time. Optional: without it the record method is absent.
	RecordCandidate func(genesis.SelfCorrectionCandidateRecord) (genesis.SelfCorrectionCandidateRecord, error)
	// RecordDispatch appends one authoritative delivery event (started -> PR ->
	// merged -> deployed -> watched/rolled back). Optional for read-only servers.
	RecordDispatch func(genesis.SelfCorrectionCandidateRecord) (genesis.SelfCorrectionCandidateRecord, error)
	// RecordImpact appends a deterministic usefulness verdict after the exact
	// dispatch attempt reaches watch_passed.
	RecordImpact func(genesis.SelfCorrectionCandidateRecord) (genesis.SelfCorrectionCandidateRecord, error)
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
	// record / dispatch / impact are CLI-and-script-only writers (the L4 miners
	// under scripts/audit/ and scripts/dev/self_correction_dispatch.py reach them
	// with the client token); no native client calls them — .list is the only
	// client-facing read. Kept on the miniapp surface on purpose: the token gate
	// is what lets out-of-process tooling write to the queue at all.
	if deps.RecordCandidate != nil {
		methods["miniapp.self_improvement_coding.record"] = selfImprovementCodingRecord(deps)
	}
	if deps.RecordDispatch != nil {
		methods["miniapp.self_improvement_coding.dispatch"] = selfImprovementCodingDispatch(deps)
	}
	if deps.RecordImpact != nil {
		methods["miniapp.self_improvement_coding.impact"] = selfImprovementCodingImpact(deps)
	}
	return methods
}

// selfImprovementCodingImpact records observations only; the tracker owns the
// terminal usefulness classification and rejects pre-watch or stale attempts.
func selfImprovementCodingImpact(deps SelfImprovementCodingDeps) rpcutil.HandlerFunc {
	type params struct {
		ID                  string   `json:"id"`
		AttemptID           string   `json:"attemptId"`
		Observed            float64  `json:"observed"`
		Samples             int      `json:"samples"`
		GuardrailViolations []string `json:"guardrailViolations"`
		Note                string   `json:"note"`
	}
	return minibind.Bind[params](func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
		if strings.TrimSpace(p.ID) == "" {
			return rpcerr.MissingParam("id").Response(req.ID)
		}
		if strings.TrimSpace(p.AttemptID) == "" {
			return rpcerr.MissingParam("attemptId").Response(req.ID)
		}
		record, err := deps.RecordImpact(genesis.SelfCorrectionCandidateRecord{
			ID:        p.ID,
			AttemptID: p.AttemptID,
			ImpactResult: &rsilifecycle.ImpactResult{
				Observed:            p.Observed,
				Samples:             p.Samples,
				GuardrailViolations: p.GuardrailViolations,
				Note:                p.Note,
			},
		})
		if err != nil {
			return rpcerr.WrapValidationFailed("self-correction impact rejected", err).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, map[string]any{
			"ok": true, "id": strings.TrimSpace(p.ID), "impactStatus": record.ImpactResult.Status,
		})
	})
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
		Scope          string                        `json:"scope"`
		SkillName      string                        `json:"skillName"`
		Title          string                        `json:"title"`
		Candidate      string                        `json:"candidate"`
		Evidence       string                        `json:"evidence"`
		Reason         string                        `json:"reason"`
		TargetFiles    []string                      `json:"targetFiles"`
		ProposedChange string                        `json:"proposedChange"`
		Risk           string                        `json:"risk"`
		Source         string                        `json:"source"`
		ImpactContract *SelfCorrectionImpactContract `json:"impactContract"`
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
			ImpactContract: genesisImpactContract(p.ImpactContract),
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
		// Translate renders the review prose into Korean for a human reader.
		// OPT-IN on purpose. The native clients ask for it; the L4 miners and
		// scripts/dev/self_correction_dispatch.py must not get it, because the
		// candidate they read is fed to a coding agent as its instructions —
		// translating those would silently rewrite what the agent is told to do.
		// The dispatchableOnly selector ignores this flag for the same reason.
		Translate bool `json:"translate"`
	}
	return minibind.BindOptional[params](func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
		if p.DispatchableOnly {
			if deps.NextDispatchCandidate == nil {
				return rpcerr.Unavailable("self-improvement dispatch selector unavailable").Response(req.ID)
			}
			selected, ok, err := deps.NextDispatchCandidate(p.ExcludeIDs)
			if err != nil {
				return rpcerr.WrapUnavailable("self-improvement coding queue unavailable", err).Response(req.ID)
			}
			if !ok {
				return rpcutil.RespondOK(req.ID, SelfImprovementCodingListResponse{})
			}
			return rpcutil.RespondOK(req.ID, SelfImprovementCodingListResponse{
				Candidates: []SelfCorrectionCandidate{selfCorrectionCandidate(selected)},
				Count:      1,
			})
		}

		if p.Limit <= 0 || p.Limit > lifecycleScanLimit {
			p.Limit = 60
		}
		status, normalizeErr := normalizeSelfImprovementCodingStatus(p.Status)
		if normalizeErr != nil {
			return rpcerr.InvalidParams(normalizeErr).Response(req.ID)
		}
		allRecs, err := deps.RecentCandidates("", lifecycleScanLimit)
		if err != nil {
			return rpcerr.WrapUnavailable("self-improvement coding queue unavailable", err).Response(req.ID)
		}
		recs := filterSelfImprovementCodingRecords(allRecs, status, p.Limit)
		candidates := make([]SelfCorrectionCandidate, 0, len(recs))
		for _, rec := range recs {
			candidates = append(candidates, selfCorrectionCandidate(rec))
		}
		if p.Translate {
			translateSelfCorrectionProse(ctx, candidates)
		}
		return rpcutil.RespondOK(req.ID, SelfImprovementCodingListResponse{
			Candidates:   candidates,
			Count:        len(candidates),
			StatusCounts: selfImprovementCodingStatusCounts(allRecs),
			Funnel:       selfImprovementCodingFunnel(deps),
		})
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
			InfraRejections7d:      f.InfraRejections7d,
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

// translateSelfCorrectionProse renders the operator-facing review text into
// Korean in place. Measured over the live queue on 2026-09-03: 78% of titles and
// 71% of reasons carried no Hangul at all, because the review models answer in
// English.
//
// Evidence is deliberately NOT translated. It is the raw proof an operator
// judges the candidate by — telemetry lines like
// "observe.behavior 7d vs 30d baseline: mail_archive calls=113 avgMs=750" and
// session keys. Machine-translating a measurement is how a review screen starts
// lying about numbers; English proof beats mangled proof.
// translateProseFn is a package var so tests can pin the WHICH-fields and
// WHICH-requests contract without a live translator.
var translateProseFn = opstranslate.Fields

func translateSelfCorrectionProse(ctx context.Context, candidates []SelfCorrectionCandidate) {
	if len(candidates) == 0 {
		return
	}
	// Pointers rather than index arithmetic: the input and output slices must
	// stay aligned, and off-by-one here would put one candidate's reason on
	// another's title.
	fields := make([]*string, 0, len(candidates)*6)
	for i := range candidates {
		c := &candidates[i]
		fields = append(
			fields,
			&c.Title, &c.Candidate, &c.Reason,
			&c.ProposedChange, &c.Risk, &c.ReviewNote, &c.OutcomeNote,
		)
	}
	texts := make([]string, len(fields))
	for i, p := range fields {
		texts[i] = *p
	}
	rendered := translateProseFn(ctx, texts)
	if len(rendered) != len(fields) {
		return // never partially apply a misaligned result
	}
	for i, p := range fields {
		*p = rendered[i]
	}
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
		AutoDispatch:   rec.Scope == "code" && genesis.CandidateAutoDispatches(rec),
		Reviewer:       rec.Reviewer,
		ReviewNote:     textutil.TruncateRunes(rec.ReviewNote, lifecycleTextMaxRunes, "…"),
		ImpactContract: selfCorrectionImpactContract(rec.ImpactContract),
		ImpactResult:   selfCorrectionImpactResult(rec),
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

func genesisImpactContract(contract *SelfCorrectionImpactContract) *rsilifecycle.ImpactContract {
	if contract == nil {
		return nil
	}
	return &rsilifecycle.ImpactContract{
		Metric: contract.Metric, Direction: contract.Direction,
		Baseline: contract.Baseline, Target: contract.Target,
		MinSamples: contract.MinSamples, ObservationWindowMs: contract.ObservationWindowMs,
		Guardrails: contract.Guardrails,
	}
}

func selfCorrectionImpactContract(contract *rsilifecycle.ImpactContract) *SelfCorrectionImpactContract {
	if contract == nil {
		return nil
	}
	return &SelfCorrectionImpactContract{
		Metric: contract.Metric, Direction: contract.Direction,
		Baseline: contract.Baseline, Target: contract.Target,
		MinSamples: contract.MinSamples, ObservationWindowMs: contract.ObservationWindowMs,
		Guardrails: contract.Guardrails,
	}
}

func selfCorrectionImpactResult(rec genesis.SelfCorrectionCandidateRecord) *SelfCorrectionImpactResult {
	status := ""
	if rec.ImpactResult != nil {
		status = rec.ImpactResult.Status
	} else if rec.ImpactContract != nil && rsilifecycle.NormalizeDelivery(rec.DispatchPhase) == rsilifecycle.DeliveryWatchPassed {
		status = "pending"
	}
	if status == "" {
		return nil
	}
	if rec.ImpactResult == nil {
		return &SelfCorrectionImpactResult{Status: status}
	}
	return &SelfCorrectionImpactResult{
		Status: status, Observed: rec.ImpactResult.Observed, Samples: rec.ImpactResult.Samples,
		GuardrailViolations: rec.ImpactResult.GuardrailViolations,
		Note:                textutil.TruncateRunes(rec.ImpactResult.Note, lifecycleTextMaxRunes, "…"),
		CheckedAt:           rec.ImpactResult.CheckedAt,
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
		return selfCorrectionStatusRejected, nil
	case "supersede", "superseded":
		return selfCorrectionStatusSuperseded, nil
	case "apply", "applied":
		return genesis.SelfCorrectionStatusApplied, nil
	default:
		return "", fmt.Errorf("unknown self-improvement coding status %q", status)
	}
}

func selfImprovementCodingStatusCounts(recs []genesis.SelfCorrectionCandidateRecord) []SelfImprovementCodingStatusCount {
	counts := map[string]int{
		"all":                                len(recs),
		genesis.SelfCorrectionStatusProposed: 0,
		genesis.SelfCorrectionStatusAccepted: 0,
		genesis.SelfCorrectionStatusApplied:  0,
		selfCorrectionStatusRejected:         0,
		selfCorrectionStatusSuperseded:       0,
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
		selfCorrectionStatusRejected,
		selfCorrectionStatusSuperseded,
		"all",
	}
	out := make([]SelfImprovementCodingStatusCount, 0, len(order))
	for _, status := range order {
		out = append(out, SelfImprovementCodingStatusCount{Status: status, Count: counts[status]})
	}
	return out
}

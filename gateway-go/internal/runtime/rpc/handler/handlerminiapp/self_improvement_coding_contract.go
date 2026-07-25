package handlerminiapp

// SelfCorrectionImpactContract declares the metric that must improve after a
// safely deployed L4 change.
//
//deneb:wire
type SelfCorrectionImpactContract struct {
	Metric              string   `json:"metric"`
	Direction           string   `json:"direction"`
	Baseline            float64  `json:"baseline"`
	Target              float64  `json:"target"`
	MinSamples          int      `json:"minSamples"`
	ObservationWindowMs int64    `json:"observationWindowMs,omitempty"`
	Guardrails          []string `json:"guardrails,omitempty"`
}

// SelfCorrectionImpactResult is the usefulness verdict independent of the
// delivery rollback watch. pending is a derived, non-terminal state.
//
//deneb:wire
type SelfCorrectionImpactResult struct {
	Status              string   `json:"status"`
	Observed            float64  `json:"observed,omitempty"`
	Samples             int      `json:"samples,omitempty"`
	GuardrailViolations []string `json:"guardrailViolations,omitempty"`
	Note                string   `json:"note,omitempty"`
	CheckedAt           int64    `json:"checkedAt,omitempty"`
}

// SelfCorrectionCandidate is one pending deferred correction from the
// append-only self-correction queue. Behavior lives in the skillsrpc
// subpackage; this file stays the client generator's source of truth for the
// //deneb:wire types.
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
	// Surface is the declared editable-surface tier summarizing TargetFiles
	// (auto-apply / propose-only). Empty target lists default to propose-only.
	Surface string `json:"surface,omitempty"`
	// AutoDispatch is true when this candidate's source is graduated into the
	// coding-dispatch allowlist — it auto-implements + lands through the gate
	// stack rather than waiting for review. Clients label it 자동수리 vs 검토 대기.
	AutoDispatch   bool                          `json:"autoDispatch,omitempty"`
	Reviewer       string                        `json:"reviewer,omitempty"`
	ReviewNote     string                        `json:"reviewNote,omitempty"`
	ImpactContract *SelfCorrectionImpactContract `json:"impactContract,omitempty"`
	ImpactResult   *SelfCorrectionImpactResult   `json:"impactResult,omitempty"`
	EvidenceKinds  []string                      `json:"evidenceKinds,omitempty"`
	ReviewActions  []string                      `json:"reviewActions,omitempty"`
	DispatchPhase  string                        `json:"dispatchPhase,omitempty"`
	AttemptID      string                        `json:"attemptId,omitempty"`
	Branch         string                        `json:"branch,omitempty"`
	PRNumber       int                           `json:"prNumber,omitempty"`
	PRURL          string                        `json:"prUrl,omitempty"`
	CommitSHA      string                        `json:"commitSha,omitempty"`
	DeployHead     string                        `json:"deployHead,omitempty"`
	OutcomeNote    string                        `json:"outcomeNote,omitempty"`
	CreatedAt      int64                         `json:"createdAt,omitempty"`
	UpdatedAt      int64                         `json:"updatedAt,omitempty"`
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
	LastCaptureAt int64 `json:"lastCaptureAt,omitempty"`
	LastReviewAt  int64 `json:"lastReviewAt,omitempty"`
	Rejections7d  int   `json:"rejections7d,omitempty"`
	// InfraRejections7d counts evolve rejections that were OUTAGES, not verdicts
	// (judge call errored, teacher rewrite produced nothing). Split out of
	// Rejections7d so a judge outage cannot read as "the gates are rejecting
	// more"; exposed rather than merely excluded because a spike here is an
	// availability signal someone needs to see.
	InfraRejections7d      int   `json:"infraRejections7d,omitempty"`
	PromotableRejections7d int   `json:"promotableRejections7d,omitempty"`
	LastRejectionAt        int64 `json:"lastRejectionAt,omitempty"`
	LastNudgeAt            int64 `json:"lastNudgeAt,omitempty"`
	// ── Closure side (loop-closing health) ──
	Proposed7d          int     `json:"proposed7d,omitempty"`
	Verdicted7d         int     `json:"verdicted7d,omitempty"`
	Applied7d           int     `json:"applied7d,omitempty"`
	ConversionRate      float64 `json:"conversionRate,omitempty"`
	MeanTimeToVerdictMs int64   `json:"meanTimeToVerdictMs,omitempty"`
	Reopens7d           int     `json:"reopens7d,omitempty"`
	PendingCount        int     `json:"pendingCount,omitempty"`
	OldestPendingAgeMs  int64   `json:"oldestPendingAgeMs,omitempty"`
	Dispatched7d        int     `json:"dispatched7d,omitempty"`
	WatchPassed7d       int     `json:"watchPassed7d,omitempty"`
	RolledBack7d        int     `json:"rolledBack7d,omitempty"`
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

// SelfImprovementCodingRecordResponse is the miniapp.self_improvement_coding.record
// payload. The callers today are script-side miners (e.g.
// scripts/audit/health-finding-miner.py); native clients read the queue via .list.
//
//deneb:wire
type SelfImprovementCodingRecordResponse struct {
	OK        bool                    `json:"ok"`
	Candidate SelfCorrectionCandidate `json:"candidate"`
}

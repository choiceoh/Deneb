package briefcase

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/briefcase/runcontract"
)

const (
	SupervisorPlanSchemaVersion   = "deneb-briefcase-supervisor/v1"
	SupervisorResultSchemaVersion = "deneb-briefcase-supervisor-result/v1"
	MaxSupervisorCycles           = 100
)

var (
	ErrInvalidSupervisorPlan = errors.New("invalid briefcase supervisor plan")
	ErrSupervisorTerminal    = errors.New("briefcase supervisor is terminal")
)

// SupervisorDecision is the only decision surface exposed to the supervised
// agent or an external runner. Rubric-level diagnostics stay on the separate
// SupervisorHiddenDiagnostics surface.
type SupervisorDecision string

const (
	SupervisorPass     SupervisorDecision = "PASS"
	SupervisorContinue SupervisorDecision = "CONTINUE"
	SupervisorFail     SupervisorDecision = "FAIL"
)

// SupervisorFingerprint binds a sealed supervisor plan to the RunResult fields
// that the supervisor can verify. Empty optional strings and a zero seed act as
// wildcards; CaseID is always required.
type SupervisorFingerprint struct {
	CaseID                 string `json:"caseId"`
	CasepackSHA256         string `json:"casepackSha256,omitempty"`
	Seed                   int64  `json:"seed,omitempty"`
	Model                  string `json:"model,omitempty"`
	ProviderModel          string `json:"providerModel,omitempty"`
	Arm                    string `json:"arm,omitempty"`
	APIMode                string `json:"apiMode,omitempty"`
	RecallMode             string `json:"recallMode,omitempty"`
	DevicePlanSHA256       string `json:"devicePlanSha256,omitempty"`
	DevicePlanSourceSHA256 string `json:"devicePlanSourceSha256,omitempty"`
	ToolSchemaSHA256       string `json:"toolSchemaSha256,omitempty"`
	EndpointSHA256         string `json:"endpointSha256,omitempty"`
	BuildSHA256            string `json:"buildSha256,omitempty"`
	ExecutionProfileSHA256 string `json:"executionProfileSha256,omitempty"`
}

// SupervisorCheckpoint is the deterministic rubric applied after one cycle.
// Cycles in a valid plan are contiguous from 1 through MaxCycles.
type SupervisorCheckpoint struct {
	Cycle  int     `json:"cycle"`
	Checks []Check `json:"checks"`
}

// SupervisorPlan is stored in the sealed case area. PlanDigest authenticates
// the typed plan with this field blank, matching the content-addressed manifest
// pattern used by Deneb-Briefcase casepacks.
type SupervisorPlan struct {
	SchemaVersion string                `json:"schemaVersion"`
	PlanDigest    string                `json:"planDigest"`
	Fingerprint   SupervisorFingerprint `json:"fingerprint"`
	MaxCycles     int                   `json:"maxCycles"`
	PassThreshold float64               `json:"passThreshold,omitempty"`
	// FeedbackDenyTokens are explicit answer/canary fragments that must never
	// cross the supervisor-to-simulator firewall. Multi-cycle plans require at
	// least one so partial gold answers are not inferred heuristically.
	FeedbackDenyTokens []string               `json:"feedbackDenyTokens"`
	Checkpoints        []SupervisorCheckpoint `json:"checkpoints"`
}

// SupervisorPlanDigest returns the SHA-256 of the typed plan with PlanDigest
// blank. Checkpoint order is semantic and therefore remains significant.
func SupervisorPlanDigest(plan SupervisorPlan) (string, error) {
	plan.PlanDigest = ""
	data, err := json.Marshal(plan)
	if err != nil {
		return "", fmt.Errorf("%w: encode plan: %w", ErrInvalidSupervisorPlan, err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// SetSupervisorPlanDigest updates a plan before it is written into sealed
// storage. A loader or runner should use NewSupervisor to verify it.
func SetSupervisorPlanDigest(plan *SupervisorPlan) (string, error) {
	if plan == nil {
		return "", fmt.Errorf("%w: nil plan", ErrInvalidSupervisorPlan)
	}
	digest, err := SupervisorPlanDigest(*plan)
	if err != nil {
		return "", err
	}
	plan.PlanDigest = digest
	return digest, nil
}

// SupervisorPublicResult is deliberately coarse. It contains no check IDs,
// statuses, expected values, grader report, or hidden reason codes.
type SupervisorPublicResult struct {
	SchemaVersion string             `json:"schemaVersion"`
	Decision      SupervisorDecision `json:"decision"`
	Cycle         int                `json:"cycle"`
	MaxCycles     int                `json:"maxCycles"`
	Score         float64            `json:"score"`
	Recoverable   bool               `json:"recoverable"`
	BestCycle     int                `json:"bestCycle,omitempty"`
	BestScore     float64            `json:"bestScore"`
	Summary       string             `json:"summary"`
}

// SupervisorHiddenReason is trusted-side diagnostic metadata. It must never be
// copied into SupervisorPublicResult.Summary.
type SupervisorHiddenReason string

const (
	HiddenPassed         SupervisorHiddenReason = "passed"
	HiddenBelowThreshold SupervisorHiddenReason = "below_threshold"
	HiddenCriticalGate   SupervisorHiddenReason = "critical_gate_failed"
	HiddenInvalidGrade   SupervisorHiddenReason = "invalid_grade"
	HiddenRunMismatch    SupervisorHiddenReason = "run_fingerprint_mismatch"
	HiddenCycleLimit     SupervisorHiddenReason = "cycle_limit_reached"
)

type SupervisorCycleDiagnostic struct {
	Cycle    int
	RunID    string
	Decision SupervisorDecision
	Reason   SupervisorHiddenReason
	Report   Report
}

// SupervisorHiddenDiagnostics is available only from the trusted Supervisor
// object. The external public DTO has no field referencing this type.
type SupervisorHiddenDiagnostics struct {
	PlanDigest string
	Terminal   bool
	BestCycle  int
	BestScore  float64
	Cycles     []SupervisorCycleDiagnostic
}

// Supervisor evaluates one checkpoint per call and retains only trusted-side
// diagnostics. It is safe for concurrent callers; calls serialize into the
// signed checkpoint order.
type Supervisor struct {
	mu sync.Mutex

	plan             SupervisorPlan
	threshold        float64
	nextCycle        int
	terminal         bool
	history          []SupervisorCycleDiagnostic
	hasBest          bool
	bestCycle        int
	bestScore        float64
	artifactMaxBytes map[string]int64
}

func NewSupervisor(plan SupervisorPlan) (*Supervisor, error) {
	return NewSupervisorWithArtifactLimits(plan, nil)
}

func NewSupervisorWithArtifactLimits(plan SupervisorPlan, limits map[string]int64) (*Supervisor, error) {
	threshold, err := validateSupervisorPlan(plan)
	if err != nil {
		return nil, err
	}
	return &Supervisor{
		plan:             cloneSupervisorPlan(plan),
		threshold:        threshold,
		nextCycle:        1,
		history:          make([]SupervisorCycleDiagnostic, 0, plan.MaxCycles),
		artifactMaxBytes: cloneArtifactLimits(limits),
	}, nil
}

func cloneArtifactLimits(limits map[string]int64) map[string]int64 {
	if len(limits) == 0 {
		return nil
	}
	out := make(map[string]int64, len(limits))
	for path, limit := range limits {
		out[path] = limit
	}
	return out
}

// Evaluate grades the next signed checkpoint against a complete RunResult.
// A critical failure, invalid grade, or run fingerprint mismatch is terminal.
// A valid non-critical miss returns CONTINUE until MaxCycles, then FAIL.
func (s *Supervisor) Evaluate(run *runcontract.RunResult) (SupervisorPublicResult, error) {
	return s.EvaluateContext(context.Background(), run)
}

// EvaluateContext evaluates one checkpoint without committing any supervisor
// state if the context is canceled during grading.
func (s *Supervisor) EvaluateContext(ctx context.Context, run *runcontract.RunResult) (SupervisorPublicResult, error) {
	if s == nil {
		return SupervisorPublicResult{}, ErrSupervisorTerminal
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return SupervisorPublicResult{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return SupervisorPublicResult{}, err
	}
	if s.terminal {
		return SupervisorPublicResult{}, ErrSupervisorTerminal
	}

	cycle := s.nextCycle
	checkpoint := s.plan.Checkpoints[cycle-1]
	mismatchReason := runMismatch(s.plan.Fingerprint, run)
	if err := ctx.Err(); err != nil {
		return SupervisorPublicResult{}, err
	}
	if mismatchReason != "" {
		diagnostic := SupervisorCycleDiagnostic{
			Cycle:    cycle,
			Decision: SupervisorFail,
			Reason:   HiddenRunMismatch,
		}
		if run != nil {
			diagnostic.RunID = run.RunID
		}
		s.history = append(s.history, diagnostic)
		s.terminal = true
		return s.publicResult(cycle, SupervisorFail, 0, false, "checkpoint could not be verified"), nil
	}

	evidence := Evidence{
		Text:             latestExecutorText(run),
		ArtifactRoot:     run.ArtifactRoot,
		State:            bytes.Clone(run.State),
		ArtifactMaxBytes: cloneArtifactLimits(s.artifactMaxBytes),
	}
	report, err := GradeContext(ctx, Plan{
		Fingerprint:   FingerprintFromRun(run),
		PassThreshold: s.threshold,
		Checks:        cloneChecks(checkpoint.Checks),
	}, evidence)
	if err != nil {
		return SupervisorPublicResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return SupervisorPublicResult{}, err
	}
	score := clampScore(report.Score)

	// Best-so-far is the highest trustworthy checkpoint score, independent of
	// the terminal decision. Critical gates still control PASS/FAIL, but a
	// valid partial score remains useful when a run times out or fails later.
	if report.Status != StatusInvalid {
		s.considerBest(cycle, score)
	}

	decision := SupervisorContinue
	recoverable := true
	summary := "checkpoint requires another cycle"
	reason := HiddenBelowThreshold
	switch {
	case report.Status == StatusInvalid:
		decision, recoverable = SupervisorFail, false
		summary, reason = "checkpoint evaluation failed", HiddenInvalidGrade
		s.terminal = true
	case !report.CriticalPassed:
		decision, recoverable = SupervisorFail, false
		summary, reason = "checkpoint failed a required gate", HiddenCriticalGate
		s.terminal = true
	case report.Status == StatusPass:
		decision, recoverable = SupervisorPass, false
		summary, reason = "checkpoint passed", HiddenPassed
		s.terminal = true
	case cycle >= s.plan.MaxCycles:
		decision, recoverable = SupervisorFail, false
		summary, reason = "checkpoint did not pass within the cycle limit", HiddenCycleLimit
		s.terminal = true
	default:
		s.nextCycle++
	}

	s.history = append(s.history, SupervisorCycleDiagnostic{
		Cycle: cycle, RunID: run.RunID, Decision: decision, Reason: reason,
		Report: cloneReport(report),
	})
	return s.publicResult(cycle, decision, score, recoverable, summary), nil
}

func latestExecutorText(run *runcontract.RunResult) string {
	return runcontract.LatestExecutorText(run)
}

// HiddenDiagnostics returns a defensive copy of all trusted-side reports.
func (s *Supervisor) HiddenDiagnostics() SupervisorHiddenDiagnostics {
	if s == nil {
		return SupervisorHiddenDiagnostics{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cycles := make([]SupervisorCycleDiagnostic, len(s.history))
	for i, diagnostic := range s.history {
		cycles[i] = diagnostic
		cycles[i].Report = cloneReport(diagnostic.Report)
	}
	return SupervisorHiddenDiagnostics{
		PlanDigest: s.plan.PlanDigest,
		Terminal:   s.terminal,
		BestCycle:  s.bestCycle,
		BestScore:  s.bestScore,
		Cycles:     cycles,
	}
}

func (s *Supervisor) considerBest(cycle int, score float64) {
	if !s.hasBest || score > s.bestScore {
		s.hasBest = true
		s.bestCycle = cycle
		s.bestScore = score
	}
	// Equal scores deliberately keep the earliest cycle.
}

func (s *Supervisor) publicResult(cycle int, decision SupervisorDecision, score float64, recoverable bool, summary string) SupervisorPublicResult {
	return SupervisorPublicResult{
		SchemaVersion: SupervisorResultSchemaVersion,
		Decision:      decision,
		Cycle:         cycle,
		MaxCycles:     s.plan.MaxCycles,
		Score:         clampScore(score),
		Recoverable:   recoverable,
		BestCycle:     s.bestCycle,
		BestScore:     clampScore(s.bestScore),
		Summary:       summary,
	}
}

func validateSupervisorPlan(plan SupervisorPlan) (float64, error) {
	if plan.SchemaVersion != SupervisorPlanSchemaVersion {
		return 0, fmt.Errorf("%w: unsupported schema version %q", ErrInvalidSupervisorPlan, plan.SchemaVersion)
	}
	if !lowerSHA256(plan.PlanDigest) {
		return 0, fmt.Errorf("%w: planDigest must be lowercase SHA-256", ErrInvalidSupervisorPlan)
	}
	want, err := SupervisorPlanDigest(plan)
	if err != nil {
		return 0, err
	}
	if plan.PlanDigest != want {
		return 0, fmt.Errorf("%w: planDigest mismatch", ErrInvalidSupervisorPlan)
	}
	if strings.TrimSpace(plan.Fingerprint.CaseID) == "" {
		return 0, fmt.Errorf("%w: fingerprint.caseId is required", ErrInvalidSupervisorPlan)
	}
	if plan.Fingerprint.Seed < 0 {
		return 0, fmt.Errorf("%w: fingerprint.seed must not be negative", ErrInvalidSupervisorPlan)
	}
	for name, digest := range map[string]string{
		"casepackSha256":         plan.Fingerprint.CasepackSHA256,
		"devicePlanSha256":       plan.Fingerprint.DevicePlanSHA256,
		"devicePlanSourceSha256": plan.Fingerprint.DevicePlanSourceSHA256,
		"toolSchemaSha256":       plan.Fingerprint.ToolSchemaSHA256,
		"endpointSha256":         plan.Fingerprint.EndpointSHA256,
		"buildSha256":            plan.Fingerprint.BuildSHA256,
		"executionProfileSha256": plan.Fingerprint.ExecutionProfileSHA256,
	} {
		if digest != "" && !lowerSHA256(digest) {
			return 0, fmt.Errorf("%w: fingerprint.%s must be lowercase SHA-256", ErrInvalidSupervisorPlan, name)
		}
	}
	if arm := plan.Fingerprint.Arm; arm != "" && arm != string(runcontract.ArmRawPrimary) && arm != string(runcontract.ArmMemoryAssisted) {
		return 0, fmt.Errorf("%w: unsupported fingerprint arm %q", ErrInvalidSupervisorPlan, arm)
	}
	if mode := plan.Fingerprint.APIMode; mode != "" && mode != "openai" && mode != "anthropic" {
		return 0, fmt.Errorf("%w: unsupported fingerprint apiMode %q", ErrInvalidSupervisorPlan, mode)
	}
	if mode := plan.Fingerprint.RecallMode; mode != "" && mode != "enabled" && mode != "disabled" {
		return 0, fmt.Errorf("%w: unsupported fingerprint recallMode %q", ErrInvalidSupervisorPlan, mode)
	}
	threshold := plan.PassThreshold
	if threshold == 0 {
		threshold = 1
	}
	if threshold <= 0 || threshold > 1 || math.IsNaN(threshold) || math.IsInf(threshold, 0) {
		return 0, fmt.Errorf("%w: passThreshold must be in (0, 1] or zero for the strict default", ErrInvalidSupervisorPlan)
	}
	if plan.MaxCycles <= 0 || plan.MaxCycles > MaxSupervisorCycles {
		return 0, fmt.Errorf("%w: maxCycles must be between 1 and %d", ErrInvalidSupervisorPlan, MaxSupervisorCycles)
	}
	if len(plan.Checkpoints) != plan.MaxCycles {
		return 0, fmt.Errorf("%w: checkpoints must cover every cycle", ErrInvalidSupervisorPlan)
	}
	if plan.MaxCycles > 1 && len(plan.FeedbackDenyTokens) == 0 {
		return 0, fmt.Errorf("%w: multi-cycle plans require feedbackDenyTokens", ErrInvalidSupervisorPlan)
	}
	if len(plan.FeedbackDenyTokens) > 256 {
		return 0, fmt.Errorf("%w: feedbackDenyTokens exceeds 256 entries", ErrInvalidSupervisorPlan)
	}
	seenDenyTokens := make(map[string]struct{}, len(plan.FeedbackDenyTokens))
	for _, token := range plan.FeedbackDenyTokens {
		trimmed := strings.TrimSpace(token)
		if trimmed == "" || len([]rune(trimmed)) > 2_000 {
			return 0, fmt.Errorf("%w: feedbackDenyTokens must contain 1..2000 runes", ErrInvalidSupervisorPlan)
		}
		if _, duplicate := seenDenyTokens[trimmed]; duplicate {
			return 0, fmt.Errorf("%w: duplicate feedbackDenyToken", ErrInvalidSupervisorPlan)
		}
		seenDenyTokens[trimmed] = struct{}{}
	}
	for i, checkpoint := range plan.Checkpoints {
		if checkpoint.Cycle != i+1 {
			return 0, fmt.Errorf("%w: checkpoints must be contiguous from cycle 1", ErrInvalidSupervisorPlan)
		}
		if len(checkpoint.Checks) == 0 {
			return 0, fmt.Errorf("%w: cycle %d has no checks", ErrInvalidSupervisorPlan, checkpoint.Cycle)
		}
		if len(checkpoint.Checks) > MaxChecksPerPlanV1 {
			return 0, fmt.Errorf("%w: cycle %d exceeds %d checks", ErrInvalidSupervisorPlan, checkpoint.Cycle, MaxChecksPerPlanV1)
		}
		seen := make(map[string]struct{}, len(checkpoint.Checks))
		weightTotal := float64(0)
		for _, check := range checkpoint.Checks {
			id := strings.TrimSpace(check.ID)
			if id == "" {
				return 0, fmt.Errorf("%w: cycle %d has a blank check id", ErrInvalidSupervisorPlan, checkpoint.Cycle)
			}
			if utf8.RuneCountInString(id) > MaxCheckIDRunesV1 {
				return 0, fmt.Errorf("%w: cycle %d check id exceeds the v1 length limit", ErrInvalidSupervisorPlan, checkpoint.Cycle)
			}
			if _, exists := seen[id]; exists {
				return 0, fmt.Errorf("%w: cycle %d has duplicate check id %q", ErrInvalidSupervisorPlan, checkpoint.Cycle, id)
			}
			seen[id] = struct{}{}
			if err := validateSupervisorCheck(check); err != nil {
				return 0, fmt.Errorf("%w: cycle %d check %q: %w", ErrInvalidSupervisorPlan, checkpoint.Cycle, id, err)
			}
			weightTotal += check.Weight
			if math.IsNaN(weightTotal) || math.IsInf(weightTotal, 0) {
				return 0, fmt.Errorf("%w: cycle %d cumulative check weight exceeds the finite range", ErrInvalidSupervisorPlan, checkpoint.Cycle)
			}
		}
	}
	return threshold, nil
}

func validateSupervisorCheck(check Check) error {
	if !isValidWeight(check.Weight) {
		return errors.New("weight is outside the finite v1 range")
	}
	switch check.Type {
	case CheckExactText:
		if utf8.RuneCountInString(check.ExpectedText) > MaxCheckTextRunesV1 {
			return errors.New("expectedText exceeds the v1 length limit")
		}
		return nil
	case CheckContains, CheckContainsToken, CheckForbidden:
		if check.Needle == "" {
			return errors.New("needle is required")
		}
		if utf8.RuneCountInString(check.Needle) > MaxCheckTextRunesV1 {
			return errors.New("needle exceeds the v1 length limit")
		}
		return nil
	case CheckArtifact:
		raw := strings.TrimSpace(check.ArtifactPath)
		if utf8.RuneCountInString(raw) > MaxArtifactPathRunesV1 {
			return errors.New("artifactPath exceeds the v1 length limit")
		}
		clean := filepath.Clean(raw)
		if raw == "" || clean != raw || clean == "." || filepath.IsAbs(clean) || filepath.VolumeName(clean) != "" || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || strings.ContainsRune(clean, '\x00') {
			return errors.New("artifactPath must be a safe relative path")
		}
		want := strings.TrimSpace(check.ExpectedSHA256)
		decoded, err := hex.DecodeString(want)
		if err != nil || len(decoded) != sha256.Size {
			return errors.New("expectedSha256 must be a full hexadecimal digest")
		}
		return nil
	case CheckStateJSONEqual:
		if len(bytes.TrimSpace(check.ExpectedState)) == 0 {
			return errors.New("expectedState is required")
		}
		if _, err := decodeOneJSON(check.ExpectedState); err != nil {
			return fmt.Errorf("expectedState is invalid: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unsupported check type %q", check.Type)
	}
}

func runMismatch(expected SupervisorFingerprint, run *runcontract.RunResult) string {
	if run == nil {
		return "run result is missing"
	}
	if run.SchemaVersion != "deneb-briefcase-run/v1" {
		return "run schema version does not match"
	}
	if err := runcontract.ValidateRunProvenance(run); err != nil {
		return "run provenance is inconsistent"
	}
	if len(run.Episodes) == 0 {
		return "run has no episode result"
	}
	for _, digest := range []string{
		run.CasepackSHA256, run.ToolSchemaSHA256, run.EndpointSHA256,
		run.BuildSHA256, run.ExecutionProfileSHA256, run.SystemPromptSequenceSHA256,
	} {
		if !lowerSHA256(digest) {
			return "run provenance digest is invalid"
		}
	}
	executors := 0
	for _, episode := range run.Episodes {
		if episode.Model == "" && episode.Text == "" && episode.AllText == "" {
			continue
		}
		executors++
		if episode.Model != run.Model || episode.StopReason != "end_turn" || !lowerSHA256(episode.SystemPromptSHA256) {
			return "executor episode is incomplete or mismatched"
		}
	}
	if executors == 0 {
		return "run has no completed executor episode"
	}
	comparisons := []struct {
		name string
		want string
		got  string
	}{
		{"caseId", expected.CaseID, run.CaseID},
		{"casepackSha256", expected.CasepackSHA256, run.CasepackSHA256},
		{"model", expected.Model, run.Model},
		{"providerModel", expected.ProviderModel, run.ProviderModel},
		{"arm", expected.Arm, string(run.Arm)},
		{"apiMode", expected.APIMode, run.APIMode},
		{"recallMode", expected.RecallMode, run.RecallMode},
		{"devicePlanSha256", expected.DevicePlanSHA256, run.DevicePlanSHA256},
		{"devicePlanSourceSha256", expected.DevicePlanSourceSHA256, run.DevicePlanSourceSHA256},
		{"toolSchemaSha256", expected.ToolSchemaSHA256, run.ToolSchemaSHA256},
		{"endpointSha256", expected.EndpointSHA256, run.EndpointSHA256},
		{"buildSha256", expected.BuildSHA256, run.BuildSHA256},
		{"executionProfileSha256", expected.ExecutionProfileSHA256, run.ExecutionProfileSHA256},
	}
	for _, comparison := range comparisons {
		if comparison.want != "" && comparison.want != comparison.got {
			return comparison.name + " does not match"
		}
	}
	if expected.Seed != 0 && expected.Seed != run.Seed {
		return "seed does not match"
	}
	return ""
}

func FingerprintFromRun(run *runcontract.RunResult) Fingerprint {
	return Fingerprint{
		RunID: run.RunID, CaseID: run.CaseID, CasepackSHA256: run.CasepackSHA256,
		Model: run.Model, ProviderModel: run.ProviderModel, Arm: string(run.Arm), APIMode: run.APIMode, RecallMode: run.RecallMode,
		DevicePlanSHA256:           run.DevicePlanSHA256,
		DevicePlanSourceSHA256:     run.DevicePlanSourceSHA256,
		ToolSchemaSHA256:           run.ToolSchemaSHA256,
		EndpointSHA256:             run.EndpointSHA256,
		BuildSHA256:                run.BuildSHA256,
		ExecutionProfileSHA256:     run.ExecutionProfileSHA256,
		SystemPromptSequenceSHA256: run.SystemPromptSequenceSHA256,
		Seed:                       run.Seed,
	}
}

func lowerSHA256(value string) bool {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func clampScore(score float64) float64 {
	if math.IsNaN(score) || math.IsInf(score, 0) || score < 0 {
		return 0
	}
	if score > 1 {
		return 1
	}
	return score
}

func cloneSupervisorPlan(plan SupervisorPlan) SupervisorPlan {
	plan.FeedbackDenyTokens = append([]string(nil), plan.FeedbackDenyTokens...)
	plan.Checkpoints = append([]SupervisorCheckpoint(nil), plan.Checkpoints...)
	for i := range plan.Checkpoints {
		plan.Checkpoints[i].Checks = cloneChecks(plan.Checkpoints[i].Checks)
	}
	return plan
}

func cloneChecks(checks []Check) []Check {
	out := append([]Check(nil), checks...)
	for i := range out {
		out[i].ExpectedState = bytes.Clone(out[i].ExpectedState)
	}
	return out
}

func cloneReport(report Report) Report {
	report.Checks = append([]CheckResult(nil), report.Checks...)
	report.Errors = append([]string(nil), report.Errors...)
	return report
}

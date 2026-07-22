// Package closedloop composes a port-injected Deneb-Briefcase executor, hidden
// checkpoint supervisor, information firewall, and public user simulator.
package closedloop

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	casepack "github.com/choiceoh/deneb/gateway-go/internal/domain/briefcase"
	feedbackcontract "github.com/choiceoh/deneb/gateway-go/internal/domain/briefcase/feedback"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/briefcase/runcontract"
	evalbriefcase "github.com/choiceoh/deneb/gateway-go/internal/eval/briefcase"
)

const ResultSchemaVersion = "deneb-briefcase-loop/v1"

const maxSealedPlanBytes = 16 << 20

const (
	supervisorSourceRef    = "briefcase:supervisor-plan"
	userSimulatorSourceRef = "briefcase:user-simulator-plan"
)

var ErrInvalidClosedLoop = errors.New("invalid briefcase closed loop")

type Termination string

const (
	TerminationPass             Termination = "supervisor-pass"
	TerminationFail             Termination = "supervisor-fail"
	TerminationGlobalTimeout    Termination = "global-timeout"
	TerminationTurnTimeout      Termination = "turn-timeout"
	TerminationCanceled         Termination = "canceled"
	TerminationFeedbackRejected Termination = "feedback-rejected"
	TerminationExecutorError    Termination = "executor-error"
	TerminationInvalid          Termination = "invalid"
)

type Config struct {
	Pack                          *casepack.Pack
	Executor                      Executor
	SupervisorPlanSource          []byte
	SupervisorPlanSourceSHA256    string
	UserSimulatorPlanSource       []byte
	UserSimulatorPlanSourceSHA256 string
}

// Executor is the only runtime capability required by the closed-loop
// evaluator. The command composition root injects the concrete ChatHarness.
type Executor interface {
	Binding() (runcontract.HarnessBinding, error)
	Run(context.Context) (*runcontract.RunResult, error)
	Continue(context.Context, string, string) (*runcontract.RunResult, error)
}

type CycleResult struct {
	Cycle          int                                  `json:"cycle"`
	ExecutorTurnID string                               `json:"executorTurnId"`
	Supervisor     evalbriefcase.SupervisorPublicResult `json:"supervisor"`
	Handoff        json.RawMessage                      `json:"simulatorHandoff,omitempty"`
	Feedback       string                               `json:"sanitizedFeedback,omitempty"`
	FeedbackSHA256 string                               `json:"feedbackSha256,omitempty"`
}

// SupervisorAudit is trusted evaluator output. It is serialized for benchmark
// auditability but is never passed to the simulator or executor. Operators must
// treat a loop result as grader-private until this field is removed.
type SupervisorAudit struct {
	PlanDigest string       `json:"planDigest"`
	Terminal   bool         `json:"terminal"`
	BestCycle  int          `json:"bestCycle,omitempty"`
	BestScore  float64      `json:"bestScore"`
	Cycles     []CycleAudit `json:"cycles"`
}

type CycleAudit struct {
	Cycle    int                                  `json:"cycle"`
	RunID    string                               `json:"runId,omitempty"`
	Decision evalbriefcase.SupervisorDecision     `json:"decision"`
	Reason   evalbriefcase.SupervisorHiddenReason `json:"reason"`
	Report   evalbriefcase.Report                 `json:"report"`
}

type Result struct {
	SchemaVersion                 string                           `json:"schemaVersion"`
	CaseID                        string                           `json:"caseId"`
	CasepackSHA256                string                           `json:"casepackSha256"`
	SupervisorPlanSourceSHA256    string                           `json:"supervisorPlanSourceSha256"`
	UserSimulatorPlanSourceSHA256 string                           `json:"userSimulatorPlanSourceSha256,omitempty"`
	Decision                      evalbriefcase.SupervisorDecision `json:"decision"`
	Termination                   Termination                      `json:"termination"`
	BestCycle                     int                              `json:"bestCycle,omitempty"`
	BestScore                     float64                          `json:"bestScore"`
	Cycles                        []CycleResult                    `json:"cycles"`
	Run                           *runcontract.RunResult           `json:"run,omitempty"`
	BestRun                       *runcontract.RunResult           `json:"bestRun,omitempty"`
	SupervisorAudit               SupervisorAudit                  `json:"supervisorAudit"`
}

type Runner struct {
	mu         sync.Mutex
	started    bool
	pack       *casepack.Pack
	executor   Executor
	supervisor *evalbriefcase.Supervisor
	simulator  feedbackcontract.UserSimulator
	firewall   *feedbackcontract.FeedbackFirewall
	bestRun    *runcontract.RunResult
	result     Result
}

func New(cfg Config) (*Runner, error) {
	if cfg.Pack == nil || cfg.Executor == nil {
		return nil, closedLoopError("pack and executor are required")
	}
	if err := casepack.Validate(cfg.Pack); err != nil {
		return nil, closedLoopError("casepack is invalid")
	}
	authenticatedPack, err := casepack.LoadDir(cfg.Pack.Root)
	if err != nil {
		return nil, fmt.Errorf("%w: reload casepack: %w", ErrInvalidClosedLoop, err)
	}
	if authenticatedPack.Digest != cfg.Pack.Digest {
		return nil, closedLoopError("casepack changed after authentication")
	}
	cfg.Pack = authenticatedPack
	binding, err := cfg.Executor.Binding()
	if err != nil {
		return nil, err
	}
	if binding.CaseID != cfg.Pack.Manifest.CaseID || binding.CasepackSHA256 != cfg.Pack.Digest || binding.Seed != cfg.Pack.Manifest.Seed {
		return nil, closedLoopError("executor does not match the authenticated casepack")
	}
	var supervisorPlan evalbriefcase.SupervisorPlan
	if err := decodeBoundSealedSource(cfg.Pack, cfg.SupervisorPlanSourceSHA256, cfg.SupervisorPlanSource, supervisorSourceRef, &supervisorPlan, "supervisor plan"); err != nil {
		return nil, err
	}
	wantCycles := cfg.Pack.Manifest.RunPolicy.MaxFollowUps + 1
	if supervisorPlan.MaxCycles != wantCycles {
		return nil, closedLoopError("supervisor cycles must equal one plus the signed follow-up budget")
	}
	if err := validateSupervisorArtifactBindings(cfg.Pack, supervisorPlan); err != nil {
		return nil, err
	}
	if supervisorPlan.Fingerprint.CaseID != cfg.Pack.Manifest.CaseID {
		return nil, closedLoopError("supervisor caseId does not match the signed case")
	}
	if err := verifySupervisorBinding(supervisorPlan.Fingerprint, binding); err != nil {
		return nil, err
	}
	var simulator feedbackcontract.UserSimulator
	var userPlan feedbackcontract.UserSimulatorPlan
	if cfg.Pack.Manifest.RunPolicy.MaxFollowUps > 0 {
		if err := decodeBoundSealedSource(cfg.Pack, cfg.UserSimulatorPlanSourceSHA256, cfg.UserSimulatorPlanSource, userSimulatorSourceRef, &userPlan, "user simulator plan"); err != nil {
			return nil, err
		}
		if userPlan.CaseID != cfg.Pack.Manifest.CaseID {
			return nil, closedLoopError("user simulator caseId does not match the signed case")
		}
		simulator, err = feedbackcontract.NewScriptedUserSimulator(userPlan, cfg.Pack.Manifest.RunPolicy.MaxFollowUps)
		if err != nil {
			return nil, err
		}
	} else if len(cfg.UserSimulatorPlanSource) != 0 || cfg.UserSimulatorPlanSourceSHA256 != "" {
		return nil, closedLoopError("user simulator source is forbidden when no follow-ups are signed")
	}
	var firewall *feedbackcontract.FeedbackFirewall
	if cfg.Pack.Manifest.RunPolicy.MaxFollowUps > 0 {
		hidden, err := HiddenFeedbackInputs(cfg.Pack, supervisorPlan, cfg.UserSimulatorPlanSourceSHA256, cfg.SupervisorPlanSourceSHA256)
		if err != nil {
			return nil, err
		}
		firewall, err = feedbackcontract.NewFeedbackFirewall(hidden, feedbackcontract.FeedbackLimits{})
		if err != nil {
			return nil, err
		}
		// Scripted feedback is fully known at construction time. Scan the whole
		// sequence before the executor spends a token, including cross-message
		// splits, then reset to a fresh firewall for the actual loop boundary.
		for _, followUp := range userPlan.FollowUps {
			if _, err := firewall.SanitizeFollowUp(followUp.Message); err != nil {
				return nil, err
			}
		}
		firewall, err = feedbackcontract.NewFeedbackFirewall(hidden, feedbackcontract.FeedbackLimits{})
		if err != nil {
			return nil, err
		}
	}
	supervisor, err := evalbriefcase.NewSupervisorWithArtifactLimits(supervisorPlan, artifactLimits(cfg.Pack))
	if err != nil {
		return nil, err
	}
	return &Runner{
		pack:       cfg.Pack,
		executor:   cfg.Executor,
		supervisor: supervisor,
		simulator:  simulator,
		firewall:   firewall,
		result: Result{
			SchemaVersion:                 ResultSchemaVersion,
			CaseID:                        cfg.Pack.Manifest.CaseID,
			CasepackSHA256:                cfg.Pack.Digest,
			SupervisorPlanSourceSHA256:    cfg.SupervisorPlanSourceSHA256,
			UserSimulatorPlanSourceSHA256: cfg.UserSimulatorPlanSourceSHA256,
			Cycles:                        make([]CycleResult, 0, wantCycles),
		},
	}, nil
}

func artifactLimits(pack *casepack.Pack) map[string]int64 {
	limits := make(map[string]int64, len(pack.Manifest.Artifacts))
	for _, artifact := range pack.Manifest.Artifacts {
		limit := artifact.MaxBytes
		if limit <= 0 {
			limit = casepack.MaxArtifactBytesV1
		}
		limits[artifact.Path] = limit
	}
	return limits
}

func validateSupervisorArtifactBindings(pack *casepack.Pack, plan evalbriefcase.SupervisorPlan) error {
	declared := make(map[string]struct{}, len(pack.Manifest.Artifacts))
	for _, artifact := range pack.Manifest.Artifacts {
		declared[artifact.Path] = struct{}{}
	}
	for _, checkpoint := range plan.Checkpoints {
		for _, check := range checkpoint.Checks {
			if check.Type != evalbriefcase.CheckArtifact {
				continue
			}
			if _, ok := declared[check.ArtifactPath]; !ok {
				return closedLoopError(fmt.Sprintf("cycle %d artifact check %q references undeclared path %q", checkpoint.Cycle, check.ID, check.ArtifactPath))
			}
		}
	}
	return nil
}

func verifySupervisorBinding(fingerprint evalbriefcase.SupervisorFingerprint, binding runcontract.HarnessBinding) error {
	comparisons := []struct {
		name string
		want string
		got  string
	}{
		{"casepackSha256", fingerprint.CasepackSHA256, binding.CasepackSHA256},
		{"model", fingerprint.Model, binding.Model},
		{"arm", fingerprint.Arm, string(binding.Arm)},
		{"apiMode", fingerprint.APIMode, binding.APIMode},
		{"recallMode", fingerprint.RecallMode, binding.RecallMode},
		{"devicePlanSha256", fingerprint.DevicePlanSHA256, binding.DevicePlanSHA256},
		{"devicePlanSourceSha256", fingerprint.DevicePlanSourceSHA256, binding.DevicePlanSourceSHA256},
		{"toolSchemaSha256", fingerprint.ToolSchemaSHA256, binding.ToolSchemaSHA256},
		{"endpointSha256", fingerprint.EndpointSHA256, binding.EndpointSHA256},
		{"buildSha256", fingerprint.BuildSHA256, binding.BuildSHA256},
		{"executionProfileSha256", fingerprint.ExecutionProfileSHA256, binding.ExecutionProfileSHA256},
	}
	for _, comparison := range comparisons {
		if comparison.want != "" && comparison.want != comparison.got {
			return closedLoopError("supervisor " + comparison.name + " does not match the executor")
		}
	}
	if fingerprint.Seed != 0 && fingerprint.Seed != binding.Seed {
		return closedLoopError("supervisor seed does not match the executor")
	}
	return nil
}

// Run executes the complete signed cycle budget under one global deadline. If
// a timeout or post-cycle infrastructure failure occurs, it returns both the
// best completed result and an error so a CLI can persist partial credit while
// still exiting non-zero.
func (r *Runner) Run(ctx context.Context) (*Result, error) {
	if r == nil || r.supervisor == nil {
		return nil, closedLoopError("runner is not initialized")
	}
	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		return nil, closedLoopError("runner is single-use")
	}
	r.started = true
	r.mu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(r.pack.Manifest.RunPolicy.TimeoutSeconds)*time.Second)
	defer cancel()

	run, err := r.executor.Run(ctx)
	if err != nil {
		r.result.Decision = evalbriefcase.SupervisorFail
		r.result.Termination = terminationForExecutorError(ctx, err)
		r.finish(run)
		return cloneResult(&r.result), err
	}

	for {
		cycleNumber := len(r.result.Cycles) + 1
		cycleRun, snapshotErr := r.snapshotCycleRun(ctx, run, cycleNumber)
		if snapshotErr != nil {
			r.result.Decision = evalbriefcase.SupervisorFail
			r.result.Termination = terminationForPhaseError(ctx, TerminationInvalid)
			r.finish(run)
			return cloneResult(&r.result), snapshotErr
		}
		public, evalErr := r.supervisor.EvaluateContext(ctx, run)
		if evalErr != nil {
			r.result.Decision = evalbriefcase.SupervisorFail
			r.result.Termination = terminationForPhaseError(ctx, TerminationInvalid)
			r.finish(run)
			return cloneResult(&r.result), evalErr
		}
		turnID := latestExecutorTurnID(run)
		r.result.Cycles = append(r.result.Cycles, CycleResult{
			Cycle: public.Cycle, ExecutorTurnID: turnID, Supervisor: public,
		})
		if diagnostics := r.supervisor.HiddenDiagnostics(); diagnostics.BestCycle == public.Cycle {
			r.bestRun = cycleRun
		}

		switch public.Decision {
		case evalbriefcase.SupervisorPass:
			r.result.Decision = public.Decision
			r.result.Termination = TerminationPass
			r.finish(run)
			return cloneResult(&r.result), nil
		case evalbriefcase.SupervisorFail:
			r.result.Decision = public.Decision
			r.result.Termination = TerminationFail
			r.finish(run)
			return cloneResult(&r.result), nil
		case evalbriefcase.SupervisorContinue:
			// Cycle N's feedback drives executor follow-up N, which is then
			// evaluated by supervisor checkpoint N+1.
			if public.Cycle > r.pack.Manifest.RunPolicy.MaxFollowUps {
				err := closedLoopError("supervisor continued beyond the signed follow-up budget")
				r.result.Decision = evalbriefcase.SupervisorFail
				r.result.Termination = TerminationInvalid
				r.finish(run)
				return cloneResult(&r.result), err
			}
		default:
			err := closedLoopError("supervisor returned an unsupported decision")
			r.result.Decision = evalbriefcase.SupervisorFail
			r.result.Termination = TerminationInvalid
			r.finish(run)
			return cloneResult(&r.result), err
		}

		handoff, handoffErr := r.buildHandoff(public, run)
		if handoffErr != nil {
			r.result.Decision = evalbriefcase.SupervisorFail
			r.result.Termination = TerminationFeedbackRejected
			r.finish(run)
			return cloneResult(&r.result), handoffErr
		}
		handoffJSON, marshalErr := json.Marshal(handoff)
		if marshalErr != nil {
			r.result.Decision = evalbriefcase.SupervisorFail
			r.result.Termination = TerminationFeedbackRejected
			r.finish(run)
			return cloneResult(&r.result), marshalErr
		}
		cycleRecord := &r.result.Cycles[len(r.result.Cycles)-1]
		cycleRecord.Handoff = handoffJSON

		feedback, simulatorErr := r.simulator.NextFeedback(ctx, public.Cycle, handoff)
		if simulatorErr != nil {
			r.result.Decision = evalbriefcase.SupervisorFail
			r.result.Termination = terminationForFeedbackError(ctx, simulatorErr)
			r.finish(run)
			return cloneResult(&r.result), simulatorErr
		}
		feedback, sanitizeErr := r.firewall.SanitizeFollowUp(feedback)
		if sanitizeErr != nil {
			r.result.Decision = evalbriefcase.SupervisorFail
			r.result.Termination = TerminationFeedbackRejected
			r.finish(run)
			return cloneResult(&r.result), sanitizeErr
		}
		cycleRecord.Feedback = feedback
		sum := sha256.Sum256([]byte(feedback))
		cycleRecord.FeedbackSHA256 = hex.EncodeToString(sum[:])

		nextRun, continueErr := r.executor.Continue(ctx, fmt.Sprintf("simulator-followup-%d", public.Cycle), feedback)
		if continueErr != nil {
			r.result.Decision = evalbriefcase.SupervisorFail
			r.result.Termination = terminationForExecutorError(ctx, continueErr)
			// Continue may have mutated the live workspace before failing. The
			// pre-follow-up cycle snapshot is the last trustworthy evidence.
			r.finish(cycleRun)
			return cloneResult(&r.result), continueErr
		}
		run = nextRun
	}
}

func (r *Runner) buildHandoff(public evalbriefcase.SupervisorPublicResult, run *runcontract.RunResult) (feedbackcontract.SimulatorHandoff, error) {
	trajectory, err := visibleTrajectory(r.pack, run, r.result.Cycles)
	if err != nil {
		return feedbackcontract.SimulatorHandoff{}, err
	}
	return r.firewall.BuildHandoff(feedbackcontract.SimulatorHandoffInput{
		VerdictCategory:            verdictCategory(public.Decision),
		Recoverable:                public.Recoverable,
		ScoreBand:                  scoreBand(public),
		VisibleTrajectorySummaries: trajectory,
		VisibleArtifactSummaries:   visibleArtifacts(r.pack, run),
	})
}

func (r *Runner) finish(run *runcontract.RunResult) {
	r.result.Run = run
	r.result.BestRun = r.bestRun
	diagnostics := r.supervisor.HiddenDiagnostics()
	r.result.BestCycle = diagnostics.BestCycle
	r.result.BestScore = diagnostics.BestScore
	r.result.SupervisorAudit = auditFromHidden(diagnostics)
}

func (r *Runner) snapshotCycleRun(ctx context.Context, run *runcontract.RunResult, cycle int) (*runcontract.RunResult, error) {
	if run == nil || strings.TrimSpace(run.ArtifactRoot) == "" {
		return nil, closedLoopError("cycle run has no artifact root")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := json.Marshal(run)
	if err != nil {
		return nil, fmt.Errorf("closed loop: clone cycle run: %w", err)
	}
	var snapshot runcontract.RunResult
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, fmt.Errorf("closed loop: clone cycle run: %w", err)
	}
	rootParent := filepath.Dir(filepath.Clean(run.ArtifactRoot))
	snapshotRoot := filepath.Join(rootParent, "artifacts", fmt.Sprintf("cycle-%d", cycle))
	if err := os.MkdirAll(snapshotRoot, 0o700); err != nil {
		return nil, fmt.Errorf("closed loop: create cycle artifact snapshot: %w", err)
	}
	for _, artifact := range r.pack.Manifest.Artifacts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		source := filepath.Join(run.ArtifactRoot, filepath.FromSlash(artifact.Path))
		info, err := os.Lstat(source)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("closed loop: inspect artifact %q: %w", artifact.ID, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			continue
		}
		limit := artifact.MaxBytes
		if limit <= 0 {
			limit = casepack.MaxArtifactBytesV1
		}
		if info.Size() > limit {
			return nil, fmt.Errorf("closed loop: artifact %q exceeds its signed size limit", artifact.ID)
		}
		target := filepath.Join(snapshotRoot, filepath.FromSlash(artifact.Path))
		if err := copyArtifactContext(ctx, source, target); err != nil {
			return nil, fmt.Errorf("closed loop: snapshot artifact %q: %w", artifact.ID, err)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	snapshot.ArtifactRoot = snapshotRoot
	return &snapshot, nil
}

func copyArtifactContext(ctx context.Context, source, target string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		_ = output.Close()
		if !committed {
			_ = os.Remove(target)
		}
	}()
	buffer := make([]byte, 64*1024)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		read, readErr := input.Read(buffer)
		if read > 0 {
			if _, err := output.Write(buffer[:read]); err != nil {
				return err
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	if err := output.Close(); err != nil {
		return err
	}
	committed = true
	return ctx.Err()
}

func auditFromHidden(hidden evalbriefcase.SupervisorHiddenDiagnostics) SupervisorAudit {
	audit := SupervisorAudit{
		PlanDigest: hidden.PlanDigest,
		Terminal:   hidden.Terminal,
		BestCycle:  hidden.BestCycle,
		BestScore:  hidden.BestScore,
		Cycles:     make([]CycleAudit, len(hidden.Cycles)),
	}
	for i, cycle := range hidden.Cycles {
		audit.Cycles[i] = CycleAudit{
			Cycle: cycle.Cycle, RunID: cycle.RunID, Decision: cycle.Decision,
			Reason: cycle.Reason, Report: cycle.Report,
		}
	}
	return audit
}

// HiddenFeedbackInputs derives deny tokens from the sealed supervisor plan and
// all sealed case sources. It does not include public executor output.
func HiddenFeedbackInputs(pack *casepack.Pack, plan evalbriefcase.SupervisorPlan, _ ...string) (feedbackcontract.HiddenFeedbackInputs, error) {
	var hidden feedbackcontract.HiddenFeedbackInputs
	if err := collectSealedSourceTokens(pack, &hidden); err != nil {
		return feedbackcontract.HiddenFeedbackInputs{}, err
	}
	appendPlanMetadataTokens(&hidden, plan)
	appendCheckpointTokens(&hidden, plan)
	return hidden, nil
}

// collectSealedSourceTokens folds every sealed case source into hidden: its
// identifiers always, and its full content when the source declares a
// scannable firewall content role within the sealed-size budget.
func collectSealedSourceTokens(pack *casepack.Pack, hidden *feedbackcontract.HiddenFeedbackInputs) error {
	if pack == nil {
		return nil
	}
	sealedContentBytes := int64(0)
	for _, source := range pack.Manifest.Sources {
		if source.Access != casepack.SourceAccessSealed {
			continue
		}
		hidden.SealedSourceIDs = appendNonBlank(hidden.SealedSourceIDs, source.ID)
		hidden.SealedPaths = appendNonBlank(hidden.SealedPaths, source.Path)
		hidden.HiddenReferences = appendNonBlank(hidden.HiddenReferences, source.SourceRef)
		hidden.SupervisorMetadata = appendNonBlank(hidden.SupervisorMetadata, source.SHA256)
		if source.SourceRef == supervisorSourceRef || source.SourceRef == userSimulatorSourceRef {
			continue
		}
		if !isFirewallContentRole(source.SourceRef) {
			return fmt.Errorf("closed loop: sealed source %q must declare a scannable briefcase grader, device, or gold role", source.ID)
		}
		info, err := os.Lstat(filepath.Join(pack.Root, filepath.FromSlash(source.Path)))
		if err != nil || !info.Mode().IsRegular() || info.Size() > maxSealedPlanBytes || sealedContentBytes+info.Size() > maxSealedPlanBytes {
			return fmt.Errorf("closed loop: sealed firewall inputs exceed %d bytes", maxSealedPlanBytes)
		}
		content, err := pack.ReadFile(source.Path)
		if err != nil {
			return fmt.Errorf("closed loop: read sealed source for firewall: %w", err)
		}
		if !utf8.Valid(content) {
			return fmt.Errorf("closed loop: sealed firewall source %q is not valid UTF-8", source.ID)
		}
		sealedContentBytes += int64(len(content))
		hidden.SealedContents = append(hidden.SealedContents, string(content))
	}
	return nil
}

// appendPlanMetadataTokens folds the plan's digest, explicit deny tokens,
// fingerprint fields, pass threshold, and cycle budget into hidden.
func appendPlanMetadataTokens(hidden *feedbackcontract.HiddenFeedbackInputs, plan evalbriefcase.SupervisorPlan) {
	hidden.SupervisorMetadata = appendNonBlank(hidden.SupervisorMetadata, plan.SchemaVersion)
	hidden.SupervisorMetadata = appendNonBlank(hidden.SupervisorMetadata, plan.PlanDigest)
	hidden.ExplicitSensitiveTokens = append(hidden.ExplicitSensitiveTokens, plan.FeedbackDenyTokens...)
	hidden.SupervisorMetadata = appendNonBlank(hidden.SupervisorMetadata, plan.Fingerprint.CaseID)
	hidden.SupervisorMetadata = appendNonBlank(hidden.SupervisorMetadata, plan.Fingerprint.CasepackSHA256)
	hidden.SupervisorMetadata = appendNonBlank(hidden.SupervisorMetadata, plan.Fingerprint.Model)
	hidden.SupervisorMetadata = appendNonBlank(hidden.SupervisorMetadata, plan.Fingerprint.ProviderModel)
	hidden.SupervisorMetadata = appendNonBlank(hidden.SupervisorMetadata, plan.Fingerprint.Arm)
	hidden.SupervisorMetadata = appendNonBlank(hidden.SupervisorMetadata, plan.Fingerprint.APIMode)
	hidden.SupervisorMetadata = appendNonBlank(hidden.SupervisorMetadata, plan.Fingerprint.RecallMode)
	hidden.SupervisorMetadata = appendNonBlank(hidden.SupervisorMetadata, plan.Fingerprint.DevicePlanSHA256)
	hidden.SupervisorMetadata = appendNonBlank(hidden.SupervisorMetadata, plan.Fingerprint.DevicePlanSourceSHA256)
	hidden.SupervisorMetadata = appendNonBlank(hidden.SupervisorMetadata, plan.Fingerprint.ToolSchemaSHA256)
	hidden.SupervisorMetadata = appendNonBlank(hidden.SupervisorMetadata, plan.Fingerprint.EndpointSHA256)
	hidden.SupervisorMetadata = appendNonBlank(hidden.SupervisorMetadata, plan.Fingerprint.BuildSHA256)
	hidden.SupervisorMetadata = appendNonBlank(hidden.SupervisorMetadata, plan.Fingerprint.ExecutionProfileSHA256)
	if plan.Fingerprint.Seed != 0 {
		hidden.SupervisorMetadata = append(hidden.SupervisorMetadata, strconv.FormatInt(plan.Fingerprint.Seed, 10))
	}
	threshold := plan.PassThreshold
	if threshold == 0 {
		threshold = 1
	}
	thresholdText := strconv.FormatFloat(threshold, 'g', -1, 64)
	if thresholdText != "1" {
		hidden.SupervisorMetadata = append(hidden.SupervisorMetadata, thresholdText)
	}
	hidden.SupervisorMetadata = append(hidden.SupervisorMetadata, "maxCycles="+strconv.Itoa(plan.MaxCycles))
}

// appendCheckpointTokens folds every checkpoint check's identifiers, expected
// answers, and hidden references into hidden.
func appendCheckpointTokens(hidden *feedbackcontract.HiddenFeedbackInputs, plan evalbriefcase.SupervisorPlan) {
	for _, checkpoint := range plan.Checkpoints {
		hidden.CheckpointIDs = append(hidden.CheckpointIDs, fmt.Sprintf("checkpoint-cycle-%d", checkpoint.Cycle))
		for _, check := range checkpoint.Checks {
			hidden.RubricIDs = appendNonBlank(hidden.RubricIDs, check.ID)
			hidden.SupervisorMetadata = append(
				hidden.SupervisorMetadata,
				string(check.Type),
				"weight="+strconv.FormatFloat(check.Weight, 'g', -1, 64),
				"critical="+strconv.FormatBool(check.Critical),
			)
			hidden.ExpectedAnswers = appendNonBlank(hidden.ExpectedAnswers, check.ExpectedText)
			hidden.ExpectedAnswers = appendNonBlank(hidden.ExpectedAnswers, check.Needle)
			hidden.HiddenReferences = appendNonBlank(hidden.HiddenReferences, check.ArtifactPath)
			hidden.HiddenReferences = appendNonBlank(hidden.HiddenReferences, check.ExpectedSHA256)
			if len(bytes.TrimSpace(check.ExpectedState)) != 0 {
				hidden.ExpectedAnswers = appendNonBlank(hidden.ExpectedAnswers, string(check.ExpectedState))
				for _, token := range expectedStateScalarTokens(check.ExpectedState) {
					hidden.ExpectedAnswers = appendNonBlank(hidden.ExpectedAnswers, token)
				}
			}
			if check.Type == evalbriefcase.CheckForbidden {
				hidden.HiddenReferences = appendNonBlank(hidden.HiddenReferences, check.Needle)
			}
		}
	}
}

func isFirewallContentRole(sourceRef string) bool {
	return sourceRef == "briefcase:grader-plan" || sourceRef == "briefcase:device-plan" ||
		sourceRef == "briefcase:gold" || strings.HasPrefix(sourceRef, "briefcase:gold:")
}

func expectedStateScalarTokens(raw json.RawMessage) []string {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil
	}
	var tokens []string
	var collect func(any)
	collect = func(item any) {
		switch typed := item.(type) {
		case string:
			tokens = appendNonBlank(tokens, typed)
		case json.Number:
			tokens = appendNonBlank(tokens, typed.String())
			tokens = appendNonBlank(tokens, canonicalJSONNumber(typed))
		case bool:
			tokens = append(tokens, strconv.FormatBool(typed))
		case nil:
			tokens = append(tokens, "null")
		case []any:
			for _, child := range typed {
				collect(child)
			}
		case map[string]any:
			keys := make([]string, 0, len(typed))
			for key := range typed {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				tokens = appendNonBlank(tokens, key)
				collect(typed[key])
			}
		}
	}
	collect(value)
	return tokens
}

func canonicalJSONNumber(number json.Number) string {
	rational, ok := casepack.ParseBoundedRational(number.String())
	if !ok {
		return ""
	}
	denominator := new(big.Int).Set(rational.Denom())
	two, five, zero := big.NewInt(2), big.NewInt(5), big.NewInt(0)
	twos, fives := 0, 0
	for new(big.Int).Mod(denominator, two).Cmp(zero) == 0 {
		denominator.Quo(denominator, two)
		twos++
	}
	for new(big.Int).Mod(denominator, five).Cmp(zero) == 0 {
		denominator.Quo(denominator, five)
		fives++
	}
	if denominator.Cmp(big.NewInt(1)) != 0 {
		return ""
	}
	precision := twos
	if fives > precision {
		precision = fives
	}
	if precision > casepack.MaxRationalExponentAbs {
		return ""
	}
	canonical := rational.FloatString(precision)
	if strings.Contains(canonical, ".") {
		canonical = strings.TrimRight(strings.TrimRight(canonical, "0"), ".")
	}
	if canonical == "-0" {
		return "0"
	}
	return canonical
}

func visibleTrajectory(pack *casepack.Pack, run *runcontract.RunResult, cycles []CycleResult) ([]string, error) {
	const maxItems = 24
	if pack == nil || run == nil {
		return []string{"Executor produced no completed public turn."}, nil
	}
	manifestEpisodes := make(map[string]casepack.Episode, len(pack.Manifest.Episodes))
	for _, episode := range pack.Manifest.Episodes {
		manifestEpisodes[episode.ID] = episode
	}
	feedbackByCycle := make(map[int]string, len(cycles))
	for _, cycle := range cycles {
		if cycle.Feedback != "" {
			feedbackByCycle[cycle.Cycle] = cycle.Feedback
		}
	}
	items := make([]string, 0, len(run.Episodes))
	turn := 0
	for _, episode := range run.Episodes {
		if episode.Model == "" && episode.Text == "" && episode.AllText == "" {
			continue
		}
		turn++
		switch episode.Phase {
		case "follow-up":
			if feedback := feedbackByCycle[episode.Cycle]; feedback != "" {
				items = append(items, fmt.Sprintf("User follow-up %d: %s", episode.Cycle, truncateRunes(feedback, 430)))
			}
		default:
			manifestEpisode, ok := manifestEpisodes[episode.EpisodeID]
			if ok && manifestEpisode.Input != nil {
				input, err := pack.ReadFile(manifestEpisode.Input.Path)
				if err != nil {
					return nil, fmt.Errorf("closed loop: read public episode input %q: %w", episode.EpisodeID, err)
				}
				label := "Public trigger"
				if manifestEpisode.Kind == casepack.EpisodeUserTurn {
					label = "User"
				}
				items = append(items, fmt.Sprintf("%s: %s", label, truncateRunes(strings.TrimSpace(string(input)), 440)))
			}
		}
		text := strings.TrimSpace(episode.Text)
		if text == "" {
			text = "Executor produced no visible text."
		}
		items = append(items, fmt.Sprintf("Executor turn %d: %s", turn, truncateRunes(text, 440)))
	}
	if len(items) == 0 {
		items = append(items, "Executor produced no completed public turn.")
	}
	if len(items) > maxItems {
		items = items[len(items)-maxItems:]
	}
	return items, nil
}

func visibleArtifacts(pack *casepack.Pack, run *runcontract.RunResult) []feedbackcontract.VisibleArtifactSummary {
	const maxItems = 48
	if pack == nil || run == nil || strings.TrimSpace(run.ArtifactRoot) == "" {
		return nil
	}
	artifacts := pack.Manifest.Artifacts
	if len(artifacts) > maxItems {
		artifacts = artifacts[:maxItems]
	}
	out := make([]feedbackcontract.VisibleArtifactSummary, 0, len(artifacts))
	for index, artifact := range artifacts {
		summary := feedbackcontract.VisibleArtifactSummary{Label: fmt.Sprintf("artifact-%d", index+1)}
		path := filepath.Join(run.ArtifactRoot, filepath.FromSlash(artifact.Path))
		info, err := os.Lstat(path)
		switch {
		case errors.Is(err, os.ErrNotExist):
			summary.Status = feedbackcontract.ArtifactMissing
			summary.Summary = "Declared artifact is not present."
		case err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular():
			summary.Status = feedbackcontract.ArtifactUnreadable
			summary.Summary = "Declared artifact is not a readable regular file."
		case artifact.MaxBytes > 0 && info.Size() > artifact.MaxBytes:
			summary.Status = feedbackcontract.ArtifactAvailable
			summary.Summary = "Declared artifact is present but exceeds its public size limit."
		default:
			summary.Status = feedbackcontract.ArtifactAvailable
			summary.Summary = "Declared artifact is present."
		}
		out = append(out, summary)
	}
	return out
}

func latestExecutorTurnID(run *runcontract.RunResult) string {
	if run == nil {
		return ""
	}
	for i := len(run.Episodes) - 1; i >= 0; i-- {
		episode := run.Episodes[i]
		if episode.Model != "" || episode.Text != "" || episode.AllText != "" {
			return episode.EpisodeID
		}
	}
	return ""
}

func verdictCategory(decision evalbriefcase.SupervisorDecision) feedbackcontract.VerdictCategory {
	switch decision {
	case evalbriefcase.SupervisorPass:
		return feedbackcontract.VerdictSatisfactory
	case evalbriefcase.SupervisorContinue:
		return feedbackcontract.VerdictNeedsRevision
	case evalbriefcase.SupervisorFail:
		return feedbackcontract.VerdictBlocked
	default:
		return feedbackcontract.VerdictCannotAssess
	}
}

func scoreBand(result evalbriefcase.SupervisorPublicResult) feedbackcontract.ScoreBand {
	if result.Decision == evalbriefcase.SupervisorFail {
		return feedbackcontract.ScoreBandUnavailable
	}
	if result.Score >= 0.8 {
		return feedbackcontract.ScoreBandHigh
	}
	if result.Score >= 0.4 {
		return feedbackcontract.ScoreBandMedium
	}
	return feedbackcontract.ScoreBandLow
}

func terminationForExecutorError(ctx context.Context, err error) Termination {
	if termination := terminationForPhaseError(ctx, ""); termination != "" {
		return termination
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return TerminationTurnTimeout
	}
	if errors.Is(err, runcontract.ErrTurnTimeout) {
		return TerminationTurnTimeout
	}
	return TerminationExecutorError
}

func terminationForFeedbackError(ctx context.Context, _ error) Termination {
	if termination := terminationForPhaseError(ctx, ""); termination != "" {
		return termination
	}
	return TerminationFeedbackRejected
}

func terminationForPhaseError(ctx context.Context, fallback Termination) Termination {
	if ctx == nil {
		return fallback
	}
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return TerminationGlobalTimeout
	case errors.Is(ctx.Err(), context.Canceled):
		return TerminationCanceled
	default:
		return fallback
	}
}

func validSourceDigest(value string) bool {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func decodeBoundSealedSource(pack *casepack.Pack, digest string, data []byte, sourceRef string, target any, label string) error {
	if !validSourceDigest(digest) || !declaredSealedDigest(pack, digest, sourceRef) {
		return closedLoopError(label + " digest must uniquely own the signed " + sourceRef + " role")
	}
	if len(data) == 0 || len(data) > maxSealedPlanBytes {
		return closedLoopError(label + " bytes are missing or oversized")
	}
	if casepack.DigestBytes(data) != digest {
		return closedLoopError(label + " bytes do not match the signed source digest")
	}
	if err := casepack.RejectDuplicateJSONKeys(data); err != nil {
		return fmt.Errorf("%w: decode %s: %w", ErrInvalidClosedLoop, label, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("%w: decode %s: %w", ErrInvalidClosedLoop, label, err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return closedLoopError(label + " contains multiple JSON values")
		}
		return fmt.Errorf("%w: decode trailing %s data: %w", ErrInvalidClosedLoop, label, err)
	}
	return nil
}

func declaredSealedDigest(pack *casepack.Pack, digest, sourceRef string) bool {
	if pack == nil {
		return false
	}
	roleCount := 0
	matched := false
	for _, source := range pack.Manifest.Sources {
		if source.Access == casepack.SourceAccessSealed && source.SourceRef == sourceRef {
			roleCount++
			matched = matched || source.SHA256 == digest
		}
	}
	return roleCount == 1 && matched
}

func appendNonBlank(values []string, value string) []string {
	if value = strings.TrimSpace(value); value != "" {
		return append(values, value)
	}
	return values
}

func truncateRunes(value string, max int) string {
	if max <= 0 || utf8.RuneCountInString(value) <= max {
		return value
	}
	runes := []rune(value)
	return string(runes[:max-1]) + "…"
}

func closedLoopError(reason string) error {
	return fmt.Errorf("%w: %s", ErrInvalidClosedLoop, reason)
}

func cloneResult(result *Result) *Result {
	if result == nil {
		return nil
	}
	data, err := json.Marshal(result)
	if err != nil {
		clone := *result
		return &clone
	}
	var clone Result
	if err := json.Unmarshal(data, &clone); err != nil {
		fallback := *result
		return &fallback
	}
	return &clone
}

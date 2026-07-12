package briefcase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/tokenest"
	casepack "github.com/choiceoh/deneb/gateway-go/internal/domain/briefcase"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/briefcase/runcontract"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/toolpreset"
)

type ChatHarnessConfig struct {
	Pack                   *casepack.Pack
	Root                   *RunRoot
	Client                 *llm.Client
	Model                  string
	RunID                  string
	SessionKey             string
	Logger                 *slog.Logger
	DevicePlanSource       []byte
	DevicePlanSourceSHA256 string
	Approval               ApprovalFunc
	SkipRecall             bool
	Arm                    Arm
}

type Arm = runcontract.Arm
type EpisodeResult = runcontract.EpisodeResult
type SamplingProfile = runcontract.SamplingProfile
type ToolResultRecord = runcontract.ToolResultRecord
type RunResult = runcontract.RunResult
type HarnessBinding = runcontract.HarnessBinding

const (
	RunSchemaVersion  = runcontract.RunSchemaVersion
	ArmRawPrimary     = runcontract.ArmRawPrimary
	ArmMemoryAssisted = runcontract.ArmMemoryAssisted
)

type ChatHarness struct {
	pack                   *casepack.Pack
	binding                HarnessBinding
	root                   *RunRoot
	clock                  *ManualClock
	world                  *World
	memory                 *denebMemoryMirror
	transcript             *RunTranscript
	device                 *DeviceTwin
	devicePlanSHA256       string
	devicePlanSourceSHA256 string
	toolSchemaSHA256       string
	endpointSHA256         string
	buildSHA256            string
	executionProfileSHA256 string
	sampling               SamplingProfile
	promptAudit            *systemPromptAudit
	gate                   *ToolGate
	handler                *chat.Handler
	model                  string
	apiMode                string
	runID                  string
	sessionKey             string
	skipRecall             bool
	arm                    Arm
	paths                  RunPaths

	mu          sync.Mutex
	toolResults []ToolResultRecord

	lifecycleMu      sync.Mutex
	started          bool
	result           *RunResult
	runDeadline      time.Time
	followUpAttempts int
	modelTurnsUsed   int
	outputTokensUsed int
	poisoned         bool
}

type systemPromptAudit struct {
	mu     sync.Mutex
	digest string
	count  int
}

func (a *systemPromptAudit) reset() {
	a.mu.Lock()
	a.digest = ""
	a.count = 0
	a.mu.Unlock()
}

func (a *systemPromptAudit) record(_ string, prompt []byte) {
	a.mu.Lock()
	a.digest = casepack.DigestBytes(prompt)
	a.count++
	a.mu.Unlock()
}

func (a *systemPromptAudit) consume() (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.count != 1 || a.digest == "" {
		return "", fmt.Errorf("briefcase: expected one finalized system prompt, observed %d", a.count)
	}
	return a.digest, nil
}

var (
	ErrHarnessAlreadyRun = errors.New("briefcase harness initial timeline has already run")
	ErrHarnessNotRun     = errors.New("briefcase harness initial timeline has not completed")
	ErrTurnTimeout       = runcontract.ErrTurnTimeout
	ErrTurnIncomplete    = errors.New("briefcase executor turn did not complete normally")
)

const maxFollowUpInputBytes = 64 << 10

func NewChatHarness(cfg ChatHarnessConfig) (*ChatHarness, error) {
	if cfg.Pack == nil || cfg.Root == nil || cfg.Client == nil {
		return nil, errors.New("briefcase: pack, run root, and LLM client are required")
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, errors.New("briefcase: model is required")
	}
	cfg.Client = cfg.Client.CloneForDeterministicRun()
	if err := casepack.Validate(cfg.Pack); err != nil {
		return nil, fmt.Errorf("briefcase: harness pack: %w", err)
	}
	authenticatedPack, err := casepack.LoadDir(cfg.Pack.Root)
	if err != nil {
		return nil, fmt.Errorf("briefcase: reload harness pack: %w", err)
	}
	if authenticatedPack.Digest != cfg.Pack.Digest {
		return nil, errors.New("briefcase: harness pack changed after authentication")
	}
	cfg.Pack = authenticatedPack
	if cfg.Pack.Manifest.RunPolicy.MaxTokens > int64(math.MaxInt) {
		return nil, errors.New("briefcase: maxTokens exceeds platform int range")
	}
	paths, err := cfg.Root.Paths()
	if err != nil {
		return nil, err
	}
	if err := requireFreshWorkspace(paths.Workspace); err != nil {
		return nil, err
	}
	clock := NewManualClock(cfg.Pack.Manifest.FrozenNow)
	arm := cfg.Arm
	if arm == "" {
		arm = ArmMemoryAssisted
	}
	if arm != ArmRawPrimary && arm != ArmMemoryAssisted {
		return nil, fmt.Errorf("briefcase: unsupported arm %q", arm)
	}
	if err := cfg.Root.claimHarness(); err != nil {
		return nil, err
	}
	var device *DeviceTwin
	var devicePlans []DevicePlan
	devicePlanSHA256 := ""
	devicePlanRoleCount := 0
	devicePlanRoleSHA256 := ""
	for _, source := range cfg.Pack.Manifest.Sources {
		if source.Access == casepack.SourceAccessSealed && source.SourceRef == "briefcase:device-plan" {
			devicePlanRoleCount++
			devicePlanRoleSHA256 = source.SHA256
		}
	}
	if devicePlanRoleCount > 1 {
		return nil, errors.New("briefcase: casepack must declare at most one briefcase:device-plan role")
	}
	if cfg.DevicePlanSourceSHA256 != "" {
		decoded, decodeErr := hex.DecodeString(cfg.DevicePlanSourceSHA256)
		if decodeErr != nil || len(decoded) != sha256.Size || strings.ToLower(cfg.DevicePlanSourceSHA256) != cfg.DevicePlanSourceSHA256 {
			return nil, errors.New("briefcase: device plan source digest must be lowercase SHA-256")
		}
		if devicePlanRoleCount != 1 || devicePlanRoleSHA256 != cfg.DevicePlanSourceSHA256 {
			return nil, errors.New("briefcase: device plan source is not the signed briefcase:device-plan role")
		}
		if casepack.DigestBytes(cfg.DevicePlanSource) != cfg.DevicePlanSourceSHA256 {
			return nil, errors.New("briefcase: device plan bytes do not match the signed source digest")
		}
		devicePlans, err = DecodeDevicePlanSource(cfg.DevicePlanSource)
		if err != nil {
			return nil, err
		}
		devicePlanSHA256, err = DevicePlansDigest(devicePlans)
		if err != nil {
			return nil, err
		}
		device, err = NewDeviceTwin(clock, devicePlans)
		if err != nil {
			return nil, err
		}
	} else {
		if len(cfg.DevicePlanSource) != 0 {
			return nil, errors.New("briefcase: device plan bytes require a signed source digest")
		}
		if devicePlanRoleCount == 1 {
			return nil, errors.New("briefcase: signed briefcase:device-plan source must be loaded")
		}
	}
	allowedDeviceKinds := make([]string, 0, len(devicePlans))
	seenKinds := make(map[string]struct{})
	for _, plan := range devicePlans {
		if _, seen := seenKinds[plan.Kind]; !seen {
			seenKinds[plan.Kind] = struct{}{}
			allowedDeviceKinds = append(allowedDeviceKinds, plan.Kind)
		}
	}
	world, err := NewWorldWithOptions(cfg.Pack, clock, WorldOptions{IncludeMemory: arm == ArmMemoryAssisted})
	if err != nil {
		return nil, err
	}
	if err := world.Materialize(cfg.Root); err != nil {
		return nil, err
	}
	writableArtifacts := make(map[string]int64, len(cfg.Pack.Manifest.Artifacts))
	for _, artifact := range cfg.Pack.Manifest.Artifacts {
		limit := artifact.MaxBytes
		if limit <= 0 {
			limit = casepack.MaxArtifactBytesV1
		}
		writableArtifacts[artifact.Path] = limit
	}
	policy, err := NewPolicy(cfg.Root, PolicyOptions{
		AllowedDeviceKinds: allowedDeviceKinds,
		WritableArtifacts:  writableArtifacts,
	})
	if err != nil {
		return nil, err
	}
	memory, err := newDenebMemoryMirror(paths, world)
	if err != nil {
		return nil, err
	}
	if err := memory.Sync(); err != nil {
		_ = memory.Close()
		return nil, err
	}
	registry, err := NewFixtureRegistry(FixtureRegistryConfig{
		Workspace:  paths.Workspace,
		World:      world,
		Policy:     policy,
		Device:     device,
		WikiStore:  memory.store,
		ToolPolicy: cfg.Pack.Manifest.ToolPolicy,
		Approval:   cfg.Approval,
	})
	if err != nil {
		_ = memory.Close()
		return nil, err
	}
	toolSchemaSHA256, err := fixtureToolSchemaDigest(registry)
	if err != nil {
		_ = memory.Close()
		return nil, err
	}
	endpointSHA256, err := modelEndpointDigest(cfg.Client.BaseURL())
	if err != nil {
		_ = memory.Close()
		return nil, err
	}
	buildSHA256 := buildIdentityDigest()
	sampling := SamplingProfile{Temperature: 0, TopP: 1, FrequencyPenalty: 0, PresencePenalty: 0}
	executionProfileSHA256, err := executionProfileDigest(cfg, toolSchemaSHA256, endpointSHA256, buildSHA256, sampling)
	if err != nil {
		_ = memory.Close()
		return nil, err
	}
	gate, err := NewToolGate(cfg.Pack.Manifest.ToolPolicy, cfg.Approval)
	if err != nil {
		_ = memory.Close()
		return nil, err
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	transcript, err := NewRunTranscript(paths, logger)
	if err != nil {
		_ = memory.Close()
		return nil, err
	}
	sessions := session.NewManager()
	handlerCfg := chat.DefaultHandlerConfig()
	// Never inherit DENEB_MEMORY_TOKEN_BUDGET or another host deployment knob.
	// These constants are part of the Briefcase execution profile; the signed
	// maxTokens/maxTurns/timeouts remain the per-request budget authority.
	handlerCfg.ContextCfg = chat.ContextConfig{
		MemoryTokenBudget:  170_000,
		SystemPromptBudget: 30_000,
		FreshTailCount:     24,
	}
	handlerCfg.LLMClient = cfg.Client
	handlerCfg.Transcript = transcript.Bridge()
	handlerCfg.Tools = registry
	if arm == ArmMemoryAssisted {
		handlerCfg.Memory = chat.MemoryDeps{Wiki: memory.store}
	}
	handlerCfg.DefaultModel = cfg.Model
	handlerCfg.MaxTokens = int(cfg.Pack.Manifest.RunPolicy.MaxTokens)
	handlerCfg.RunLimits = chat.RunLimits{
		MaxTurns: cfg.Pack.Manifest.RunPolicy.MaxTurns,
		Timeout:  runTurnTimeout(cfg.Pack.Manifest.RunPolicy),
	}
	seed := cfg.Pack.Manifest.Seed
	handlerCfg.SamplingSeed = &seed
	handlerCfg.DisableTier1Wiki = true
	handlerCfg.SemanticNow = clock.Now
	handlerCfg.SemanticTimezone = cfg.Pack.Manifest.Timezone
	handlerCfg.WorkspaceDir = paths.Workspace
	handlerCfg.PromptWorkspaceDir = "/briefcase/workspace"
	handlerCfg.BriefcaseMode = true
	promptAudit := &systemPromptAudit{}
	handlerCfg.AuditSystemPrompt = promptAudit.record
	handler := chat.NewHandler(sessions, nil, logger, handlerCfg)

	runID := strings.TrimSpace(cfg.RunID)
	if runID == "" {
		runID = fmt.Sprintf("%s-seed-%d-%s", cfg.Pack.Manifest.CaseID, cfg.Pack.Manifest.Seed, arm)
	}
	sessionKey := strings.TrimSpace(cfg.SessionKey)
	if sessionKey == "" {
		sessionKey = "bench:" + cfg.Pack.Manifest.CaseID + ":" + string(arm)
	}
	// Session keys reach filesystem-backed Polaris paths. Hash the complete
	// caller value with a RunRoot nonce so it is both unique and an opaque safe
	// path segment; raw separators and traversal text never reach a store.
	sessionDigest := sha256.Sum256([]byte(sessionKey + "\x00" + paths.Root))
	sessionKey = "briefcase-" + hex.EncodeToString(sessionDigest[:16])
	binding := HarnessBinding{
		CaseID: cfg.Pack.Manifest.CaseID, CasepackSHA256: cfg.Pack.Digest,
		Seed: cfg.Pack.Manifest.Seed, Model: cfg.Model, APIMode: cfg.Client.APIMode(), Arm: arm,
		RecallMode:       recallMode(cfg.SkipRecall || arm == ArmRawPrimary),
		DevicePlanSHA256: devicePlanSHA256, DevicePlanSourceSHA256: cfg.DevicePlanSourceSHA256,
		ToolSchemaSHA256: toolSchemaSHA256,
		EndpointSHA256:   endpointSHA256, BuildSHA256: buildSHA256,
		ExecutionProfileSHA256: executionProfileSHA256,
	}
	return &ChatHarness{
		pack: cfg.Pack, root: cfg.Root, clock: clock, world: world, memory: memory,
		binding:    binding,
		transcript: transcript,
		device:     device, devicePlanSHA256: devicePlanSHA256,
		devicePlanSourceSHA256: cfg.DevicePlanSourceSHA256, gate: gate, handler: handler,
		toolSchemaSHA256: toolSchemaSHA256,
		endpointSHA256:   endpointSHA256, buildSHA256: buildSHA256,
		executionProfileSHA256: executionProfileSHA256, sampling: sampling, promptAudit: promptAudit,
		model: cfg.Model, apiMode: cfg.Client.APIMode(), runID: runID, sessionKey: sessionKey,
		skipRecall: cfg.SkipRecall || arm == ArmRawPrimary, arm: arm, paths: paths,
	}, nil
}

func requireFreshWorkspace(workspace string) error {
	entries, err := os.ReadDir(workspace)
	if err != nil {
		return fmt.Errorf("briefcase: inspect run workspace: %w", err)
	}
	if len(entries) != 0 {
		return errors.New("briefcase: run workspace is not fresh; use a distinct RunRoot for every arm")
	}
	return nil
}

func (h *ChatHarness) Close() error {
	if h == nil || h.handler == nil {
		return nil
	}
	h.handler.Close()
	return errors.Join(h.transcript.Close(), h.memory.Close())
}

func (h *ChatHarness) Binding() (HarnessBinding, error) {
	if h == nil || h.pack == nil || h.handler == nil {
		return HarnessBinding{}, errors.New("briefcase: harness is not initialized")
	}
	return h.binding, nil
}

func (h *ChatHarness) Run(ctx context.Context) (*RunResult, error) {
	if h == nil || h.handler == nil {
		return nil, errors.New("briefcase: harness is not initialized")
	}
	h.lifecycleMu.Lock()
	defer h.lifecycleMu.Unlock()
	if h.started {
		return nil, ErrHarnessAlreadyRun
	}
	// A failed initial run is deliberately not retryable. The handler, tool
	// budget, transcript, and Device Twin may all have observed a prefix of the
	// attempt, so retrying in place would no longer be a fresh evaluation.
	h.started = true
	if ctx == nil {
		ctx = context.Background()
	}
	deadline := time.Now().Add(time.Duration(h.pack.Manifest.RunPolicy.TimeoutSeconds) * time.Second)
	if parentDeadline, ok := ctx.Deadline(); ok && parentDeadline.Before(deadline) {
		deadline = parentDeadline
	}
	h.runDeadline = deadline
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	episodesByID := make(map[string]casepack.Episode, len(h.pack.Manifest.Episodes))
	events := make([]TimelineEvent, 0, len(h.pack.Manifest.Episodes))
	for index, episode := range h.pack.Manifest.Episodes {
		episodesByID[episode.ID] = episode
		payload, _ := json.Marshal(struct {
			EpisodeID string `json:"episodeId"`
		}{EpisodeID: episode.ID})
		events = append(events, TimelineEvent{
			ID: episode.ID, At: episode.At, Order: uint64(index), Kind: string(episode.Kind), Payload: payload,
		})
	}
	timeline, err := NewTimeline(h.clock, events)
	if err != nil {
		return nil, err
	}
	result := &RunResult{
		SchemaVersion:          RunSchemaVersion,
		RunID:                  h.runID,
		CaseID:                 h.pack.Manifest.CaseID,
		CasepackSHA256:         h.pack.Digest,
		Seed:                   h.pack.Manifest.Seed,
		Model:                  h.model,
		APIMode:                h.apiMode,
		SeedForwarded:          h.apiMode == llm.APIModeOpenAI,
		RecallMode:             recallMode(h.skipRecall),
		DevicePlanSHA256:       h.devicePlanSHA256,
		DevicePlanSourceSHA256: h.devicePlanSourceSHA256,
		ToolSchemaSHA256:       h.toolSchemaSHA256,
		EndpointSHA256:         h.endpointSHA256,
		BuildSHA256:            h.buildSHA256,
		ExecutionProfileSHA256: h.executionProfileSHA256,
		Sampling:               h.sampling,
		Arm:                    h.arm,
		ArtifactRoot:           h.paths.Workspace,
	}
	var lastCompleted *RunResult
	executableCount := 0
	err = timeline.ReplayAll(ctx, func(ctx context.Context, event TimelineEvent) error {
		episode, ok := episodesByID[event.ID]
		if !ok {
			return fmt.Errorf("briefcase: timeline references unknown episode %q", event.ID)
		}
		release, err := h.world.ReleaseWithOutcomeContext(ctx, episode.ReleaseSourceIDs)
		if err != nil {
			return err
		}
		if err := h.world.MaterializeContext(ctx, h.root); err != nil {
			return err
		}
		if err := h.memory.SyncContext(ctx); err != nil {
			return err
		}
		if h.device != nil {
			if _, err := h.device.SettleDueContext(ctx); err != nil {
				return err
			}
		}
		episodeResult := EpisodeResult{
			EpisodeID:      event.ID,
			At:             event.At.UTC().Format(time.RFC3339Nano),
			Phase:          "timeline",
			ReleasedSource: append([]string(nil), release.Released...),
			WithheldSource: append([]string(nil), release.Withheld...),
		}
		if episode.Kind == casepack.EpisodeEvent {
			if err := ctx.Err(); err != nil {
				return err
			}
			result.Episodes = append(result.Episodes, episodeResult)
			return nil
		}
		if episode.Input == nil {
			return fmt.Errorf("briefcase: executable episode %q has no input", episode.ID)
		}
		prompt, err := h.pack.ReadFile(episode.Input.Path)
		if err != nil {
			return fmt.Errorf("briefcase: read episode %q input: %w", episode.ID, err)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		wirePrompt := chat.SanitizeInput(string(prompt))
		if wirePrompt == "" {
			return fmt.Errorf("briefcase: executable episode %q normalizes to an empty input", episode.ID)
		}
		episodeResult.InputSHA256 = casepack.DigestBytes([]byte(wirePrompt))
		turnCtx, turnCancel := context.WithTimeout(ctx, runTurnTimeout(h.pack.Manifest.RunPolicy))
		response, systemPromptSHA256, err := h.sendTurn(turnCtx, wirePrompt, episode.Kind == casepack.EpisodeHeartbeat)
		turnErr := turnCtx.Err()
		turnCancel()
		if err != nil {
			return fmt.Errorf("briefcase: execute episode %q: %w", episode.ID, err)
		}
		if turnErr != nil {
			return fmt.Errorf("briefcase: execute episode %q: %w", episode.ID, turnErr)
		}
		episodeResult.Text = response.BestTextRaw()
		episodeResult.SystemPromptSHA256 = systemPromptSHA256
		episodeResult.AllText = response.AllText
		episodeResult.Model = response.Model
		episodeResult.ProviderModel = response.ProviderModel
		episodeResult.StopReason = response.StopReason
		episodeResult.InputTokens = response.InputTokens
		episodeResult.OutputTokens = response.OutputTokens
		episodeResult.Turns = response.Turns
		result.Episodes = append(result.Episodes, episodeResult)
		if err := h.refreshResult(ctx, result); err != nil {
			return err
		}
		executableCount++
		lastCompleted, err = h.snapshotCompletedRun(ctx, result, fmt.Sprintf("timeline-%d", executableCount))
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return cloneRunResult(lastCompleted), err
	}
	if err := h.refreshResult(ctx, result); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	h.result = result
	return cloneRunResult(result), nil
}

func (h *ChatHarness) snapshotCompletedRun(ctx context.Context, run *RunResult, label string) (*RunResult, error) {
	if run == nil {
		return nil, errors.New("briefcase: cannot snapshot a nil completed run")
	}
	root := filepath.Join(h.paths.Root, "artifacts", label)
	return exportRunArtifacts(ctx, h.pack, run, root, true)
}

// ExportRunArtifacts copies only signed, declared artifacts into a durable
// directory and rewrites ArtifactRoot on a detached result. The destination
// must not already exist, preventing stale files from contaminating a score.
func ExportRunArtifacts(ctx context.Context, pack *casepack.Pack, run *RunResult, destination string) (_ *RunResult, returnErr error) {
	return exportRunArtifacts(ctx, pack, run, destination, false)
}

func exportRunArtifacts(ctx context.Context, pack *casepack.Pack, run *RunResult, destination string, allowInsideRunRoot bool) (_ *RunResult, returnErr error) {
	if pack == nil || run == nil {
		return nil, errors.New("briefcase: pack and run result are required for artifact export")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(run.ArtifactRoot) == "" {
		return nil, errors.New("briefcase: run artifact root is invalid")
	}
	sourceRoot, err := filepath.Abs(filepath.Clean(run.ArtifactRoot))
	if err != nil {
		return nil, errors.New("briefcase: run artifact root is invalid")
	}
	resolvedSourceRoot, err := filepath.EvalSymlinks(sourceRoot)
	if err != nil {
		return nil, fmt.Errorf("briefcase: resolve run artifact root: %w", err)
	}
	rootInfo, err := os.Stat(resolvedSourceRoot)
	if err != nil || !rootInfo.IsDir() {
		return nil, errors.New("briefcase: run artifact root is not a directory")
	}
	if strings.TrimSpace(destination) == "" {
		return nil, errors.New("briefcase: artifact export destination is required")
	}
	destination, err = filepath.Abs(filepath.Clean(strings.TrimSpace(destination)))
	if err != nil {
		return nil, errors.New("briefcase: artifact export destination is required")
	}
	resolvedDestination, err := resolveProspectivePath(destination)
	if err != nil {
		return nil, fmt.Errorf("briefcase: resolve artifact export destination: %w", err)
	}
	if pathWithin(resolvedSourceRoot, resolvedDestination) {
		return nil, errors.New("briefcase: artifact export destination must be outside the run root")
	}
	if !allowInsideRunRoot {
		if plaintextRoot := inferredPlaintextRunRoot(resolvedSourceRoot); plaintextRoot != "" && pathWithin(plaintextRoot, resolvedDestination) {
			return nil, errors.New("briefcase: durable artifact export destination must be outside the plaintext RunRoot")
		}
	}
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			return nil, errors.New("briefcase: artifact export destination already exists")
		}
		return nil, fmt.Errorf("briefcase: inspect artifact export destination: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return nil, fmt.Errorf("briefcase: create artifact export parent: %w", err)
	}
	if err := os.Mkdir(destination, 0o700); err != nil {
		return nil, fmt.Errorf("briefcase: create artifact export destination: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			if cleanupErr := os.RemoveAll(destination); cleanupErr != nil {
				returnErr = errors.Join(returnErr, fmt.Errorf("briefcase: clean failed artifact export: %w", cleanupErr))
			}
		}
	}()
	resolvedCreatedDestination, err := filepath.EvalSymlinks(destination)
	if err != nil || resolvedCreatedDestination != resolvedDestination {
		return nil, errors.New("briefcase: artifact export destination changed during creation")
	}

	snapshot := cloneRunResult(run)
	for _, artifact := range pack.Manifest.Artifacts {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		source := filepath.Join(run.ArtifactRoot, filepath.FromSlash(artifact.Path))
		info, err := os.Lstat(source)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("briefcase: inspect completed artifact %q: %w", artifact.ID, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("briefcase: completed artifact %q is not a regular file", artifact.ID)
		}
		resolvedSource, err := filepath.EvalSymlinks(source)
		if err != nil || !pathWithin(resolvedSourceRoot, resolvedSource) {
			return nil, fmt.Errorf("briefcase: completed artifact %q escapes the run root", artifact.ID)
		}
		limit := artifact.MaxBytes
		if limit <= 0 {
			limit = casepack.MaxArtifactBytesV1
		}
		if info.Size() > limit {
			return nil, fmt.Errorf("briefcase: completed artifact %q exceeds its signed size limit", artifact.ID)
		}
		target := filepath.Join(destination, filepath.FromSlash(artifact.Path))
		if err := copyCompletedArtifact(ctx, source, target); err != nil {
			return nil, fmt.Errorf("briefcase: snapshot completed artifact %q: %w", artifact.ID, err)
		}
	}
	snapshot.ArtifactRoot = destination
	committed = true
	return snapshot, ctx.Err()
}

func resolveProspectivePath(target string) (string, error) {
	target, err := filepath.Abs(filepath.Clean(target))
	if err != nil {
		return "", err
	}
	current := target
	var suffix []string
	for {
		if _, err := os.Lstat(current); err == nil {
			resolved, err := filepath.EvalSymlinks(current)
			if err != nil {
				return "", err
			}
			for index := len(suffix) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, suffix[index])
			}
			return filepath.Clean(resolved), nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", errors.New("no existing artifact destination ancestor")
		}
		suffix = append(suffix, filepath.Base(current))
		current = parent
	}
}

func inferredPlaintextRunRoot(sourceRoot string) string {
	switch {
	case filepath.Base(sourceRoot) == "workspace":
		return filepath.Dir(sourceRoot)
	case filepath.Base(filepath.Dir(sourceRoot)) == "artifacts":
		return filepath.Dir(filepath.Dir(sourceRoot))
	default:
		return ""
	}
}

func copyCompletedArtifact(ctx context.Context, source, target string) error {
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
		n, readErr := input.Read(buffer)
		if n > 0 {
			if _, err := output.Write(buffer[:n]); err != nil {
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

func recallMode(skip bool) string {
	if skip {
		return "disabled"
	}
	return "enabled"
}

// Continue sends one sanitized user-simulator follow-up through the same Deneb
// session as the completed initial timeline. The RunRoot, transcript, memory
// store, tool budgets, and Device Twin therefore continue atomically across
// cycles. Callers must create a fresh harness for a fresh benchmark attempt.
func (h *ChatHarness) Continue(ctx context.Context, episodeID, message string) (*RunResult, error) {
	if h == nil || h.handler == nil {
		return nil, errors.New("briefcase: harness is not initialized")
	}
	h.lifecycleMu.Lock()
	defer h.lifecycleMu.Unlock()
	if !h.started || h.result == nil {
		return nil, ErrHarnessNotRun
	}
	if h.poisoned {
		return nil, errors.New("briefcase: harness is terminal after a failed follow-up")
	}
	episodeID = strings.TrimSpace(episodeID)
	if episodeID == "" {
		return nil, errors.New("briefcase: follow-up episode id is required")
	}
	for _, episode := range h.result.Episodes {
		if episode.EpisodeID == episodeID {
			return nil, fmt.Errorf("briefcase: duplicate episode id %q", episodeID)
		}
	}
	if h.followUpAttempts >= h.pack.Manifest.RunPolicy.MaxFollowUps {
		return nil, errors.New("briefcase: signed follow-up budget exhausted")
	}
	if !utf8.ValidString(message) {
		return nil, errors.New("briefcase: follow-up message is not valid UTF-8")
	}
	if strings.TrimSpace(message) == "" {
		return nil, errors.New("briefcase: follow-up message is empty")
	}
	if len(message) > maxFollowUpInputBytes {
		return nil, fmt.Errorf("briefcase: follow-up message exceeds %d bytes", maxFollowUpInputBytes)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if h.runDeadline.IsZero() || !time.Now().Before(h.runDeadline) {
		return nil, context.DeadlineExceeded
	}
	globalCtx, globalCancel := context.WithDeadline(ctx, h.runDeadline)
	defer globalCancel()
	ctx, cancel := context.WithTimeout(globalCtx, runTurnTimeout(h.pack.Manifest.RunPolicy))
	defer cancel()
	wireMessage := chat.SanitizeInput(message)
	if wireMessage == "" {
		return nil, errors.New("briefcase: follow-up normalizes to an empty input")
	}
	h.followUpAttempts++ // reserve before any observable side effect
	response, systemPromptSHA256, err := h.sendTurn(ctx, wireMessage, false)
	if err != nil {
		h.poisoned = true
		return nil, fmt.Errorf("briefcase: execute follow-up %q: %w", episodeID, err)
	}
	cycle := 1
	for _, episode := range h.result.Episodes {
		if episode.Phase == "follow-up" && episode.Cycle >= cycle {
			cycle = episode.Cycle + 1
		}
	}
	candidate := cloneRunResult(h.result)
	candidate.Episodes = append(candidate.Episodes, EpisodeResult{
		EpisodeID:          episodeID,
		At:                 h.clock.Now().UTC().Format(time.RFC3339Nano),
		Phase:              "follow-up",
		Cycle:              cycle,
		InputSHA256:        casepack.DigestBytes([]byte(wireMessage)),
		SystemPromptSHA256: systemPromptSHA256,
		Text:               response.BestTextRaw(),
		AllText:            response.AllText,
		Model:              response.Model,
		ProviderModel:      response.ProviderModel,
		StopReason:         response.StopReason,
		InputTokens:        response.InputTokens,
		OutputTokens:       response.OutputTokens,
		Turns:              response.Turns,
	})
	if err := h.refreshResult(ctx, candidate); err != nil {
		h.poisoned = true
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		h.poisoned = true
		return nil, err
	}
	h.result = candidate
	return cloneRunResult(candidate), nil
}

// Snapshot returns a detached copy of the latest completed harness state.
func (h *ChatHarness) Snapshot() (*RunResult, error) {
	if h == nil {
		return nil, errors.New("briefcase: harness is not initialized")
	}
	h.lifecycleMu.Lock()
	defer h.lifecycleMu.Unlock()
	if h.result == nil {
		return nil, ErrHarnessNotRun
	}
	return cloneRunResult(h.result), nil
}

func (h *ChatHarness) sendTurn(ctx context.Context, message string, ephemeral bool) (*chat.SyncResult, string, error) {
	h.promptAudit.reset()
	remainingTurns := h.pack.Manifest.RunPolicy.MaxTurns - h.modelTurnsUsed
	remainingTokens := int(h.pack.Manifest.RunPolicy.MaxTokens) - h.outputTokensUsed
	if remainingTurns <= 0 {
		return nil, "", errors.New("briefcase: cumulative model-turn budget exhausted")
	}
	if remainingTokens <= 0 {
		return nil, "", errors.New("briefcase: cumulative output-token budget exhausted")
	}
	temperature := h.sampling.Temperature
	topP := h.sampling.TopP
	frequencyPenalty := h.sampling.FrequencyPenalty
	presencePenalty := h.sampling.PresencePenalty
	remainingToolCallAttempts := h.gate.RemainingAttempts()
	opts := &chat.SyncOptions{
		ToolPreset:          string(toolpreset.PresetBriefcase),
		BeforeToolCall:      h.gate.BeforeToolCall,
		OnToolResult:        h.onToolResult,
		GateUntrustedTools:  true,
		SkipRecall:          h.skipRecall,
		EphemeralUser:       ephemeral,
		Temperature:         &temperature,
		TopP:                &topP,
		FrequencyPenalty:    &frequencyPenalty,
		PresencePenalty:     &presencePenalty,
		MaxTurns:            &remainingTurns,
		MaxTokens:           &remainingTokens,
		MaxToolCallAttempts: &remainingToolCallAttempts,
	}
	response, err := h.handler.SendSyncStream(ctx, h.sessionKey, message, h.model, opts, nil)
	if err != nil {
		return nil, "", err
	}
	if err := validateTurnCompletion(response); err != nil {
		return nil, "", err
	}
	if response.Model != h.model {
		return nil, "", fmt.Errorf("briefcase: response model %q does not match requested model %q", response.Model, h.model)
	}
	if strings.TrimSpace(response.ProviderModel) == "" {
		return nil, "", errors.New("briefcase: provider did not attest a response model")
	}
	if response.Turns <= 0 || response.Turns > remainingTurns {
		return nil, "", errors.New("briefcase: model-turn accounting exceeded the signed budget")
	}
	chargedTokens := response.OutputTokens
	if estimate := tokenest.EstimateUncalibrated(response.AllText); estimate > chargedTokens {
		chargedTokens = estimate
	}
	if chargedTokens < 0 || chargedTokens > remainingTokens {
		return nil, "", errors.New("briefcase: output-token accounting exceeded the signed budget")
	}
	h.modelTurnsUsed += response.Turns
	h.outputTokensUsed += chargedTokens
	response.OutputTokens = chargedTokens
	systemPromptSHA256, err := h.promptAudit.consume()
	if err != nil {
		return nil, "", err
	}
	return response, systemPromptSHA256, nil
}

func validateTurnCompletion(response *chat.SyncResult) error {
	if response == nil {
		return fmt.Errorf("%w: empty response", ErrTurnIncomplete)
	}
	switch response.StopReason {
	case "end_turn":
		return nil
	case "timeout":
		return fmt.Errorf("%w: stop reason timeout", ErrTurnTimeout)
	default:
		return fmt.Errorf("%w: stop reason %q", ErrTurnIncomplete, response.StopReason)
	}
}

// LatestExecutorText returns the most recent completed model answer while
// ignoring non-executable timeline events that may follow it.
func LatestExecutorText(run *RunResult) string {
	return runcontract.LatestExecutorText(run)
}

func runTurnTimeout(policy casepack.RunPolicy) time.Duration {
	seconds := policy.PerTurnTimeoutSeconds
	if seconds <= 0 {
		seconds = policy.TimeoutSeconds
	}
	return time.Duration(seconds) * time.Second
}

func (h *ChatHarness) refreshResult(ctx context.Context, result *RunResult) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var deviceLedger []DeviceActionRecord
	if h.device != nil {
		var err error
		deviceLedger, err = h.device.LedgerContext(ctx)
		if err != nil {
			return err
		}
	}
	toolCalls := h.gate.Records()
	if err := ctx.Err(); err != nil {
		return err
	}
	h.mu.Lock()
	toolResults := append([]ToolResultRecord(nil), h.toolResults...)
	h.mu.Unlock()
	visibleSourceIDs, err := h.world.VisibleSourceIDsContext(ctx)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	result.DeviceLedger = deviceLedger
	result.ToolCalls = toolCalls
	result.ToolResults = toolResults
	result.VisibleSourceIDs = visibleSourceIDs
	result.State = h.stateJSON(result)
	providerModel := ""
	promptDigests := make([]string, 0, len(result.Episodes))
	for _, episode := range result.Episodes {
		if episode.SystemPromptSHA256 != "" {
			promptDigests = append(promptDigests, episode.SystemPromptSHA256)
		}
		if episode.ProviderModel != "" {
			if providerModel != "" && providerModel != episode.ProviderModel {
				return errors.New("briefcase: provider model changed across episodes")
			}
			providerModel = episode.ProviderModel
		}
	}
	result.ProviderModel = providerModel
	result.SystemPromptSequenceSHA256, err = systemPromptSequenceDigest(promptDigests)
	if err != nil {
		return err
	}
	return ctx.Err()
}

func modelEndpointDigest(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", errors.New("briefcase: model endpoint must be an absolute http(s) URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("briefcase: model endpoint must not contain credentials, query, or fragment")
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawPath = ""
	return casepack.DigestBytes([]byte(parsed.String())), nil
}

func buildIdentityDigest() string {
	if executable, err := os.Executable(); err == nil {
		if resolved, resolveErr := filepath.EvalSymlinks(executable); resolveErr == nil {
			executable = resolved
		}
		if digest, digestErr := casepack.FileDigest(executable); digestErr == nil {
			return digest
		}
	}

	type identity struct {
		GoVersion string            `json:"goVersion"`
		GOOS      string            `json:"goos"`
		GOARCH    string            `json:"goarch"`
		Module    string            `json:"module"`
		Version   string            `json:"version"`
		Settings  map[string]string `json:"settings,omitempty"`
	}
	value := identity{GoVersion: runtime.Version(), GOOS: runtime.GOOS, GOARCH: runtime.GOARCH}
	if info, ok := debug.ReadBuildInfo(); ok {
		value.Module = info.Main.Path
		value.Version = info.Main.Version
		value.Settings = make(map[string]string)
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs", "vcs.revision", "vcs.time", "vcs.modified":
				value.Settings[setting.Key] = setting.Value
			}
		}
	}
	encoded, _ := json.Marshal(value)
	return casepack.DigestBytes(encoded)
}

func executionProfileDigest(cfg ChatHarnessConfig, toolSchema, endpoint, build string, sampling SamplingProfile) (string, error) {
	return canonicalExecutionProfileDigest(cfg.Model, cfg.Client.APIMode(), toolSchema, endpoint, build, sampling)
}

type executionProfile = runcontract.ExecutionProfile

func fixedExecutionProfile(model, apiMode, toolSchema, endpoint, build string, sampling SamplingProfile) executionProfile {
	return runcontract.FixedExecutionProfile(model, apiMode, toolSchema, endpoint, build, sampling)
}

func canonicalExecutionProfileDigest(model, apiMode, toolSchema, endpoint, build string, sampling SamplingProfile) (string, error) {
	return runcontract.ExecutionProfileDigest(model, apiMode, toolSchema, endpoint, build, sampling)
}

func systemPromptSequenceDigest(digests []string) (string, error) {
	return runcontract.SystemPromptSequenceDigest(digests)
}

// SetRunProvenance fills the derived fixed-profile fields on a detached run.
// Runtime producers call it after episode prompt digests are known; tests and
// importers can use it to construct schema-valid fixtures without duplicating
// canonical hashing rules.
func SetRunProvenance(result *RunResult) error {
	return runcontract.SetRunProvenance(result)
}

// ValidateRunProvenance checks the self-consistency of the run's fixed
// execution profile and finalized system-prompt sequence.
func ValidateRunProvenance(result *RunResult) error {
	return runcontract.ValidateRunProvenance(result)
}

func cloneRunResult(result *RunResult) *RunResult {
	return runcontract.CloneRunResult(result)
}

func (h *ChatHarness) onToolResult(name, toolUseID, value string, isErr bool) {
	sum := sha256.Sum256([]byte(value))
	h.mu.Lock()
	h.toolResults = append(h.toolResults, ToolResultRecord{
		Name: name, ToolUseID: toolUseID, ResultSHA256: hex.EncodeToString(sum[:]), Error: isErr,
	})
	h.mu.Unlock()
}

func (h *ChatHarness) stateJSON(result *RunResult) json.RawMessage {
	state := struct {
		Now              string               `json:"now"`
		VisibleSourceIDs []string             `json:"visibleSourceIds"`
		DeviceLedger     []DeviceActionRecord `json:"deviceLedger,omitempty"`
	}{
		Now:              h.clock.Now().UTC().Format(time.RFC3339Nano),
		VisibleSourceIDs: append([]string(nil), result.VisibleSourceIDs...),
		DeviceLedger:     append([]DeviceActionRecord(nil), result.DeviceLedger...),
	}
	data, _ := json.Marshal(state)
	return data
}

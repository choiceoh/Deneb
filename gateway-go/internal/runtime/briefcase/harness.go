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
	casepack "github.com/choiceoh/deneb/gateway-go/internal/domain/briefcase"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/briefcase/runcontract"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolwire"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/briefcase/runtranscript"
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
	// TokenEstimate overrides response token accounting; nil keeps provider OutputTokens only.
	TokenEstimate func(string) int
}

type (
	Arm              = runcontract.Arm
	EpisodeResult    = runcontract.EpisodeResult
	SamplingProfile  = runcontract.SamplingProfile
	ToolResultRecord = runcontract.ToolResultRecord
	RunResult        = runcontract.RunResult
	HarnessBinding   = runcontract.HarnessBinding
)

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
	transcript             *runtranscript.RunTranscript
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
	tokenEstimate          func(string) int
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

type chatHarnessAssembly struct {
	cfg    ChatHarnessConfig
	paths  RunPaths
	clock  *ManualClock
	arm    Arm
	device harnessDevicePlan
	world  *World
	policy *Policy
	memory *denebMemoryMirror

	profile     harnessExecutionProfile
	transcript  *runtranscript.RunTranscript
	gate        *ToolGate
	handler     *chat.Handler
	promptAudit *systemPromptAudit
}

type harnessDevicePlan struct {
	twin         *DeviceTwin
	digest       string
	allowedKinds []string
}

type harnessExecutionProfile struct {
	toolSchemaSHA256       string
	endpointSHA256         string
	buildSHA256            string
	executionProfileSHA256 string
	sampling               SamplingProfile
}

func NewChatHarness(cfg ChatHarnessConfig) (*ChatHarness, error) {
	assembly, err := prepareChatHarnessAssembly(cfg)
	if err != nil {
		return nil, err
	}
	// A post-claim failure deliberately consumes the RunRoot: device validation
	// and runtime assembly may have observed or materialized attempt-local state.
	if err := assembly.cfg.Root.ClaimHarness(); err != nil {
		return nil, err
	}
	if err := assembly.prepareDevicePlan(); err != nil {
		return nil, err
	}
	if err := assembly.prepareWorldAndMemory(); err != nil {
		return nil, err
	}
	// The memory mirror is the first closeable resource. Every remaining
	// fallible step runs behind one boundary so cleanup cannot drift by branch.
	if err := assembly.prepareExecution(); err != nil {
		_ = assembly.memory.Close()
		return nil, err
	}
	return assembly.buildHarness(), nil
}

func prepareChatHarnessAssembly(cfg ChatHarnessConfig) (*chatHarnessAssembly, error) {
	if cfg.Pack == nil || cfg.Root == nil || cfg.Client == nil {
		return nil, errors.New("briefcase: pack, run root, and LLM client are required")
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, errors.New("briefcase: model is required")
	}
	cfg.Client = cfg.Client.CloneForDeterministicRun()
	authenticatedPack, err := authenticateHarnessPack(cfg.Pack)
	if err != nil {
		return nil, err
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
	arm, err := normalizeHarnessArm(cfg.Arm)
	if err != nil {
		return nil, err
	}
	return &chatHarnessAssembly{cfg: cfg, paths: paths, clock: clock, arm: arm}, nil
}

func authenticateHarnessPack(pack *casepack.Pack) (*casepack.Pack, error) {
	if err := casepack.Validate(pack); err != nil {
		return nil, fmt.Errorf("briefcase: harness pack: %w", err)
	}
	authenticatedPack, err := casepack.LoadDir(pack.Root)
	if err != nil {
		return nil, fmt.Errorf("briefcase: reload harness pack: %w", err)
	}
	if authenticatedPack.Digest != pack.Digest {
		return nil, errors.New("briefcase: harness pack changed after authentication")
	}
	return authenticatedPack, nil
}

func normalizeHarnessArm(arm Arm) (Arm, error) {
	if arm == "" {
		return ArmMemoryAssisted, nil
	}
	if arm != ArmRawPrimary && arm != ArmMemoryAssisted {
		return "", fmt.Errorf("briefcase: unsupported arm %q", arm)
	}
	return arm, nil
}

func (a *chatHarnessAssembly) prepareDevicePlan() error {
	devicePlan, err := loadHarnessDevicePlan(a.cfg, a.clock)
	if err != nil {
		return err
	}
	a.device = devicePlan
	return nil
}

func loadHarnessDevicePlan(cfg ChatHarnessConfig, clock *ManualClock) (harnessDevicePlan, error) {
	roleCount, roleSHA256 := declaredDevicePlanRole(cfg.Pack)
	if roleCount > 1 {
		return harnessDevicePlan{}, errors.New("briefcase: casepack must declare at most one briefcase:device-plan role")
	}
	if cfg.DevicePlanSourceSHA256 == "" {
		if len(cfg.DevicePlanSource) != 0 {
			return harnessDevicePlan{}, errors.New("briefcase: device plan bytes require a signed source digest")
		}
		if roleCount == 1 {
			return harnessDevicePlan{}, errors.New("briefcase: signed briefcase:device-plan source must be loaded")
		}
		return harnessDevicePlan{}, nil
	}
	if err := validateDevicePlanSource(cfg, roleCount, roleSHA256); err != nil {
		return harnessDevicePlan{}, err
	}
	plans, err := DecodeDevicePlanSource(cfg.DevicePlanSource)
	if err != nil {
		return harnessDevicePlan{}, err
	}
	digest, err := DevicePlansDigest(plans)
	if err != nil {
		return harnessDevicePlan{}, err
	}
	twin, err := NewDeviceTwin(clock, plans)
	if err != nil {
		return harnessDevicePlan{}, err
	}
	return harnessDevicePlan{twin: twin, digest: digest, allowedKinds: uniqueDeviceKinds(plans)}, nil
}

func declaredDevicePlanRole(pack *casepack.Pack) (count int, sha256 string) {
	for _, source := range pack.Manifest.Sources {
		if source.Access == casepack.SourceAccessSealed && source.SourceRef == "briefcase:device-plan" {
			count++
			sha256 = source.SHA256
		}
	}
	return count, sha256
}

func validateDevicePlanSource(cfg ChatHarnessConfig, roleCount int, roleSHA256 string) error {
	decoded, err := hex.DecodeString(cfg.DevicePlanSourceSHA256)
	if err != nil || len(decoded) != sha256.Size || strings.ToLower(cfg.DevicePlanSourceSHA256) != cfg.DevicePlanSourceSHA256 {
		return errors.New("briefcase: device plan source digest must be lowercase SHA-256")
	}
	if roleCount != 1 || roleSHA256 != cfg.DevicePlanSourceSHA256 {
		return errors.New("briefcase: device plan source is not the signed briefcase:device-plan role")
	}
	if casepack.DigestBytes(cfg.DevicePlanSource) != cfg.DevicePlanSourceSHA256 {
		return errors.New("briefcase: device plan bytes do not match the signed source digest")
	}
	return nil
}

func uniqueDeviceKinds(plans []DevicePlan) []string {
	kinds := make([]string, 0, len(plans))
	seen := make(map[string]struct{}, len(plans))
	for _, plan := range plans {
		if _, ok := seen[plan.Kind]; ok {
			continue
		}
		seen[plan.Kind] = struct{}{}
		kinds = append(kinds, plan.Kind)
	}
	return kinds
}

func (a *chatHarnessAssembly) prepareWorldAndMemory() error {
	world, err := NewWorldWithOptions(a.cfg.Pack, a.clock, WorldOptions{IncludeMemory: a.arm == ArmMemoryAssisted})
	if err != nil {
		return err
	}
	if err := world.Materialize(a.cfg.Root); err != nil {
		return err
	}
	policy, err := NewPolicy(a.cfg.Root, PolicyOptions{
		AllowedDeviceKinds: a.device.allowedKinds,
		WritableArtifacts:  writableArtifactLimits(a.cfg.Pack),
	})
	if err != nil {
		return err
	}
	memory, err := newDenebMemoryMirror(a.paths, world)
	if err != nil {
		return err
	}
	if err := memory.Sync(); err != nil {
		_ = memory.Close()
		return err
	}
	a.world = world
	a.policy = policy
	a.memory = memory
	return nil
}

func writableArtifactLimits(pack *casepack.Pack) map[string]int64 {
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

func (a *chatHarnessAssembly) prepareExecution() error {
	registry, err := NewFixtureRegistry(FixtureRegistryConfig{
		Workspace:  a.paths.Workspace,
		World:      a.world,
		Policy:     a.policy,
		Device:     a.device.twin,
		WikiStore:  a.memory.store,
		ToolPolicy: a.cfg.Pack.Manifest.ToolPolicy,
		Approval:   a.cfg.Approval,
	})
	if err != nil {
		return err
	}
	profile, err := buildHarnessExecutionProfile(a.cfg, registry)
	if err != nil {
		return err
	}
	gate, err := NewToolGate(a.cfg.Pack.Manifest.ToolPolicy, a.cfg.Approval)
	if err != nil {
		return err
	}
	logger := a.cfg.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	transcript, err := runtranscript.NewRunTranscript(runtranscript.Paths{Root: a.paths.Root, State: a.paths.State}, logger)
	if err != nil {
		return err
	}
	handler, promptAudit := newDeterministicChatHandler(a, registry, transcript, logger)
	a.profile = profile
	a.gate = gate
	a.transcript = transcript
	a.handler = handler
	a.promptAudit = promptAudit
	return nil
}

func buildHarnessExecutionProfile(cfg ChatHarnessConfig, registry *chat.ToolRegistry) (harnessExecutionProfile, error) {
	toolSchemaSHA256, err := fixtureToolSchemaDigest(registry)
	if err != nil {
		return harnessExecutionProfile{}, err
	}
	endpointSHA256, err := modelEndpointDigest(cfg.Client.BaseURL())
	if err != nil {
		return harnessExecutionProfile{}, err
	}
	buildSHA256 := buildIdentityDigest()
	sampling := SamplingProfile{Temperature: 0, TopP: 1, FrequencyPenalty: 0, PresencePenalty: 0}
	executionProfileSHA256, err := executionProfileDigest(cfg, toolSchemaSHA256, endpointSHA256, buildSHA256, sampling)
	if err != nil {
		return harnessExecutionProfile{}, err
	}
	return harnessExecutionProfile{
		toolSchemaSHA256:       toolSchemaSHA256,
		endpointSHA256:         endpointSHA256,
		buildSHA256:            buildSHA256,
		executionProfileSHA256: executionProfileSHA256,
		sampling:               sampling,
	}, nil
}

func newDeterministicChatHandler(
	a *chatHarnessAssembly,
	registry *chat.ToolRegistry,
	transcript *runtranscript.RunTranscript,
	logger *slog.Logger,
) (*chat.Handler, *systemPromptAudit) {
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
	handlerCfg.LLMClient = a.cfg.Client
	handlerCfg.Transcript = transcript.Bridge()
	handlerCfg.Tools = registry
	if a.arm == ArmMemoryAssisted {
		handlerCfg.Memory = chat.MemoryDeps{Wiki: a.memory.store}
	}
	handlerCfg.DefaultModel = a.cfg.Model
	handlerCfg.MaxTokens = int(a.cfg.Pack.Manifest.RunPolicy.MaxTokens)
	handlerCfg.RunLimits = chat.RunLimits{
		MaxTurns: a.cfg.Pack.Manifest.RunPolicy.MaxTurns,
		Timeout:  runTurnTimeout(a.cfg.Pack.Manifest.RunPolicy),
	}
	seed := a.cfg.Pack.Manifest.Seed
	handlerCfg.SamplingSeed = &seed
	handlerCfg.DisableTier1Wiki = true
	handlerCfg.SemanticNow = a.clock.Now
	handlerCfg.SemanticTimezone = a.cfg.Pack.Manifest.Timezone
	handlerCfg.WorkspaceDir = a.paths.Workspace
	handlerCfg.PromptWorkspaceDir = "/briefcase/workspace"
	handlerCfg.BriefcaseMode = true
	promptAudit := &systemPromptAudit{}
	handlerCfg.AuditSystemPrompt = promptAudit.record
	return chat.NewHandler(sessions, nil, logger, handlerCfg), promptAudit
}

func (a *chatHarnessAssembly) buildHarness() *ChatHarness {
	runID := strings.TrimSpace(a.cfg.RunID)
	if runID == "" {
		runID = fmt.Sprintf("%s-seed-%d-%s", a.cfg.Pack.Manifest.CaseID, a.cfg.Pack.Manifest.Seed, a.arm)
	}
	sessionKey := strings.TrimSpace(a.cfg.SessionKey)
	if sessionKey == "" {
		sessionKey = "bench:" + a.cfg.Pack.Manifest.CaseID + ":" + string(a.arm)
	}
	// Session keys reach filesystem-backed Polaris paths. Hash the complete
	// caller value with a RunRoot nonce so it is both unique and an opaque safe
	// path segment; raw separators and traversal text never reach a store.
	sessionDigest := sha256.Sum256([]byte(sessionKey + "\x00" + a.paths.Root))
	sessionKey = "briefcase-" + hex.EncodeToString(sessionDigest[:16])
	binding := HarnessBinding{
		CaseID: a.cfg.Pack.Manifest.CaseID, CasepackSHA256: a.cfg.Pack.Digest,
		Seed: a.cfg.Pack.Manifest.Seed, Model: a.cfg.Model, APIMode: a.cfg.Client.APIMode(), Arm: a.arm,
		RecallMode:       recallMode(a.cfg.SkipRecall || a.arm == ArmRawPrimary),
		DevicePlanSHA256: a.device.digest, DevicePlanSourceSHA256: a.cfg.DevicePlanSourceSHA256,
		ToolSchemaSHA256: a.profile.toolSchemaSHA256,
		EndpointSHA256:   a.profile.endpointSHA256, BuildSHA256: a.profile.buildSHA256,
		ExecutionProfileSHA256: a.profile.executionProfileSHA256,
	}
	return &ChatHarness{
		pack: a.cfg.Pack, root: a.cfg.Root, clock: a.clock, world: a.world, memory: a.memory,
		binding:    binding,
		transcript: a.transcript,
		device:     a.device.twin, devicePlanSHA256: a.device.digest,
		devicePlanSourceSHA256: a.cfg.DevicePlanSourceSHA256, gate: a.gate, handler: a.handler,
		toolSchemaSHA256: a.profile.toolSchemaSHA256,
		endpointSHA256:   a.profile.endpointSHA256, buildSHA256: a.profile.buildSHA256,
		executionProfileSHA256: a.profile.executionProfileSHA256, sampling: a.profile.sampling, promptAudit: a.promptAudit,
		model: a.cfg.Model, apiMode: a.cfg.Client.APIMode(), runID: runID, sessionKey: sessionKey,
		skipRecall: a.cfg.SkipRecall || a.arm == ArmRawPrimary, tokenEstimate: a.cfg.TokenEstimate, arm: a.arm, paths: a.paths,
	}
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
		ToolPreset:          toolwire.PresetBriefcase,
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
	if h.tokenEstimate != nil {
		if estimate := h.tokenEstimate(response.AllText); estimate > chargedTokens {
			chargedTokens = estimate
		}
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

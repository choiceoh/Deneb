package core

import (
	"log/slog"
	"path/filepath"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/generation"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/review"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/configresolve"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/skilllifecycle/nudgeradapt"
)

// Type aliases so runtime/server can depend on this adhesion package instead of
// importing genesis leaf packages (generation/review) directly. Identical types —
// assignable to handler Deps that still name the leaf packages.
type (
	GenesisService       = generation.Service
	MetaArtifacts        = generation.MetaArtifacts
	Nudger               = review.Nudger
	Catalog              = skills.Catalog
	Evolver              = genesis.Evolver
	Tracker              = genesis.Tracker
	RetryCorrectionMiner = genesis.RetryCorrectionMiner
)

// NewRetryCorrectionMiner re-exports the transcript retry-pair miner
// constructor (evidence-side; see genesis/retry_correction_miner.go).
var NewRetryCorrectionMiner = genesis.NewRetryCorrectionMiner

// GenesisBundle is the owning-module port for genesis services wired at boot.
// Server holds these fields via GenesisSubsystem; construction lives here so
// runtime/server does not import generation/review.
type GenesisBundle struct {
	Service *GenesisService
	Meta    *MetaArtifacts
	Tracker *Tracker
	Evolver *Evolver
	Nudger  *Nudger
	Catalog *Catalog
	Config  generation.Config
}

// DefaultMetaArtifacts returns the compiled-in RSI P1 prompt defaults.
func DefaultMetaArtifacts() map[string]string {
	return generation.DefaultMetaArtifacts()
}

// DefaultNudgeInterval is the review nudger's default interval.
const DefaultNudgeInterval = review.DefaultNudgeInterval

// NewMetaArtifacts constructs a meta-artifact store under dir.
func NewMetaArtifacts(dir string, logger *slog.Logger) *MetaArtifacts {
	return generation.NewMetaArtifacts(dir, logger)
}

// NewNudgerFromEnvWithTrackerAndReviewer wraps the review package constructor.
func NewNudgerFromEnvWithTrackerAndReviewer(
	svc *GenesisService,
	tracker *Tracker,
	reviewer review.SkillReviewRunner,
	logger *slog.Logger,
) *Nudger {
	return review.NewNudgerFromEnvWithTrackerAndReviewer(svc, tracker, reviewer, logger)
}

// NewSkillNudger adapts a review nudger to the stable chat port.
func NewSkillNudger(n *Nudger) chatport.SkillNudger {
	return nudgeradapt.New(n)
}

// CoreBuildInput is the model/workspace surface needed to construct a GenesisBundle
// without pulling Server into this package.
type CoreBuildInput struct {
	Logger                *slog.Logger
	LWClient              *llm.Client
	LWModel               string
	MainClient            *llm.Client
	MainModel             string
	WorkspaceDir          string
	BundledSkillsDir      string
	ThinkingKwargs        map[string]string
	LowConfidenceObserver func(genesis.EvolveResult)
	ConfigureEvolver      func(*Evolver) (roleName string, model string)
}

// BuildCore constructs catalog + genesis/evolver/meta services (no nudger/chat wiring).
// Nudger attachment stays in the composition root because it needs chatHandler ports.
func BuildCore(in CoreBuildInput) *GenesisBundle {
	if in.Logger == nil || in.LWClient == nil || in.LWModel == "" {
		return nil
	}
	cfg := generation.DefaultConfigFromEnv()
	cfg.Model = in.LWModel

	catalog := skills.NewCatalog(in.Logger)
	seedSkillCatalog(catalog, in.WorkspaceDir, in.Logger)

	svc := generation.NewService(cfg, in.LWClient, catalog, in.Logger)

	var tracker *Tracker
	if t, err := genesis.NewTracker(in.Logger); err != nil {
		in.Logger.Warn("genesis: tracker unavailable", "error", err)
	} else {
		tracker = t
	}

	if tracker != nil && catalog != nil {
		known := map[string]bool{}
		for _, e := range catalog.List() {
			known[e.Skill.Name] = true
		}
		if pruned, rerr := tracker.ReconcileCuratorAgainstCatalog(known); rerr != nil {
			in.Logger.Warn("genesis: curator reconcile failed", "error", rerr)
		} else if len(pruned) > 0 {
			in.Logger.Info("genesis: pruned orphan curator entries", "skills", pruned)
		}
	}

	evolver := genesis.NewEvolver(in.LWClient, catalog, tracker, in.LWModel, in.Logger)
	if in.LowConfidenceObserver != nil {
		evolver.SetLowConfidenceObserver(in.LowConfidenceObserver)
	}

	meta := generation.NewMetaArtifacts(filepath.Join(cfg.OutputDir, "meta"), in.Logger)
	svc.SetMetaArtifacts(meta)
	evolver.SetMetaArtifacts(meta)
	evolver.SetAdoptionDirs(in.BundledSkillsDir, "")

	var evolverRole, evolverModel string
	if in.ConfigureEvolver != nil {
		evolverRole, evolverModel = in.ConfigureEvolver(evolver)
	}

	if in.MainClient != nil && in.MainModel != "" {
		svc.SetJudge(in.MainClient, in.MainModel, &llm.ThinkingConfig{
			Type:          "disabled",
			TemplateKwarg: in.ThinkingKwargs[in.MainModel],
		})
	}

	in.Logger.Info("genesis: core bundle built",
		"model", in.LWModel, "evolverRole", evolverRole, "evolverModel", evolverModel,
		"outputDir", cfg.OutputDir,
		"minToolCalls", cfg.MinToolCalls,
		"minTurns", cfg.MinTurns,
		"maxSkillsPerDay", cfg.MaxSkillsPerDay)

	return &GenesisBundle{
		Service: svc,
		Meta:    meta,
		Tracker: tracker,
		Evolver: evolver,
		Catalog: catalog,
		Config:  cfg,
	}
}

func seedSkillCatalog(catalog *Catalog, workspaceDir string, logger *slog.Logger) {
	if catalog == nil {
		return
	}
	if workspaceDir == "" {
		workspaceDir = configresolve.WorkspaceDir()
	}
	entries := skills.DiscoverWorkspaceSkills(skills.DiscoverConfig{
		WorkspaceDir: workspaceDir,
		Logger:       logger,
	})
	for _, entry := range entries {
		catalog.Register(entry)
	}
	if len(entries) > 0 {
		logger.Info("genesis: seeded skill catalog", "skills", len(entries), "workspace", workspaceDir)
	}
}

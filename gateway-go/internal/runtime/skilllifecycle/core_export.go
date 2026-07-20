package skilllifecycle

import (
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/skilllifecycle/core"
)

// Genesis adhesion types — re-exported so runtime/server imports this package
// once instead of skilllifecycle/core + nudgeradapt.
type (
	GenesisService       = core.GenesisService
	MetaArtifacts        = core.MetaArtifacts
	Nudger               = core.Nudger
	Catalog              = core.Catalog
	Evolver              = core.Evolver
	Tracker              = core.Tracker
	GenesisBundle        = core.GenesisBundle
	CoreBuildInput       = core.CoreBuildInput
	RetryCorrectionMiner = core.RetryCorrectionMiner
)

const DefaultNudgeInterval = core.DefaultNudgeInterval

var (
	BuildCore                              = core.BuildCore
	DefaultMetaArtifacts                   = core.DefaultMetaArtifacts
	NewMetaArtifacts                       = core.NewMetaArtifacts
	NewNudgerFromEnvWithTrackerAndReviewer = core.NewNudgerFromEnvWithTrackerAndReviewer
	NewSkillNudger                         = core.NewSkillNudger
	NewRetryCorrectionMiner                = core.NewRetryCorrectionMiner
)

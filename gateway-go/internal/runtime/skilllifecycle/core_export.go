package skilllifecycle

import (
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/skilllifecycle/leafbind"
)

// Genesis adhesion types — re-exported so runtime/server imports this package
// once instead of skilllifecycle/core + nudgeradapt.
type (
	GenesisService = leafbind.GenesisService
	MetaArtifacts  = leafbind.MetaArtifacts
	Nudger         = leafbind.Nudger
	Catalog        = leafbind.Catalog
	Evolver        = leafbind.Evolver
	Tracker        = leafbind.Tracker
	GenesisBundle  = leafbind.GenesisBundle
	CoreBuildInput = leafbind.CoreBuildInput
)

const DefaultNudgeInterval = leafbind.DefaultNudgeInterval

var (
	BuildCore                              = leafbind.BuildCore
	DefaultMetaArtifacts                   = leafbind.DefaultMetaArtifacts
	NewMetaArtifacts                       = leafbind.NewMetaArtifacts
	NewNudgerFromEnvWithTrackerAndReviewer = leafbind.NewNudgerFromEnvWithTrackerAndReviewer
	NewSkillNudger                         = leafbind.NewSkillNudger
)

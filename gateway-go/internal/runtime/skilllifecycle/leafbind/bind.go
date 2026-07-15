// Package leafbind aggregates skilllifecycle leaf imports for the parent
// skilllifecycle package so direct fan-out stays under the Health Bench bar.
package leafbind

import (
	genesiscommon "github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/common"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/generation"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/propus"
	chattools "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/lifecycletool"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/skilllifecycle/core"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/skilllifecycle/propuswire"
)

type (
	// core — genesis bundle adhesion for runtime/server.
	GenesisService = core.GenesisService
	MetaArtifacts  = core.MetaArtifacts
	Nudger         = core.Nudger
	Catalog        = core.Catalog
	Evolver        = core.Evolver
	Tracker        = core.Tracker
	GenesisBundle  = core.GenesisBundle
	CoreBuildInput = core.CoreBuildInput

	// generation — skill genesis service surface.
	Service        = generation.Service
	GeneratedSkill = generation.GeneratedSkill
	SessionContext = generation.SessionContext
	ToolActivity   = generation.ToolActivity

	// propus — doctrine contract (tests).
	PropusDoctrineSpec = propus.PropusDoctrineSpec
)

const (
	DefaultNudgeInterval = core.DefaultNudgeInterval

	ScopeSkill  = propuswire.ScopeSkill
	ScopeGlobal = propuswire.ScopeGlobal
)

var (
	ErrSkillDeduped = generation.ErrSkillDeduped

	TruncateRunes = genesiscommon.TruncateRunes

	BuildCore                              = core.BuildCore
	DefaultMetaArtifacts                   = core.DefaultMetaArtifacts
	NewMetaArtifacts                       = core.NewMetaArtifacts
	NewNudgerFromEnvWithTrackerAndReviewer = core.NewNudgerFromEnvWithTrackerAndReviewer
	NewSkillNudger                         = core.NewSkillNudger

	PropusDoctrine = propus.PropusDoctrine

	UnavailableOverview = propuswire.UnavailableOverview
	SkillOverview       = propuswire.SkillOverview
	GlobalOverview      = propuswire.GlobalOverview
	SystemStatus        = propuswire.SystemStatus
)

// SkillLifecycleOverview is the chat tool overview wire type.
type SkillLifecycleOverview = chattools.SkillLifecycleOverview

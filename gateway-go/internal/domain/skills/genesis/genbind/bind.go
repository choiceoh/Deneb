// Package genbind aggregates genesis leaf imports for the parent genesis
// package so direct fan-out stays under the Health Bench soft boundary.
package genbind

import (
	genesiscommon "github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/common"
	genesiseprocess "github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/eprocess"
	generation "github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/generation"
	guardrails "github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/guardrails"
	rsilifecycle "github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/lifecycle"
	rsistatus "github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/status"
	surfaces "github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/surfaces"
)

type (
	// generation — meta artifacts and genesis service.
	Service       = generation.Service
	MetaArtifacts = generation.MetaArtifacts

	// guardrails — deterministic edit safety checks.
	Audit = guardrails.Audit

	// lifecycle — L4 review/delivery state (Layer renamed to avoid status.Layer).
	LifecycleLayer    = rsilifecycle.Layer
	LifecycleIdentity = rsilifecycle.Identity
	ReviewState       = rsilifecycle.ReviewState
	DeliveryPhase     = rsilifecycle.DeliveryPhase
	DeliveryClass     = rsilifecycle.DeliveryClass
	DispatchFacts     = rsilifecycle.DispatchFacts

	// status — RSI health read model.
	LoopStatus  = rsistatus.LoopStatus
	StatusLayer = rsistatus.Layer
	Health      = rsistatus.Health
	Metric      = rsistatus.Metric

	// surfaces — declared self-improvement editable surfaces.
	EditableSurface = surfaces.EditableSurface
)

const (
	MetaGenesisSystemPrompt    = generation.MetaGenesisSystemPrompt
	MetaEvolveSystemPrompt     = generation.MetaEvolveSystemPrompt
	MetaSkillJudgeSystemPrompt = generation.MetaSkillJudgeSystemPrompt
	MetaArtifactMinBytes       = generation.MetaArtifactMinBytes

	MaxChangedSections = guardrails.MaxChangedSections

	LifecycleLayerL1 = rsilifecycle.LayerL1
	LifecycleLayerL2 = rsilifecycle.LayerL2
	LifecycleLayerL3 = rsilifecycle.LayerL3
	LifecycleLayerL4 = rsilifecycle.LayerL4

	ReviewProposed   = rsilifecycle.ReviewProposed
	ReviewAccepted   = rsilifecycle.ReviewAccepted
	ReviewRejected   = rsilifecycle.ReviewRejected
	ReviewSuperseded = rsilifecycle.ReviewSuperseded
	ReviewApplied    = rsilifecycle.ReviewApplied

	DeliveryStarted     = rsilifecycle.DeliveryStarted
	DeliveryPROpened    = rsilifecycle.DeliveryPROpened
	DeliveryMerged      = rsilifecycle.DeliveryMerged
	DeliveryDeployed    = rsilifecycle.DeliveryDeployed
	DeliveryWatchPassed = rsilifecycle.DeliveryWatchPassed
	DeliveryDeclined    = rsilifecycle.DeliveryDeclined
	DeliveryFailed      = rsilifecycle.DeliveryFailed
	DeliveryRolledBack  = rsilifecycle.DeliveryRolledBack

	DeliveryQueued    = rsilifecycle.DeliveryQueued
	DeliveryInFlight  = rsilifecycle.DeliveryInFlight
	DeliveryVerified  = rsilifecycle.DeliveryVerified
	DeliverySafeNoop  = rsilifecycle.DeliverySafeNoop
	DeliveryRetryable = rsilifecycle.DeliveryRetryable

	StateLive      = rsistatus.StateLive
	StateDataGated = rsistatus.StateDataGated
	StateStarved   = rsistatus.StateStarved
	StateFrozen    = rsistatus.StateFrozen
	StateIdle      = rsistatus.StateIdle

	SurfaceTierAutoApply   = surfaces.SurfaceTierAutoApply
	SurfaceTierProposeOnly = surfaces.SurfaceTierProposeOnly
	SurfaceTierForbidden   = surfaces.SurfaceTierForbidden
)

var (
	DefaultEProcessAlpha = genesiseprocess.DefaultEProcessAlpha

	TruncateRunes     = genesiscommon.TruncateRunes
	ErrorString       = genesiscommon.ErrorString
	JaccardSimilarity = genesiscommon.JaccardSimilarity
	SkillDedupTokens  = genesiscommon.SkillDedupTokens

	BenchAdmissibility   = generation.BenchAdmissibility
	ContentSHA256        = generation.ContentSHA256
	DefaultMetaArtifacts = generation.DefaultMetaArtifacts
	NewMetaArtifacts     = generation.NewMetaArtifacts

	NewEProcess = genesiseprocess.NewEProcess

	CanonicalSkillSurface             = guardrails.CanonicalSkillSurface
	NormalizeSignature                = guardrails.NormalizeSignature
	NormalizedSkillHeadings           = guardrails.NormalizedSkillHeadings
	SignatureMatches                  = guardrails.SignatureMatches
	ValidateEditedSurface             = guardrails.ValidateEditedSurface
	ValidateHermesEvolutionGuardrails = guardrails.ValidateHermesEvolutionGuardrails
	ValidateTextualEditBudget         = guardrails.ValidateTextualEditBudget

	IdentityFor           = rsilifecycle.IdentityFor
	NormalizeReview       = rsilifecycle.NormalizeReview
	CanReviewTransition   = rsilifecycle.CanReviewTransition
	NormalizeDelivery     = rsilifecycle.NormalizeDelivery
	CanDeliveryTransition = rsilifecycle.CanDeliveryTransition
	ClassifyDelivery      = rsilifecycle.ClassifyDelivery
	ReviewAfterDelivery   = rsilifecycle.ReviewAfterDelivery
	CanDispatch           = rsilifecycle.CanDispatch

	ClassifyProposalSurfaces = surfaces.ClassifyProposalSurfaces
	ClassifySurface          = surfaces.ClassifySurface
	ForbiddenSurfaceMentions = surfaces.ForbiddenSurfaceMentions
)

// EProcess is the anytime-valid sequential test primitive.
type EProcess = genesiseprocess.EProcess

// Package contract exposes the stable Mini App wire DTO surface consumed by
// sibling RPC handler packages.
//
// The generated wire structs remain in the parent handlerminiapp package so
// client model generation keeps one source of truth. Leaf handlers depend on
// this narrow facade instead of importing the volatile parent implementation
// package directly.
package contract

import handlerminiapp "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp"

type (
	ContactRow = handlerminiapp.ContactRow

	DashboardItem = handlerminiapp.DashboardItem
	LaneOut       = handlerminiapp.LaneOut
	DashboardOut  = handlerminiapp.DashboardOut

	MarketQuote   = handlerminiapp.MarketQuote
	MarketSummary = handlerminiapp.MarketSummary

	SessionRowOut           = handlerminiapp.SessionRowOut
	TranscriptAttachmentOut = handlerminiapp.TranscriptAttachmentOut
	TranscriptMsgOut        = handlerminiapp.TranscriptMsgOut
	TranscriptToolTraceOut  = handlerminiapp.TranscriptToolTraceOut
	SessionSearchHitOut     = handlerminiapp.SessionSearchHitOut
	SessionSearchResult     = handlerminiapp.SessionSearchResult
	SessionFocusResult      = handlerminiapp.SessionFocusResult

	SkillRow                = handlerminiapp.SkillRow
	SkillsListResponse      = handlerminiapp.SkillsListResponse
	SkillLifecycleEvent     = handlerminiapp.SkillLifecycleEvent
	PropusLifecycleSummary  = handlerminiapp.PropusLifecycleSummary
	SkillsLifecycleResponse = handlerminiapp.SkillsLifecycleResponse
	SkillDetailResponse     = handlerminiapp.SkillDetailResponse

	SelfCorrectionCandidate             = handlerminiapp.SelfCorrectionCandidate
	SelfCorrectionImpactContract        = handlerminiapp.SelfCorrectionImpactContract
	SelfCorrectionImpactResult          = handlerminiapp.SelfCorrectionImpactResult
	SelfImprovementCodingStatusCount    = handlerminiapp.SelfImprovementCodingStatusCount
	SelfImprovementCodingFunnel         = handlerminiapp.SelfImprovementCodingFunnel
	SelfImprovementCodingListResponse   = handlerminiapp.SelfImprovementCodingListResponse
	SelfImprovementCodingRecordResponse = handlerminiapp.SelfImprovementCodingRecordResponse

	RSILoopStatusResponse = handlerminiapp.RSILoopStatusResponse
	RSIHealthView         = handlerminiapp.RSIHealthView
	RSILayerView          = handlerminiapp.RSILayerView
	RSIMetricView         = handlerminiapp.RSIMetricView

	ProjectRef = handlerminiapp.ProjectRef
	QATurn     = handlerminiapp.QATurn

	MailRowOut            = handlerminiapp.MailRowOut
	MailAttachmentOut     = handlerminiapp.MailAttachmentOut
	MailMessageOut        = handlerminiapp.MailMessageOut
	MailNativeStatusOut   = handlerminiapp.MailNativeStatusOut
	MailNativeMailboxOut  = handlerminiapp.MailNativeMailboxOut
	MailNativeOverlayOut  = handlerminiapp.MailNativeOverlayOut
	MailNativePipelineOut = handlerminiapp.MailNativePipelineOut
	MailAnalysisOut       = handlerminiapp.MailAnalysisOut
	SenderWikiHitOut      = handlerminiapp.SenderWikiHitOut
	SenderRecentOut       = handlerminiapp.SenderRecentOut
)

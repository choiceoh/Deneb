// Package handlerops re-exports domain/ops RPC handler leaves for the composition root.
package handlerops

import (
	minimodule "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp/module"
	handlerinsights "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/insights"
	handlermail "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/mail"
	handlerobservatory "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/observatory"
	handlerobserve "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/observe"
	handlerskill "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/skill"
	handlerwiki "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/wiki"
)

// --- handlerminiapp/module re-exports (import cycle + Methods name clash
// block putting these in the handlerminiapp package itself) ---

type (
	MiniContactsDeps              = minimodule.ContactsDeps
	MiniDashboardDeps             = minimodule.DashboardDeps
	MiniDashboardWorkFeedSource   = minimodule.DashboardWorkFeedSource
	MiniDependencies              = minimodule.Dependencies
	MiniMarketDeps                = minimodule.MarketDeps
	MiniRSIStatusDeps             = minimodule.RSIStatusDeps
	MiniSelfImprovementCodingDeps = minimodule.SelfImprovementCodingDeps
	MiniSessionsDeps              = minimodule.SessionsDeps
	MiniSkillsDeps                = minimodule.SkillsDeps
	MiniSyncDeps                  = minimodule.SyncDeps
	MiniTranscriptLoader          = minimodule.TranscriptLoader
)

var (
	MiniModuleMethods                = minimodule.Methods
	MiniOrgDashboardDeps             = minimodule.OrgDashboardDeps
	MiniRSIStatusMethods             = minimodule.RSIStatusMethods
	MiniSelfImprovementCodingMethods = minimodule.SelfImprovementCodingMethods
	MiniSkillsMethods                = minimodule.SkillsMethods
)

// --- insights re-exports ---

type InsightsDeps = handlerinsights.Deps

var InsightsMethods = handlerinsights.Methods

// --- observatory re-exports ---

type ObservatoryDeps = handlerobservatory.Deps

var (
	ObservatoryMethods        = handlerobservatory.Methods
	ObservatoryMiniappMethods = handlerobservatory.MiniappMethods
)

// --- observe re-exports ---

type ObserveDeps = handlerobserve.Deps

var (
	ObserveMethods        = handlerobserve.Methods
	ObserveMiniappMethods = handlerobserve.MiniappMethods
)

// --- skill re-exports ---

type (
	SkillDeps        = handlerskill.Deps
	SkillGenesisDeps = handlerskill.GenesisDeps
	SkillToolDeps    = handlerskill.ToolDeps
)

var (
	SkillGenesisMethods = handlerskill.GenesisMethods
	SkillMethods        = handlerskill.Methods
	SkillToolMethods    = handlerskill.ToolMethods
)

// --- mail re-exports (handlermail facade + gmail_context) ---

type (
	MailAnalyzePipeline   = handlermail.AnalyzePipeline
	MailCachedAnalysis    = handlermail.CachedAnalysis
	MailGmailAnalyzeDeps  = handlermail.GmailAnalyzeDeps
	MailGmailClient       = handlermail.GmailClient
	MailGmailContextDeps  = handlermail.GmailContextDeps
	MailGmailDeps         = handlermail.GmailDeps
	MailQATurn            = handlermail.QATurn
	MailStoreReader       = handlermail.MailStoreReader
	MailWikiAnalysisInput = handlermail.WikiAnalysisInput
)

var (
	ErrMailAnalyzeNoLLM          = handlermail.ErrAnalyzeNoLLM
	MailGmailAnalyzeMethods      = handlermail.GmailAnalyzeMethods
	MailGmailContextMethods      = handlermail.GmailContextMethods
	MailGmailMethods             = handlermail.GmailMethods
	MailNewAnalysisStore         = handlermail.NewAnalysisStore
	MailPipelineFromMailAnalysis = handlermail.PipelineFromMailAnalysis
)

// --- wiki re-exports ---

type WikiDeps = handlerwiki.Deps

var WikiMethods = handlerwiki.Methods

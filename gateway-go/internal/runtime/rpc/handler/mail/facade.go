// facade.go re-exports the Gmail-triage (gmailops) and mail-analysis
// (analyzebind) leaf packages' public surface under the handlermail
// package so server call sites keep importing only "handler/mail" — the
// split into leaf packages is an internal reorganization, not a wire
// contract change. Every alias below is a straight re-export (type
// alias or identical-signature var); there is no adapter logic here.
package handlermail

import (
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/mail/analyzebind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/mail/gmailops"
)

// --- gmailops re-exports (miniapp.mail.list/get/mark_read/archive/trash) ---

type (
	GmailDeps       = gmailops.GmailDeps
	GmailClient     = gmailops.GmailClient
	MailStoreReader = gmailops.MailStoreReader
	AnalysisStore   = gmailops.AnalysisStore
	CachedAnalysis  = gmailops.CachedAnalysis
)

var (
	GmailMethods     = gmailops.GmailMethods
	NewAnalysisStore = gmailops.NewAnalysisStore
)

// --- analyzebind re-exports (miniapp.mail.analyze/analysis_cached/ask) ---

type (
	GmailAnalyzeDeps  = analyzebind.GmailAnalyzeDeps
	AnalyzePipeline   = analyzebind.AnalyzePipeline
	WikiAnalysisInput = analyzebind.WikiAnalysisInput
	QATurn            = analyzebind.QATurn
)

var (
	GmailAnalyzeMethods      = analyzebind.GmailAnalyzeMethods
	PipelineFromMailAnalysis = analyzebind.PipelineFromMailAnalysis
	ErrAnalyzeNoLLM          = analyzebind.ErrAnalyzeNoLLM
)

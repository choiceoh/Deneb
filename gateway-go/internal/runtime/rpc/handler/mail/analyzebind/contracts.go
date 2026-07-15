// Package analyzebind holds the miniapp.gmail.analyze / analysis_cached /
// ask RPC handlers — the LLM-backed mail-analysis and follow-up Q&A
// surface. Split out of the parent handlermail package as a leaf (like
// the handlerminiapp dashboard/contacts children) to keep the parent's
// own import fanout small; the parent re-exports the pieces server call
// sites need via thin aliases.
//
// This package depends on the sibling gmailops package for the Gmail
// client contract and the on-disk analysis cache both packages share.
package analyzebind

import (
	"context"

	handlerminiapp "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp"
	miniknowledge "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp/knowledge"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/mail/gmailops"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/minibind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// ProjectRef is the generated-wire project reference shared with native clients.
type ProjectRef = handlerminiapp.ProjectRef

// QATurn is the generated-wire mail Q&A turn shared with native clients.
type QATurn = handlerminiapp.QATurn

// MemorySearcher is the existing miniapp wiki contract used by project enrichment.
type MemorySearcher = miniknowledge.MemorySearcher

type mailAnalysisOut = handlerminiapp.MailAnalysisOut

// GmailClient, the on-disk analysis cache, and the shared Gmail-error /
// workflow-state helpers live in the sibling gmailops package — both
// packages need to talk about the exact same cache and error shapes.
type (
	GmailClient    = gmailops.GmailClient
	AnalysisStore  = gmailops.AnalysisStore
	AnalysisRecord = gmailops.AnalysisRecord
	CachedAnalysis = gmailops.CachedAnalysis
)

const AnalysisPromptVersion = gmailops.AnalysisPromptVersion

var (
	NewAnalysisStore       = gmailops.NewAnalysisStore
	mapGmailError          = gmailops.MapGmailError
	errGmailNotFound       = gmailops.ErrGmailNotFound
	normalizeDate          = gmailops.NormalizeDate
	messageInputFromDetail = gmailops.MessageInputFromDetail
)

func bindOptional[P any](next func(context.Context, *protocol.RequestFrame, P) *protocol.ResponseFrame) rpcutil.HandlerFunc {
	return minibind.BindOptional(next)
}

package handlermail

import (
	"context"

	handlerminiapp "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp"
	miniknowledge "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp/knowledge"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/minibind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// MemorySearcher is the existing miniapp wiki contract used by mail context and project enrichment.
type MemorySearcher = miniknowledge.MemorySearcher

// ProjectRef is the generated-wire project reference shared with native clients.
type ProjectRef = handlerminiapp.ProjectRef

// QATurn is the generated-wire mail Q&A turn shared with native clients.
type QATurn = handlerminiapp.QATurn

type (
	mailRowOut            = handlerminiapp.MailRowOut
	mailAttachmentOut     = handlerminiapp.MailAttachmentOut
	mailMessageOut        = handlerminiapp.MailMessageOut
	mailNativeStatusOut   = handlerminiapp.MailNativeStatusOut
	mailNativeMailboxOut  = handlerminiapp.MailNativeMailboxOut
	mailNativeOverlayOut  = handlerminiapp.MailNativeOverlayOut
	mailNativePipelineOut = handlerminiapp.MailNativePipelineOut
	mailAnalysisOut       = handlerminiapp.MailAnalysisOut
	senderWikiHitOut      = handlerminiapp.SenderWikiHitOut
	senderRecentOut       = handlerminiapp.SenderRecentOut
)

func authenticated(next rpcutil.HandlerFunc) rpcutil.HandlerFunc {
	return minibind.Authenticated(next)
}

func bind[P any](next func(context.Context, *protocol.RequestFrame, P) *protocol.ResponseFrame) rpcutil.HandlerFunc {
	return minibind.Bind(next)
}

func bindOptional[P any](next func(context.Context, *protocol.RequestFrame, P) *protocol.ResponseFrame) rpcutil.HandlerFunc {
	return minibind.BindOptional(next)
}

func parseSender(raw string) (email, display string) {
	return miniknowledge.ParseSender(raw)
}

func looksLikeEmail(s string) bool {
	return miniknowledge.LooksLikeEmail(s)
}

// Package gmailops holds the miniapp.mail.* Gmail-triage RPC handlers
// (list/get/mark_read/archive/trash/native_status) plus the on-disk
// analysis cache they share with the analyzebind package. Split out of
// the parent handlermail package as a leaf (like the handlerminiapp
// dashboard/contacts children) to keep the parent's own import fanout
// small; the parent re-exports the pieces server call sites need via
// thin aliases.
package gmailops

import (
	"context"

	miniappcontract "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp/contract"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/minibind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// ProjectRef is the generated-wire project reference shared with native clients.
type ProjectRef = miniappcontract.ProjectRef

type (
	mailRowOut            = miniappcontract.MailRowOut
	mailAttachmentOut     = miniappcontract.MailAttachmentOut
	mailMessageOut        = miniappcontract.MailMessageOut
	mailNativeStatusOut   = miniappcontract.MailNativeStatusOut
	mailNativeMailboxOut  = miniappcontract.MailNativeMailboxOut
	mailNativeOverlayOut  = miniappcontract.MailNativeOverlayOut
	mailNativePipelineOut = miniappcontract.MailNativePipelineOut
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

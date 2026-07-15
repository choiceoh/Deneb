package handlermail

import (
	"context"

	handlerminiapp "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp"
	miniknowledge "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp/knowledge"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/mail/gmailops"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/minibind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// MemorySearcher is the existing miniapp wiki contract used by mail context and project enrichment.
type MemorySearcher = miniknowledge.MemorySearcher

type (
	senderWikiHitOut = handlerminiapp.SenderWikiHitOut
	senderRecentOut  = handlerminiapp.SenderRecentOut
)

func bindOptional[P any](next func(context.Context, *protocol.RequestFrame, P) *protocol.ResponseFrame) rpcutil.HandlerFunc {
	return minibind.BindOptional(next)
}

func parseSender(raw string) (email, display string) {
	return miniknowledge.ParseSender(raw)
}

func looksLikeEmail(s string) bool {
	return miniknowledge.LooksLikeEmail(s)
}

func normalizeDate(raw string) string {
	return gmailops.NormalizeDate(raw)
}

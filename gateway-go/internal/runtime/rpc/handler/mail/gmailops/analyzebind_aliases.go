package gmailops

import (
	"github.com/choiceoh/deneb/gateway-go/internal/core/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailwork"
)

// Mail-work + rpcerr aliases for analyzebind so that package stays ≤ soft fanout
// without growing its own import set (gmailops already imports both).
type (
	WorkStore         = mailwork.Store
	WorkMessageState  = mailwork.MessageState
	WorkMessageInput  = mailwork.MessageInput
	WorkAnalysisInput = mailwork.AnalysisInput
)

var (
	RPCMissingParam    = rpcerr.MissingParam
	RPCWrapUnavailable = rpcerr.WrapUnavailable
	RPCNotFound        = rpcerr.NotFound
	RPCUnavailable     = rpcerr.Unavailable
)

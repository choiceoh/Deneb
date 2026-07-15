// Package leafbind aggregates rarely-used leaf imports for the chat package
// root so direct fan-out stays under the soft boundary bar.
package leafbind

import (
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelcaps"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/router"
	"github.com/choiceoh/deneb/gateway-go/internal/core/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/shortid"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/chatportwire"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/recall"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/subagent"
)

type (
	Capability = modelcaps.Capability
	Profile    = router.Profile
	Request    = router.Request

	SubagentNotifier     = subagent.SubagentNotifier
	SubagentNotifierDeps = subagent.SubagentNotifierDeps

	RecallDeps      = recall.Deps
	RecallOrgLoader = recall.OrgLoader
	RecallParams    = recall.Params
	FileRecallHit   = recall.FileRecallHit
	FileRecallFunc  = recall.FileRecallFunc
)

var (
	Builtin                   = modelcaps.Builtin
	RejectsCacheControl       = modelcaps.RejectsCacheControl
	DefaultProfile            = router.DefaultProfile
	Decide                    = router.Decide
	NewShortID                = shortid.New
	ResolveStateDir           = config.ResolveStateDir
	LoadConfigFromDefaultPath = config.LoadConfigFromDefaultPath
	ResolveAgentWorkspaceDir  = config.ResolveAgentWorkspaceDir
	NewSubagentNotifier       = subagent.NewSubagentNotifier

	RPCNew                  = rpcerr.New
	RPCNewf                 = rpcerr.Newf
	RPCMissingParam         = rpcerr.MissingParam
	RPCNotFound             = rpcerr.NotFound
	RPCInvalidRequest       = rpcerr.InvalidRequest
	RPCWrapInvalidRequest   = rpcerr.WrapInvalidRequest
	RPCWrapDependencyFailed = rpcerr.WrapDependencyFailed

	// recall
	RecallBuild          = recall.Build
	RecallCachedSnapshot = recall.CachedSnapshot
	RecallClearSession   = recall.ClearSession
	RecallCueFingerprint = recall.CueFingerprint
	RecallShouldFreeze   = recall.ShouldFreeze
	RecallStoreSnapshot  = recall.StoreSnapshot

	// chatportwire
	ChatportClassify             = chatportwire.Classify
	ChatportNewTypingSignaler    = chatportwire.NewTypingSignaler
	ChatportParseReplyDirectives = chatportwire.ParseReplyDirectives
	ChatportSanitizeDraft        = chatportwire.SanitizeDraft
)

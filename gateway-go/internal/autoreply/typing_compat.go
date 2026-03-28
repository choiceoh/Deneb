// typing_compat.go — temporary re-exports during subpackage refactoring.
// TODO: Remove after all callers are updated to import autoreply/typing directly.
package autoreply

import "github.com/choiceoh/deneb/gateway-go/internal/autoreply/typing"

// Type aliases — autoreply.TypingController == typing.TypingController, etc.
type TypingController = typing.TypingController
type TypingControllerConfig = typing.TypingControllerConfig
type TypingSignaler = typing.TypingSignaler
type TypingMode = typing.TypingMode
type TypingModeContext = typing.TypingModeContext
type FullTypingSignaler = typing.FullTypingSignaler
type ResolveRunTypingPolicyParams = typing.ResolveRunTypingPolicyParams
type ResolvedRunTypingPolicy = typing.ResolvedRunTypingPolicy

// Function re-exports.
var NewTypingController = typing.NewTypingController
var NewTypingSignaler = typing.NewTypingSignaler
var NewFullTypingSignaler = typing.NewFullTypingSignaler
var ResolveTypingMode = typing.ResolveTypingMode
var ResolveRunTypingPolicy = typing.ResolveRunTypingPolicy

// TypingMode constants — constants cannot be aliased; copy the values.
const (
	TypingModeInstant  TypingMode = "instant"
	TypingModeMessage  TypingMode = "message"
	TypingModeThinking TypingMode = "thinking"
	TypingModeNever    TypingMode = "never"
)

// DefaultGroupTypingMode is the typing mode used for unmentioned group messages.
const DefaultGroupTypingMode TypingMode = TypingModeMessage

// InternalMessageChannel is the well-known channel name for internal webchat messages.
const InternalMessageChannel = "webchat"

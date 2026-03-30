package chat

import (
	"github.com/choiceoh/deneb/gateway-go/internal/chat/toolctx"
)

// Type aliases — all wire types are defined in toolctx/ (the leaf package).
// These aliases preserve backward compatibility for external consumers.

type BroadcastFunc = toolctx.BroadcastFunc
type ReplyFunc = toolctx.ReplyFunc
type TypingFunc = toolctx.TypingFunc
type ReactionFunc = toolctx.ReactionFunc
type ToolProgressFunc = toolctx.ToolProgressFunc
type ToolProgressEvent = toolctx.ToolProgressEvent
type ProviderConfig = toolctx.ProviderConfig
type DeliveryContext = toolctx.DeliveryContext
type ChatMessage = toolctx.ChatMessage
type ChatAttachment = toolctx.ChatAttachment
type AbortEntry = toolctx.AbortEntry
type MediaSendFunc = toolctx.MediaSendFunc

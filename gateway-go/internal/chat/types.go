package chat

import (
	"github.com/choiceoh/deneb/gateway-go/internal/chat/toolctx"
)

// Type aliases — all wire types are defined in toolctx/ (the leaf package).
// These aliases preserve backward compatibility for external consumers.

// BroadcastFunc sends an event to all matching subscribers.
type BroadcastFunc = toolctx.BroadcastFunc

// ReplyFunc delivers the assistant response back to the originating channel.
type ReplyFunc = toolctx.ReplyFunc

// TypingFunc signals a typing indicator to the originating channel.
type TypingFunc = toolctx.TypingFunc

// ReactionFunc sets or removes an emoji reaction on the triggering message.
type ReactionFunc = toolctx.ReactionFunc

// DraftEditFunc sends or edits a streaming draft message on the originating channel.
type DraftEditFunc = toolctx.DraftEditFunc

// DraftDeleteFunc deletes a streaming draft message from the originating channel.
type DraftDeleteFunc = toolctx.DraftDeleteFunc

// ToolProgressFunc is called during agent execution to report tool lifecycle events.
type ToolProgressFunc = toolctx.ToolProgressFunc

// ToolProgressEvent describes a tool execution lifecycle event (start or complete).
type ToolProgressEvent = toolctx.ToolProgressEvent

// ProviderConfig holds credentials and endpoint for an LLM provider.
type ProviderConfig = toolctx.ProviderConfig

// DeliveryContext carries channel routing information for a chat message.
type DeliveryContext = toolctx.DeliveryContext

// ChatMessage represents a message in a session transcript.
type ChatMessage = toolctx.ChatMessage

// ChatAttachment represents a file or media attachment on a chat message.
type ChatAttachment = toolctx.ChatAttachment

// AbortEntry tracks an active abort controller for a running chat session.
type AbortEntry = toolctx.AbortEntry

// MediaSendFunc delivers a file to the originating channel.
type MediaSendFunc = toolctx.MediaSendFunc

// NewTextChatMessage creates a ChatMessage with text-only content.
var NewTextChatMessage = toolctx.NewTextChatMessage

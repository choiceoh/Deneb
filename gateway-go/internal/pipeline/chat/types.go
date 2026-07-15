package chat

import (
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
)

// Type aliases — all wire types are defined in toolport/ (the leaf package).
// These aliases preserve backward compatibility for external consumers.

// BroadcastFunc sends an event to all matching subscribers.
type BroadcastFunc = toolport.BroadcastFunc

// replyFunc delivers the assistant response back to the originating channel.
type replyFunc = toolport.ReplyFunc

// typingFunc signals a typing indicator to the originating channel.
type typingFunc = toolport.TypingFunc

// reactionFunc sets or removes an emoji reaction on the triggering message.
type reactionFunc = toolport.ReactionFunc

// draftEditFunc sends or edits a streaming draft message on the originating channel.
type draftEditFunc = toolport.DraftEditFunc

// messageDeleter removes a previously-sent message from the originating channel.
type messageDeleter = toolport.MessageDeleter

// ProviderConfig holds credentials and endpoint for an LLM provider.
type ProviderConfig = toolport.ProviderConfig

// RoutingConfig is the deneb.json per-model effort-router tuning block.
type RoutingConfig = toolport.RoutingConfig

// DeliveryContext carries channel routing information for a chat message.
type DeliveryContext = toolport.DeliveryContext

// ChatMessage represents a message in a session transcript.
type ChatMessage = toolport.ChatMessage

// ChatAttachment represents a file or media attachment on a chat message.
type ChatAttachment = toolport.ChatAttachment

// abortEntry tracks an active abort controller for a running chat session.
type abortEntry = toolport.AbortEntry

// mediaSendFunc delivers a file to the originating channel.
type mediaSendFunc = toolport.MediaSendFunc

// NewTextChatMessage creates a ChatMessage with text-only content.
var NewTextChatMessage = toolport.NewTextChatMessage

// stripUserMessageTimestamp removes the baked "[<RFC3339>] " prefix from a
// user message text (see the transcript persist site in run_exec.go).
var stripUserMessageTimestamp = toolport.StripUserMessageTimestamp

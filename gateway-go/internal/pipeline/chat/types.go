package chat

import (
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/modelport"
)

// Type aliases preserve backward compatibility for external consumers while
// the owning contracts live in focused leaf packages.

// BroadcastFunc sends an event to all matching subscribers.
type BroadcastFunc = toolport.BroadcastFunc

// ReplyFunc delivers the assistant response back to the originating channel.
type ReplyFunc = toolport.ReplyFunc

// TypingFunc signals a typing indicator to the originating channel.
type TypingFunc = toolport.TypingFunc

// ReactionFunc sets or removes an emoji reaction on the triggering message.
type ReactionFunc = toolport.ReactionFunc

// DraftEditFunc sends or edits a streaming draft message on the originating channel.
type DraftEditFunc = toolport.DraftEditFunc

// MessageDeleter removes a previously-sent message from the originating channel.
type MessageDeleter = toolport.MessageDeleter

// ProviderConfig holds credentials and endpoint for an LLM provider.
type ProviderConfig = modelport.ProviderConfig

// RoutingConfig is the deneb.json per-model effort-router tuning block.
type RoutingConfig = modelport.RoutingConfig

// DeliveryContext carries channel routing information for a chat message.
type DeliveryContext = toolport.DeliveryContext

// ChatMessage represents a message in a session transcript.
type ChatMessage = toolport.ChatMessage

// ChatAttachment represents a file or media attachment on a chat message.
type ChatAttachment = toolport.ChatAttachment

// AbortEntry tracks an active abort controller for a running chat session.
type AbortEntry = toolport.AbortEntry

// MediaSendFunc delivers a file to the originating channel.
type MediaSendFunc = toolport.MediaSendFunc

// NewTextChatMessage creates a ChatMessage with text-only content.
var NewTextChatMessage = toolport.NewTextChatMessage

// StripUserMessageTimestamp removes the baked "[<RFC3339>] " prefix from a
// user message text (see the transcript persist site in run_exec.go).
var StripUserMessageTimestamp = toolport.StripUserMessageTimestamp

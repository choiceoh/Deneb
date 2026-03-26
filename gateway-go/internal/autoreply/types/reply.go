package types

import "context"

// ReplyPayload represents an outbound reply message.
type ReplyPayload struct {
	Text         string         `json:"text,omitempty"`
	MediaURL     string         `json:"mediaUrl,omitempty"`
	MediaURLs    []string       `json:"mediaUrls,omitempty"`
	ReplyToID    string         `json:"replyToId,omitempty"`
	AudioAsVoice bool           `json:"audioAsVoice,omitempty"`
	IsError      bool           `json:"isError,omitempty"`
	ChannelData  map[string]any `json:"channelData,omitempty"`
}

// TypingPolicy describes the context that triggered a reply.
type TypingPolicy string

const (
	TypingPolicyUserMessage TypingPolicy = "user_message"
	TypingPolicySystemEvent TypingPolicy = "system_event"
	TypingPolicyInternalWeb TypingPolicy = "internal_webchat"
	TypingPolicyHeartbeat   TypingPolicy = "heartbeat"
)

// ReplyDispatchKind identifies the stage of a reply in the dispatch pipeline.
type ReplyDispatchKind string

const (
	DispatchKindTool  ReplyDispatchKind = "tool"
	DispatchKindBlock ReplyDispatchKind = "block"
	DispatchKindFinal ReplyDispatchKind = "final"
)

// DeliverFunc delivers a single reply payload to the originating channel.
type DeliverFunc func(ctx context.Context, payload ReplyPayload, kind ReplyDispatchKind) error

// MessagingToolTarget describes where a messaging tool sent a message.
type MessagingToolTarget struct {
	Provider  string `json:"provider,omitempty"`
	To        string `json:"to"`
	AccountID string `json:"accountId,omitempty"`
}

// BuildReplyPayloadsParams configures reply payload processing.
type BuildReplyPayloadsParams struct {
	Payloads         []ReplyPayload
	IsHeartbeat      bool
	CurrentMessageID string
	MessageProvider  string
	SentTexts        []string
	SentMediaURLs    []string
	SentTargets      []MessagingToolTarget
	OriginTo         string
	AccountID        string
}

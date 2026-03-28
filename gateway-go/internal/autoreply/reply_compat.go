// reply_compat.go — Re-exports for symbols moved to autoreply/reply subpackage.
// TODO: Remove after all callers inside autoreply/ are updated to import autoreply/reply directly.
package autoreply

import (
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/reply"
)

// --- send_policy.go ---

// SendPolicy controls whether outbound messages are delivered.
type SendPolicy = reply.SendPolicy

const (
	SendPolicyOn      = reply.SendPolicyOn
	SendPolicyOff     = reply.SendPolicyOff
	SendPolicyInherit = reply.SendPolicyInherit
)

// NormalizeSendPolicy validates and normalizes a send policy string.
var NormalizeSendPolicy = reply.NormalizeSendPolicy

// IsSendAllowed returns true if the effective send policy allows sending.
var IsSendAllowed = reply.IsSendAllowed

// --- normalize_reply.go ---

// NormalizeOpts configures reply normalization.
type NormalizeOpts = reply.NormalizeOpts

// NormalizeReplyPayload cleans up a reply payload before delivery.
var NormalizeReplyPayload = reply.NormalizeReplyPayload

// FilterReplyPayloads normalizes a slice of payloads, removing those that should be skipped.
var FilterReplyPayloads = reply.FilterReplyPayloads

// DeduplicateReplyPayloads removes duplicate text and media from payloads.
var DeduplicateReplyPayloads = reply.DeduplicateReplyPayloads

// --- reply_payloads.go ---

// FilterMessagingToolDuplicates removes payloads whose text was already sent by a messaging tool.
var FilterMessagingToolDuplicates = reply.FilterMessagingToolDuplicates

// FilterMessagingToolMediaDuplicates removes media URLs already sent by messaging tools.
var FilterMessagingToolMediaDuplicates = reply.FilterMessagingToolMediaDuplicates

// IsRenderablePayload returns true if the payload has content worth delivering.
var IsRenderablePayload = reply.IsRenderablePayload

// ShouldSuppressMessagingToolReplies returns true if the messaging tool already delivered to the same target.
var ShouldSuppressMessagingToolReplies = reply.ShouldSuppressMessagingToolReplies

// FormatBtwTextForExternalDelivery wraps BTW (side question) text for delivery.
var FormatBtwTextForExternalDelivery = reply.FormatBtwTextForExternalDelivery

// NormalizeReplyPayloadDirectives processes [[tag]] directives in reply text.
var NormalizeReplyPayloadDirectives = reply.NormalizeReplyPayloadDirectives

// BuildReplyPayloads processes raw payloads from an agent turn into deliverable reply payloads.
var BuildReplyPayloads = reply.BuildReplyPayloads

// --- response_prefix.go ---

// ResponsePrefixTemplate defines the format for response prefix headers.
type ResponsePrefixTemplate = reply.ResponsePrefixTemplate

// ResponsePrefixParams holds the values for response prefix formatting.
type ResponsePrefixParams = reply.ResponsePrefixParams

// FormatResponsePrefix builds a response prefix string from a template.
var FormatResponsePrefix = reply.FormatResponsePrefix

// --- response_prefix_template.go ---

// ResponsePrefixContext holds values for template variable interpolation.
type ResponsePrefixContext = reply.ResponsePrefixContext

// ResolveResponsePrefixTemplate interpolates template variables in a response prefix string.
var ResolveResponsePrefixTemplate = reply.ResolveResponsePrefixTemplate

// ExtractShortModelName strips provider prefix and date/version suffixes from a full model string.
var ExtractShortModelName = reply.ExtractShortModelName

// HasTemplateVariables returns true if the template string contains any {variable} placeholders.
var HasTemplateVariables = reply.HasTemplateVariables

// --- envelope.go ---

// EnvelopeFormatOptions controls how inbound/outbound envelopes are formatted.
type EnvelopeFormatOptions = reply.EnvelopeFormatOptions

// InboundEnvelopeParams holds the data for formatting an inbound envelope.
type InboundEnvelopeParams = reply.InboundEnvelopeParams

// AgentEnvelopeParams holds the data for formatting an agent envelope.
type AgentEnvelopeParams = reply.AgentEnvelopeParams

// DefaultEnvelopeOptions returns sensible defaults for envelope formatting.
var DefaultEnvelopeOptions = reply.DefaultEnvelopeOptions

// FormatEnvelopeTimestamp formats a timestamp for envelope headers.
var FormatEnvelopeTimestamp = reply.FormatEnvelopeTimestamp

// FormatInboundFromLabel builds a sender label for inbound messages.
var FormatInboundFromLabel = reply.FormatInboundFromLabel

// FormatInboundEnvelope builds the metadata header for an inbound message.
var FormatInboundEnvelope = reply.FormatInboundEnvelope

// FormatAgentEnvelope builds the metadata header for an outbound (agent) message.
var FormatAgentEnvelope = reply.FormatAgentEnvelope

// --- templating.go ---

// TemplateVars holds variables available for message template interpolation.
type TemplateVars = reply.TemplateVars

// ApplyTemplate interpolates {{variable}} placeholders in a template string.
var ApplyTemplate = reply.ApplyTemplate

// ResolveCurrentTimeString returns the current time formatted for templates.
var ResolveCurrentTimeString = reply.ResolveCurrentTimeString

// --- mentions.go ---

// MentionPattern detects @mentions in message text.
var MentionPattern = reply.MentionPattern

// GroupContext holds context for group chat messages.
type GroupContext = reply.GroupContext

// ChannelContext holds channel-specific context.
type ChannelContext = reply.ChannelContext

// TelegramContext holds Telegram-specific message context.
type TelegramContext = reply.TelegramContext

// ReplyReference holds a reference to a message being replied to.
type ReplyReference = reply.ReplyReference

// ReplyThreading resolves threading for a reply.
type ReplyThreading = reply.ReplyThreading

// MediaPathResolver resolves media file paths for delivery.
type MediaPathResolver = reply.MediaPathResolver

// ReplyDeliveryConfig handles the final delivery of a reply to a channel.
type ReplyDeliveryConfig = reply.ReplyDeliveryConfig

// AudioTag represents audio metadata.
type AudioTag = reply.AudioTag

// ExtractMentions returns all @mentioned usernames from text.
var ExtractMentions = reply.ExtractMentions

// ContainsMention checks if text mentions a specific username.
var ContainsMention = reply.ContainsMention

// ExtractInboundText extracts the text body from an inbound message context.
var ExtractInboundText = reply.ExtractInboundText

// StripInboundMeta removes metadata markers from message text.
var StripInboundMeta = reply.StripInboundMeta

// ReplyInline wraps text for inline reply display.
var ReplyInline = reply.ReplyInline

// NormalizeInlineWhitespace collapses whitespace in inline replies.
var NormalizeInlineWhitespace = reply.NormalizeInlineWhitespace

// ResolveReplyThreading determines the threading for a reply payload.
var ResolveReplyThreading = reply.ResolveReplyThreading

// --- reply_directives.go ---

// ReplyDirectiveParseResult holds the result of parsing reply directives.
type ReplyDirectiveParseResult = reply.ReplyDirectiveParseResult

// ParseReplyDirectives parses reply directives from raw agent output text.
var ParseReplyDirectives = reply.ParseReplyDirectives

package reply

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// EnvelopeFormatOptions controls how inbound/outbound envelopes are formatted.
type EnvelopeFormatOptions struct {
	ShowTimestamp bool   `json:"showTimestamp,omitempty"`
	Timezone      string `json:"timezone,omitempty"` // "utc", "local", or IANA string
	ShowSender    bool   `json:"showSender,omitempty"`
	ShowChannel   bool   `json:"showChannel,omitempty"`
}

// DefaultEnvelopeOptions returns sensible defaults for envelope formatting.
func DefaultEnvelopeOptions() EnvelopeFormatOptions {
	return EnvelopeFormatOptions{
		ShowTimestamp: true,
		Timezone:      "utc",
		ShowSender:    true,
		ShowChannel:   false,
	}
}

// FormatEnvelopeTimestamp formats a timestamp for envelope headers.
func FormatEnvelopeTimestamp(t time.Time, timezone string) string {
	tz := strings.TrimSpace(strings.ToLower(timezone))
	switch tz {
	case "", "utc":
		return t.UTC().Format("2006-01-02 15:04:05 MST")
	case "local":
		return t.Local().Format("2006-01-02 15:04:05 MST")
	default:
		loc, err := time.LoadLocation(timezone)
		if err != nil {
			return t.UTC().Format("2006-01-02 15:04:05 MST")
		}
		return t.In(loc).Format("2006-01-02 15:04:05 MST")
	}
}

// FormatInboundFromLabel builds a sender label for inbound messages.
func FormatInboundFromLabel(from string, isGroup bool, senderID string) string {
	sanitized := sanitizeEnvelopeHeaderPart(from)
	if sanitized == "" {
		sanitized = "User"
	}
	if isGroup && senderID != "" {
		return fmt.Sprintf("%s (%s)", sanitized, senderID)
	}
	return sanitized
}

// FormatInboundEnvelope builds the metadata header for an inbound message.
func FormatInboundEnvelope(params InboundEnvelopeParams) string {
	opts := params.Options
	if opts == nil {
		defaults := DefaultEnvelopeOptions()
		opts = &defaults
	}

	var parts []string

	if opts.ShowChannel && params.Channel != "" {
		parts = append(parts, fmt.Sprintf("[%s]", sanitizeEnvelopeHeaderPart(params.Channel)))
	}

	if opts.ShowSender && params.From != "" {
		label := FormatInboundFromLabel(params.From, params.IsGroup, params.SenderID)
		parts = append(parts, label)
	}

	if opts.ShowTimestamp {
		ts := FormatEnvelopeTimestamp(params.Timestamp, opts.Timezone)
		parts = append(parts, ts)
	}

	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " | ")
}

// InboundEnvelopeParams holds the data for formatting an inbound envelope.
type InboundEnvelopeParams struct {
	Channel   string
	From      string
	SenderID  string
	IsGroup   bool
	Timestamp time.Time
	Options   *EnvelopeFormatOptions
}

// FormatAgentEnvelope builds the metadata header for an outbound (agent) message.
func FormatAgentEnvelope(params AgentEnvelopeParams) string {
	opts := params.Options
	if opts == nil {
		defaults := DefaultEnvelopeOptions()
		opts = &defaults
	}

	var parts []string

	if opts.ShowTimestamp {
		ts := FormatEnvelopeTimestamp(params.Timestamp, opts.Timezone)
		parts = append(parts, ts)
	}

	if params.ElapsedMs > 0 {
		parts = append(parts, fmt.Sprintf("%.1fs", float64(params.ElapsedMs)/1000.0))
	}

	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " | ")
}

// AgentEnvelopeParams holds the data for formatting an agent envelope.
type AgentEnvelopeParams struct {
	Timestamp time.Time
	ElapsedMs int64
	Options   *EnvelopeFormatOptions
}

var (
	newlineCollapseRe = regexp.MustCompile(`[\r\n]+`)
	bracketRe         = regexp.MustCompile(`[\[\]]`)
)

// sanitizeEnvelopeHeaderPart collapses newlines and neutralizes bracket chars.
func sanitizeEnvelopeHeaderPart(s string) string {
	s = newlineCollapseRe.ReplaceAllString(s, " ")
	s = bracketRe.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

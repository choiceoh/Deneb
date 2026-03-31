// Session envelope — packages session key, metadata, and timestamps
// for inbound message processing.
//
// Mirrors src/channels/session-envelope.ts.
package telegram

import "time"

// EnvelopeFormatOptions controls how session envelopes are formatted.
type EnvelopeFormatOptions struct {
	IncludeTimestamp bool
	TimestampFormat  string
}

// SessionEnvelopeContext holds resolved session envelope data for an inbound message.
type SessionEnvelopeContext struct {
	StorePath         string
	EnvelopeOptions   EnvelopeFormatOptions
	PreviousTimestamp *time.Time
}

// ResolveInboundSessionEnvelopeContext resolves the session envelope context
// for an inbound message. storePath is the session store directory,
// agentID and sessionKey identify the session.
func ResolveInboundSessionEnvelopeContext(storePath, agentID, sessionKey string, previousUpdatedAt *time.Time) SessionEnvelopeContext {
	return SessionEnvelopeContext{
		StorePath: storePath,
		EnvelopeOptions: EnvelopeFormatOptions{
			IncludeTimestamp: true,
			TimestampFormat:  time.RFC3339,
		},
		PreviousTimestamp: previousUpdatedAt,
	}
}

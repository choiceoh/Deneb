// Package coreprotocol provides pure-Go gateway protocol frame validation and
// RPC parameter schema validation — a port of core-rs/core/src/protocol/.
package coreprotocol

// Protocol constants mirrored from TypeScript sources.
// These values MUST stay in sync with their TypeScript counterparts.

const (
	// SessionLabelMaxLength is the max length for session labels.
	SessionLabelMaxLength = 512

	// ChatSendSessionKeyMaxLength is the max length for chat.send session keys.
	ChatSendSessionKeyMaxLength = 512

	// ExecSecretRefIDPattern is the regex pattern for exec secret ref IDs.
	// Used in error messages only; actual validation uses IsValidExecSecretRefID.
	ExecSecretRefIDPattern = `^(?!.*(?:^|/)\.{1,2}(?:/|$))[A-Za-z0-9][A-Za-z0-9._:/-]{0,255}$`
)

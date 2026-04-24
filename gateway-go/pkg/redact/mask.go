// Copyright (c) Deneb authors. Licensed under the project license.

package redact

// maskToken masks a single secret value.
//
// Short tokens (< 18 chars) carry too little prefix entropy to safely expose
// the head, so they are fully masked as "***". Longer tokens preserve the
// first 6 and last 4 characters for debuggability (matching the format used
// by the Hermes reference implementation).
//
// If the token already looks like a previous mask output ("***" or the
// "prefix...suffix" form), it is returned unchanged. This keeps the pipeline
// idempotent when the same string is passed through multiple times
// (e.g. a log line that is already cached then re-emitted).
//
// Callers should use this for the token portion of a matched pattern, never
// for the surrounding structure.
func maskToken(token string) string {
	if isAlreadyMasked(token) {
		return token
	}
	if len(token) < 18 {
		return "***"
	}
	return token[:6] + "..." + token[len(token)-4:]
}

// isAlreadyMasked reports whether s matches one of the shapes maskToken
// itself can emit: the literal "***" or the "prefix6...suffix4" form.
// Runtime cost is a single strings.Contains check after a length bound.
func isAlreadyMasked(s string) bool {
	if s == "***" {
		return true
	}
	// The mask form is always exactly "prefix6...suffix4" == 13 chars.
	if len(s) == 13 && s[6:9] == "..." {
		return true
	}
	return false
}

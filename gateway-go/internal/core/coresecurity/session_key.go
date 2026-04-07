package coresecurity

import (
	"errors"
	"unicode"
)

// maxSessionKeyLen matches Rust MAX_SESSION_KEY_LEN and TypeScript maxLength.
const maxSessionKeyLen = 512

// ValidateSessionKey validates a session key: non-empty, max 512 characters,
// no control characters (except \n, \t, \r). Uses char (rune) count, not
// byte length, to match TypeScript semantics.
func ValidateSessionKey(key string) error {
	if key == "" {
		return errors.New("coresecurity: empty session key")
	}
	count := 0
	for _, r := range key {
		count++
		if count > maxSessionKeyLen {
			return errors.New("coresecurity: session key too long")
		}
		if unicode.IsControl(r) && r != '\n' && r != '\t' && r != '\r' {
			return errors.New("coresecurity: invalid session key")
		}
	}
	return nil
}

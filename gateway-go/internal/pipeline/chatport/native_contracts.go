package chatport

import "strings"

// NativeClientChannel is the channel identity for standalone native-client chat
// turns. It keys the session, delivery, and the system prompt's runtime line.
const NativeClientChannel = "client"

// DefaultNativeSessionKey normalizes the native client's optional session key
// onto the shared default conversation so blocking and streaming bridges cannot
// drift.
func DefaultNativeSessionKey(sessionKey string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return NativeClientChannel + ":main"
	}
	return sessionKey
}

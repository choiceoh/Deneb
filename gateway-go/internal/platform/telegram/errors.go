package telegram

import (
	"errors"
	"net"
	"strings"
	"syscall"
)

// --- Network error classification ---
// Ported from extensions/telegram/src/network-errors.ts.

// preConnectErrnos are syscall errors that occur before the request reaches
// Telegram's servers. Safe to retry even for non-idempotent methods (sendMessage)
// because the message was definitely not delivered.
var preConnectErrnos = map[syscall.Errno]bool{
	syscall.ECONNREFUSED: true, // Server actively refused the connection
	syscall.ENETUNREACH:  true, // No route to host
	syscall.EHOSTUNREACH: true, // Host unreachable
}

// recoverableErrnos are syscall errors that indicate a network failure.
// Only safe to retry for idempotent methods because the request may have
// already been processed by Telegram.
var recoverableErrnos = map[syscall.Errno]bool{
	syscall.ECONNRESET:   true,
	syscall.ECONNREFUSED: true,
	syscall.EPIPE:        true,
	syscall.ETIMEDOUT:    true,
	syscall.ENETUNREACH:  true,
	syscall.EHOSTUNREACH: true,
	syscall.ECONNABORTED: true,
}

// IsPreConnectError returns true for errors that occurred before reaching Telegram.
// Safe to retry even for non-idempotent methods (sendMessage, sendPhoto, etc.)
// because the message was definitely not delivered.
func IsPreConnectError(err error) bool {
	if err == nil {
		return false
	}

	// Check for DNS resolution failures (ENOTFOUND / EAI_AGAIN equivalent).
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}

	// Check for pre-connect syscall errors.
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return preConnectErrnos[errno]
	}

	// Check for OpError wrapping syscall errors.
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		if opErr.Op == "dial" {
			return true
		}
		var innerErrno syscall.Errno
		if errors.As(opErr.Err, &innerErrno) {
			return preConnectErrnos[innerErrno]
		}
	}

	return false
}

// IsNetworkError returns true for any recoverable network error.
// Only safe to retry for idempotent methods because the request may have
// already been processed by Telegram.
func IsNetworkError(err error) bool {
	if err == nil {
		return false
	}

	if IsPreConnectError(err) {
		return true
	}

	var errno syscall.Errno
	if errors.As(err, &errno) {
		return recoverableErrnos[errno]
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) {
		if opErr.Timeout() {
			return true
		}
		var innerErrno syscall.Errno
		if errors.As(opErr.Err, &innerErrno) {
			return recoverableErrnos[innerErrno]
		}
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	msg := strings.ToLower(err.Error())
	for _, snippet := range []string{"connection reset", "broken pipe", "i/o timeout"} {
		if strings.Contains(msg, snippet) {
			return true
		}
	}

	return false
}

// IsFallbackTrigger returns true if the error should trigger a transport
// fallback (e.g. from dual-stack to IPv4-only to pinned IP).
// Ported from FALLBACK_RETRY_ERROR_CODES in extensions/telegram/src/fetch.ts.
func IsFallbackTrigger(err error) bool {
	if err == nil {
		return false
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	var errno syscall.Errno
	if errors.As(err, &errno) {
		switch errno {
		case syscall.ETIMEDOUT, syscall.ENETUNREACH, syscall.EHOSTUNREACH, syscall.ECONNREFUSED:
			return true
		}
	}

	return false
}

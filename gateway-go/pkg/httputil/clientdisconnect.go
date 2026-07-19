package httputil

import (
	"errors"
	"log/slog"
	"net"
	"strings"
	"syscall"
)

// IsClientDisconnect reports whether err from writing an HTTP response is the
// client hanging up mid-write (broken pipe / connection reset / a canceled
// request context) rather than a genuine server-side failure. Response
// encoders should log these at Debug, not Error: the payloads are well-typed
// structs that always marshal, so a write error here means the peer left — not
// a bug worth an operator's attention (and it inflates the error-rate metric).
func IsClientDisconnect(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, net.ErrClosed) {
		return true
	}
	// syscall.Errno wrapping doesn't always survive net's error chain, so fall
	// back to the canonical net-write substrings as a belt.
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "connection reset by peer") ||
		strings.Contains(msg, "use of closed network connection")
}

// LogEncodeError logs an HTTP-response JSON encode failure at the right level:
// Debug when the client hung up mid-write (IsClientDisconnect), Error for a
// genuine serialization failure the operator should see. Nil logger is a no-op.
func LogEncodeError(logger *slog.Logger, msg string, err error) {
	if logger == nil || err == nil {
		return
	}
	if IsClientDisconnect(err) {
		logger.Debug(msg+" (client disconnected)", "error", err)
		return
	}
	logger.Error(msg, "error", err)
}

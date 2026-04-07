package telegram

import (
	"errors"
	"fmt"
	"net"
	"syscall"
	"testing"
)

func TestIsPreConnectError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"generic error", errors.New("something"), false},
		{"ECONNREFUSED", syscall.ECONNREFUSED, true},
		{"ENETUNREACH", syscall.ENETUNREACH, true},
		{"EHOSTUNREACH", syscall.EHOSTUNREACH, true},
		{"ECONNRESET (not pre-connect)", syscall.ECONNRESET, false},
		{"ETIMEDOUT (not pre-connect)", syscall.ETIMEDOUT, false},
		{"DNS error", &net.DNSError{Err: "no such host", Name: "example.com"}, true},
		{"wrapped DNS error", fmt.Errorf("dial: %w", &net.DNSError{Err: "no such host", Name: "example.com"}), true},
		{"dial OpError with ECONNREFUSED", &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}, true},
		{"dial OpError generic", &net.OpError{Op: "dial", Err: errors.New("no route")}, true},
		{"read OpError (not pre-connect)", &net.OpError{Op: "read", Err: syscall.ECONNRESET}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsPreConnectError(tt.err); got != tt.want {
				t.Errorf("IsPreConnectError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestIsNetworkError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"generic error", errors.New("something"), false},
		{"ECONNRESET", syscall.ECONNRESET, true},
		{"ECONNREFUSED", syscall.ECONNREFUSED, true},
		{"EPIPE", syscall.EPIPE, true},
		{"ETIMEDOUT", syscall.ETIMEDOUT, true},
		{"DNS error (via pre-connect)", &net.DNSError{Err: "no such host", Name: "example.com"}, true},
		{"connection reset message", errors.New("connection reset by peer"), true},
		{"broken pipe message", errors.New("write: broken pipe"), true},
		{"i/o timeout message", errors.New("i/o timeout"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsNetworkError(tt.err); got != tt.want {
				t.Errorf("IsNetworkError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestIsFallbackTrigger(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"generic error", errors.New("something"), false},
		{"ETIMEDOUT", syscall.ETIMEDOUT, true},
		{"ENETUNREACH", syscall.ENETUNREACH, true},
		{"EHOSTUNREACH", syscall.EHOSTUNREACH, true},
		{"ECONNREFUSED", syscall.ECONNREFUSED, true},
		{"ECONNRESET (not fallback)", syscall.ECONNRESET, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsFallbackTrigger(tt.err); got != tt.want {
				t.Errorf("IsFallbackTrigger(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

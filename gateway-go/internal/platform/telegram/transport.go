package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"time"
)

const (
	telegramAPIHost  = "api.telegram.org"
	fallbackIPv4     = "149.154.167.220"
	keepAliveTimeout = 30 * time.Second
	dialTimeout      = 10 * time.Second
	maxIdleConns     = 10
)

// dialStrategy defines one approach for connecting to Telegram's API.
type dialStrategy struct {
	name    string
	network string // "tcp" (dual-stack) or "tcp4" (IPv4 only)
	// resolveHost overrides DNS resolution for the given host. If nil, default resolution is used.
	resolveHost func(host string) string
}

// stickyDialer tries multiple connection strategies and sticks with the one that works.
// Ported from the sticky fallback pattern in extensions/telegram/src/fetch.ts.
//
// Strategies (in order):
//  1. Dual-stack (tcp) — default, let the OS choose IPv4 or IPv6
//  2. IPv4-only (tcp4) — forces IPv4, bypasses broken IPv6
//  3. Pinned IP — hardcoded Telegram API IP, bypasses DNS entirely
type stickyDialer struct {
	stickyIndex atomic.Int32
	strategies  []dialStrategy
	logger      *slog.Logger
}

func newStickyDialer(logger *slog.Logger) *stickyDialer {
	if logger == nil {
		logger = slog.Default()
	}
	return &stickyDialer{
		logger: logger,
		strategies: []dialStrategy{
			{
				name:    "dual-stack",
				network: "tcp",
			},
			{
				name:    "ipv4-only",
				network: "tcp4",
			},
			{
				name:    "pinned-ip",
				network: "tcp4",
				resolveHost: func(host string) string {
					if host == telegramAPIHost {
						return fallbackIPv4
					}
					return host
				},
			},
		},
	}
}

// DialContext implements the net.Dialer-like interface for http.Transport.
// It tries the sticky strategy first. On fallback-triggering errors, it advances
// to the next strategy and retries within the same call.
func (d *stickyDialer) DialContext(ctx context.Context, _, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("telegram dialer: split host/port %q: %w", addr, err)
	}

	startIdx := int(d.stickyIndex.Load())
	var lastErr error

	for i := startIdx; i < len(d.strategies); i++ {
		strategy := d.strategies[i]

		dialHost := host
		if strategy.resolveHost != nil {
			dialHost = strategy.resolveHost(host)
		}

		dialer := &net.Dialer{Timeout: dialTimeout}
		conn, err := dialer.DialContext(ctx, strategy.network, net.JoinHostPort(dialHost, port))
		if err == nil {
			// Stick with this strategy for future calls.
			if i != startIdx {
				d.stickyIndex.Store(int32(i)) //nolint:gosec // G115 — i is a small strategy index (0-2)
				d.logger.Info("telegram transport fallback activated",
					"strategy", strategy.name,
					"from", d.strategies[startIdx].name,
				)
			}
			return conn, nil
		}

		lastErr = err

		// Only advance to next strategy for fallback-triggering errors.
		if !IsFallbackTrigger(err) {
			return nil, err
		}

		d.logger.Warn("telegram transport strategy failed, trying next",
			"strategy", strategy.name,
			"error", err,
		)
	}

	return nil, fmt.Errorf("telegram dialer: all strategies exhausted: %w", lastErr)
}

// ResetSticky resets the sticky index back to the default (dual-stack) strategy.
// Useful when network conditions may have changed.
func (d *stickyDialer) ResetSticky() {
	d.stickyIndex.Store(0)
}

// NewTelegramTransport creates an http.Transport optimized for Telegram API calls.
// It includes IPv4 fallback with sticky strategy selection and keepalive configuration.
//
// Ported from extensions/telegram/src/fetch.ts transport configuration.
func NewTelegramTransport(logger *slog.Logger) *http.Transport {
	dialer := newStickyDialer(logger)
	return &http.Transport{
		DialContext:         dialer.DialContext,
		MaxIdleConns:        maxIdleConns,
		MaxIdleConnsPerHost: maxIdleConns,
		IdleConnTimeout:     keepAliveTimeout,
		// Force HTTP/1.1 — Telegram's API does not benefit from h2 multiplexing
		// and some proxies have issues with it.
		ForceAttemptHTTP2: false,
	}
}

// NewTelegramHTTPClient creates an http.Client with Telegram-optimized transport.
// If proxyURL is non-empty, it configures proxy support on top of the fallback transport.
func NewTelegramHTTPClient(timeout time.Duration, logger *slog.Logger) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: NewTelegramTransport(logger),
	}
}

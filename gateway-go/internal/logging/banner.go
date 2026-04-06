// Package logging — startup banner and shutdown line for the Deneb gateway.
//
// The banner is written directly to an io.Writer (typically os.Stderr),
// bypassing slog. It provides a clean, minimal identity block at startup
// and a matching shutdown line with uptime.
package logging

import (
	"fmt"
	"io"
	"time"
)

// BannerInfo holds the values displayed in the startup banner.
type BannerInfo struct {
	Version       string
	Addr          string
	LocalAIStatus string // "online", "offline", or empty to hide
	PID           int    // non-zero in daemon mode
}

// PrintBanner writes a compact startup block to w.
//
// Example output (color omitted):
//
//	deneb gateway
//	0.1.0-go · rust-ffi
//
//	addr      127.0.0.1:18789
//	vega      enabled
//	localai    online
//
//	ready.
func PrintBanner(w io.Writer, info BannerInfo, color bool) {
	dim := pick(color, ansiDim, "")
	bold := pick(color, ansiBold, "")
	cyan := pick(color, ansiBoldCyn, "")
	green := pick(color, ansiBoldGrn, "")
	reset := pick(color, ansiReset, "")

	// Title line.
	fmt.Fprintf(w, "\n  %s%s✦%s %sdeneb gateway%s\n", cyan, bold, reset, bold, reset)

	// Version line.
	fmt.Fprintf(w, "  %s%s%s\n", dim, info.Version, reset)

	// Blank line before key-value block.
	fmt.Fprintln(w)

	// Key-value block with fixed-width keys.
	kv := func(key, val string) {
		fmt.Fprintf(w, "  %s%-10s%s%s\n", dim, key, reset, val)
	}

	kv("addr", info.Addr)

	if info.LocalAIStatus != "" {
		kv("localai", info.LocalAIStatus)
	}

	if info.PID > 0 {
		kv("pid", fmt.Sprintf("%d", info.PID))
	}

	// Ready indicator.
	fmt.Fprintf(w, "\n  %sready.%s\n\n", green, reset)
}

// PrintShutdown writes a clean shutdown line to w, matching the banner style.
func PrintShutdown(w io.Writer, uptime time.Duration, color bool) {
	dim := pick(color, ansiDim, "")
	bold := pick(color, ansiBold, "")
	reset := pick(color, ansiReset, "")

	fmt.Fprintf(w, "\n  %sdeneb gateway stopped%s  %s(%s)%s\n\n",
		bold, reset, dim, formatUptime(uptime), reset)
}

// formatUptime returns a human-readable duration string like "2h 14m" or "30s".
func formatUptime(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		m := int(d.Minutes())
		s := int(d.Seconds()) % 60
		if s == 0 {
			return fmt.Sprintf("%dm", m)
		}
		return fmt.Sprintf("%dm %ds", m, s)
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if m == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh %dm", h, m)
}

// pick returns a if color is true, b otherwise.
func pick(color bool, a, b string) string {
	if color {
		return a
	}
	return b
}

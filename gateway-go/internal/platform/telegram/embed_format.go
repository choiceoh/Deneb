package telegram

import (
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/core/coresecurity"
)

// --- Embed color/status indicator constants ---

// Status indicators for visual feedback in Telegram messages.
// These replace Discord-style embed colors with emoji-based indicators
// optimized for Telegram's HTML parse mode.
const (
	ColorSuccess = "\u2705"       // ✅
	ColorError   = "\u274C"       // ❌
	ColorWarning = "\u26A0\uFE0F" // ⚠️
	ColorInfo    = "\u2139\uFE0F" // ℹ️
	ColorPending = "\u23F3"       // ⏳
)

// --- Data types for embed builders ---

// TestResult holds test execution results for the test result embed.
func FormatError(title, detail, suggestion string) string {
	var b strings.Builder
	b.Grow(256)

	b.WriteString(ColorError)
	b.WriteString(" <b>")
	b.WriteString(coresecurity.SanitizeHTML(title))
	b.WriteString("</b>\n")
	b.WriteString("\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\n")

	if detail != "" {
		b.WriteByte('\n')
		// Truncate long error details for mobile readability.
		truncated := detail
		if len(truncated) > 500 {
			truncated = truncated[:497] + "..."
		}
		b.WriteString("<pre>")
		b.WriteString(coresecurity.SanitizeHTML(truncated))
		b.WriteString("</pre>\n")
	}

	// Try auto-translating the detail to Korean.
	if translation := TranslateErrorKorean(detail); translation != "" {
		b.WriteByte('\n')
		b.WriteString("\U0001F4AC <b>\uC6D0\uC778:</b> ")
		b.WriteString(coresecurity.SanitizeHTML(translation))
		b.WriteByte('\n')
	}

	if suggestion != "" {
		b.WriteByte('\n')
		b.WriteString("\U0001F4A1 <b>\uD574\uACB0 \uBC29\uBC95:</b> ")
		b.WriteString(coresecurity.SanitizeHTML(suggestion))
		b.WriteByte('\n')
	}

	return b.String()
}

// FormatHelp formats the /help command output for vibe coders.
// Lists available commands with Korean descriptions optimized for mobile.
func FormatHelp() string {
	var b strings.Builder
	b.Grow(512)

	b.WriteString("\U0001F916 <b>Deneb \uB3C4\uC6C0\uB9D0</b>\n")
	b.WriteString("\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\n\n")

	b.WriteString("<b>\uBA85\uB839\uC5B4</b>\n\n")

	// /btw \u2014 side question, never touches the main conversation context
	b.WriteString("\U0001F4AC <code>/btw &lt;\uC9C8\uBB38&gt;</code>\n")
	b.WriteString("  \uBA54\uC778 \uB300\uD654\uC5D0 \uC601\uD5A5 \uC5C6\uC774 \uC606\uC5D0\uC11C \uBE60\uB974\uAC8C \uB2F5\n")
	b.WriteString("  \uC608: <code>/btw \uC9C0\uAE08 \uD658\uC728 \uC5BC\uB9C8\uC57C?</code>\n\n")

	b.WriteString("\U0001F916 <code>/models</code>\n")
	b.WriteString("  \uC0AC\uC6A9\uD560 \uBAA8\uB378 \uC120\uD0DD\n\n")

	b.WriteString("\u2753 <code>/help</code>\n")
	b.WriteString("  \uC774 \uB3C4\uC6C0\uB9D0 \uD45C\uC2DC\n\n")

	b.WriteString("\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\n\n")

	b.WriteString("<b>\uC0AC\uC6A9 \uBC29\uBC95</b>\n\n")
	b.WriteString("\uD558\uACE0 \uC2F6\uC740 \uAC83\uC744 \uD55C\uAD6D\uC5B4\uB85C \uC790\uC5F0\uC2A4\uB7FD\uAC8C \uB9D0\uC500\uD574 \uC8FC\uC138\uC694.\n\n")

	b.WriteString("<i>\uC608\uC2DC:</i>\n")
	b.WriteString("  \u201C\uC624\uB298 \uBC1B\uC740 \uBA54\uC77C \uC815\uB9AC\uD574 \uC918\u201D\n")
	b.WriteString("  \u201CABC\uC0C1\uC0AC \uAC70\uB798 \uC9C4\uD589 \uC0C1\uD669 \uC54C\uB824 \uC918\u201D\n")
	b.WriteString("  \u201C\uB2E4\uC74C\uC8FC \uC77C\uC815 \uBCF4\uC5EC \uC918\u201D\n")

	return b.String()
}

// FormatCommit formats commit confirmation as a Telegram HTML message.
// Shows commit hash, message summary, file count, and branch.

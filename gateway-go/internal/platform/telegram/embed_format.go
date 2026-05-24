package telegram

import (
	"fmt"
	"strings"
	"time"

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
type TestResult struct {
	Passed  int
	Failed  int
	Skipped int
	Output  string
	Runtime time.Duration
}

// BuildResult holds build execution results for the build result embed.
type BuildResult struct {
	Success bool
	Errors  []string
	Runtime time.Duration
}

// CommitInfo holds commit details for the commit confirmation embed.
type CommitInfo struct {
	Hash    string
	Message string
	Files   int
	Branch  string
}

// PushInfo holds push details for the push confirmation embed.
type PushInfo struct {
	Branch string
	Remote string
	Ahead  int
}

// --- Format functions ---

// FormatTestResult formats test execution results as a Telegram HTML message.
// Displays pass/fail/skip counts, runtime, and a truncated output excerpt.
func FormatTestResult(r TestResult) string {
	var b strings.Builder
	b.Grow(512)

	total := r.Passed + r.Failed + r.Skipped

	// Header with overall status icon.
	if r.Failed > 0 {
		b.WriteString(ColorError)
		b.WriteString(" <b>\uD14C\uC2A4\uD2B8 \uACB0\uACFC: \uC2E4\uD328</b>\n")
	} else {
		b.WriteString(ColorSuccess)
		b.WriteString(" <b>\uD14C\uC2A4\uD2B8 \uACB0\uACFC: \uC131\uACF5</b>\n")
	}
	b.WriteString("\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\n\n")

	// Counts row.
	fmt.Fprintf(&b, "\U0001F4CA <b>\uCD1D %d\uAC1C \uD14C\uC2A4\uD2B8</b>\n", total)
	fmt.Fprintf(&b, "  %s \uD1B5\uACFC: %d\uAC1C\n", ColorSuccess, r.Passed)
	if r.Failed > 0 {
		fmt.Fprintf(&b, "  %s \uC2E4\uD328: %d\uAC1C\n", ColorError, r.Failed)
	}
	if r.Skipped > 0 {
		fmt.Fprintf(&b, "  %s \uAC74\uB108\uB6F0: %d\uAC1C\n", ColorWarning, r.Skipped)
	}

	// Runtime.
	b.WriteByte('\n')
	b.WriteString("\u23F1\uFE0F <b>\uC2E4\uD589 \uC2DC\uAC04:</b> ")
	b.WriteString(formatDuration(r.Runtime))
	b.WriteByte('\n')

	// Output excerpt (truncated for mobile readability and 4096 char limit).
	if r.Output != "" {
		b.WriteByte('\n')
		output := r.Output
		// Reserve space for header content (~300 chars) and leave room for HTML tags.
		const maxOutputLen = 2500
		if len(output) > maxOutputLen {
			output = output[len(output)-maxOutputLen:]
			output = "...\n" + output
		}
		b.WriteString("<b>\uCD9C\uB825:</b>\n")
		b.WriteString("<pre>")
		b.WriteString(coresecurity.SanitizeHTML(output))
		b.WriteString("</pre>")
	}

	return b.String()
}

// FormatBuildResult formats build execution results as a Telegram HTML message.
// Shows success/failure status, error details, and runtime.
func FormatBuildResult(r BuildResult) string {
	var b strings.Builder
	b.Grow(512)

	if r.Success {
		b.WriteString(ColorSuccess)
		b.WriteString(" <b>\uBE4C\uB4DC \uC131\uACF5</b>\n")
		b.WriteString("\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\n\n")
		b.WriteString("\u23F1\uFE0F <b>\uC2E4\uD589 \uC2DC\uAC04:</b> ")
		b.WriteString(formatDuration(r.Runtime))
		b.WriteByte('\n')
		b.WriteByte('\n')
		b.WriteString("\uBAA8\uB4E0 \uCEF4\uD30C\uC77C\uC774 \uC815\uC0C1\uC801\uC73C\uB85C \uC644\uB8CC\uB418\uC5C8\uC2B5\uB2C8\uB2E4.")
	} else {
		b.WriteString(ColorError)
		b.WriteString(" <b>\uBE4C\uB4DC \uC2E4\uD328</b>\n")
		b.WriteString("\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\n\n")

		b.WriteString("\u23F1\uFE0F <b>\uC2E4\uD589 \uC2DC\uAC04:</b> ")
		b.WriteString(formatDuration(r.Runtime))
		b.WriteByte('\n')

		if len(r.Errors) > 0 {
			b.WriteByte('\n')
			b.WriteString(ColorError)
			fmt.Fprintf(&b, " <b>\uC624\uB958 %d\uAC1C:</b>\n", len(r.Errors))

			// Show up to 5 errors to stay within message limits.
			maxErrors := 5
			if len(r.Errors) < maxErrors {
				maxErrors = len(r.Errors)
			}
			for i := range maxErrors {
				errText := r.Errors[i]
				// Truncate individual error lines for mobile display.
				if len(errText) > 200 {
					errText = errText[:197] + "..."
				}
				b.WriteString("\n<pre>")
				b.WriteString(coresecurity.SanitizeHTML(errText))
				b.WriteString("</pre>")
			}
			if len(r.Errors) > maxErrors {
				b.WriteByte('\n')
				fmt.Fprintf(&b, "\n<i>... \uC678 %d\uAC1C \uC624\uB958 \uC0DD\uB7B5</i>", len(r.Errors)-maxErrors)
			}

			// Translate first error to Korean for vibe coder.
			if translation := TranslateErrorKorean(r.Errors[0]); translation != "" {
				b.WriteByte('\n')
				b.WriteByte('\n')
				b.WriteString("\U0001F4AC <b>\uC694\uC57D:</b> ")
				b.WriteString(coresecurity.SanitizeHTML(translation))
			}
		}
	}

	return b.String()
}

// FormatError formats an error with a Korean explanation for vibe coders.
// title: short Korean error category, detail: raw error text,
// suggestion: actionable Korean guidance.
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
func FormatCommit(c CommitInfo) string {
	var b strings.Builder
	b.Grow(256)

	b.WriteString(ColorSuccess)
	b.WriteString(" <b>\uCEE4\uBC0B \uC644\uB8CC</b>\n")
	b.WriteString("\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\n\n")

	// Commit hash (short form).
	hash := c.Hash
	if len(hash) > 7 {
		hash = hash[:7]
	}
	b.WriteString("\U0001F3F7\uFE0F <b>\uD574\uC2DC:</b> <code>")
	b.WriteString(coresecurity.SanitizeHTML(hash))
	b.WriteString("</code>\n")

	// Branch.
	b.WriteString("\U0001F33F <b>\uBE0C\uB79C\uCE58:</b> <code>")
	b.WriteString(coresecurity.SanitizeHTML(c.Branch))
	b.WriteString("</code>\n")

	// File count.
	fmt.Fprintf(&b, "\U0001F4C1 <b>\uD30C\uC77C:</b> %d\uAC1C \uBCC0\uACBD\n", c.Files)

	// Commit message (truncated for mobile).
	b.WriteByte('\n')
	msg := c.Message
	if len(msg) > 200 {
		msg = msg[:197] + "..."
	}
	b.WriteString("\U0001F4DD <b>\uBA54\uC2DC\uC9C0:</b>\n")
	b.WriteString(coresecurity.SanitizeHTML(msg))
	b.WriteByte('\n')

	return b.String()
}

// FormatPush formats push confirmation as a Telegram HTML message.
// Shows branch, remote name, and number of commits pushed.
func FormatPush(p PushInfo) string {
	var b strings.Builder
	b.Grow(256)

	b.WriteString(ColorSuccess)
	b.WriteString(" <b>\uD478\uC2DC \uC644\uB8CC</b>\n")
	b.WriteString("\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\n\n")

	// Branch.
	b.WriteString("\U0001F33F <b>\uBE0C\uB79C\uCE58:</b> <code>")
	b.WriteString(coresecurity.SanitizeHTML(p.Branch))
	b.WriteString("</code>\n")

	// Remote.
	b.WriteString("\U0001F310 <b>\uC6D0\uACA9:</b> <code>")
	remote := p.Remote
	if remote == "" {
		remote = "origin"
	}
	b.WriteString(coresecurity.SanitizeHTML(remote))
	b.WriteString("</code>\n")

	// Commits ahead.
	if p.Ahead > 0 {
		fmt.Fprintf(&b, "\U0001F4E4 <b>\uC804\uC1A1:</b> %d\uAC1C \uCEE4\uBC0B\n", p.Ahead)
	}

	b.WriteByte('\n')
	b.WriteString("\uC6D0\uACA9 \uC800\uC7A5\uC18C\uC5D0 \uC131\uACF5\uC801\uC73C\uB85C \uC804\uC1A1\uB418\uC5C8\uC2B5\uB2C8\uB2E4.")

	return b.String()
}

// --- Helpers ---

// formatDuration renders a time.Duration as a human-readable Korean string.
// Optimized for short durations typical of build/test runs.
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1f\uCD08", d.Seconds())
	}
	mins := int(d.Minutes())
	secs := int(d.Seconds()) % 60
	if secs == 0 {
		return fmt.Sprintf("%d\uBD84", mins)
	}
	return fmt.Sprintf("%d\uBD84 %d\uCD08", mins, secs)
}

package denebui

import (
	"log/slog"
	"strings"
)

// HTMLFenceInfo is the fence info-string of a webpage-style HTML answer: a
// self-contained HTML document the clients render sandboxed INLINE in the
// chat transcript (Andromeda iframe / native WebView — never a separate
// screen). Authoring contract: inline CSS/JS only, no external resources
// (clients block all network), no backtick characters anywhere in the
// document (a raw ``` run would close the fence — JS must not use template
// literals), and page → chat replies go through window.deneb.send(text),
// which the clients bridge to a user message.
const HTMLFenceInfo = "deneb-html"

// MaxHTMLAnswerBytes caps a delivered deneb-html document. Oversized bodies
// degrade to a plain ```html code block rather than being truncated — a cut
// document renders broken markup; a code block stays readable.
const MaxHTMLAnswerBytes = 96 * 1024

// normalizeHTMLAnswers enforces the deneb-html delivery contract on a final
// reply: the first well-formed fence is kept (re-fenced canonically, closed
// if the model forgot the closer), later fences and contract violations
// degrade to ```html code blocks so raw markup stays readable but never
// executes. Old clients that predate the fence render it as a code block
// anyway, so the degrade path and the compatibility path look identical.
func normalizeHTMLAnswers(text, sessionKey string, logger *slog.Logger) (string, []Rejection) {
	if !hasHTMLAnswerFence(text) {
		return text, nil
	}
	var rejections []Rejection
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines)+1)
	kept := false
	for i := 0; i < len(lines); i++ {
		if !isHTMLAnswerOpen(lines[i]) {
			out = append(out, lines[i])
			continue
		}
		var body []string
		for i+1 < len(lines) {
			i++
			if isFenceClose(strings.TrimSpace(lines[i])) {
				break
			}
			body = append(body, lines[i])
		}
		doc := strings.TrimSpace(strings.Join(body, "\n"))
		reason := ""
		switch {
		case kept:
			reason = "additional_block"
		case len(doc) > MaxHTMLAnswerBytes:
			reason = "oversize"
		case !strings.HasPrefix(doc, "<"):
			reason = "not_markup"
		}
		if reason == "" {
			kept = true
			logger.Info("deneb-html answer authored", "session", sessionKey, "bytes", len(doc))
			out = append(out, "```"+HTMLFenceInfo, doc, "```")
			continue
		}
		logger.Warn("deneb-html answer degraded to code block",
			"session", sessionKey, "reason", reason, "bytes", len(doc))
		// Same channel the deneb-ui rejections use: a degraded page reaches the
		// user as a raw code block, and until now its author was told nothing
		// (the card path has carried a correction hint since #4753).
		rejections = append(rejections, Rejection{Reason: "html_" + reason})
		if doc != "" {
			out = append(out, "```html", doc, "```")
		}
	}
	return strings.Join(out, "\n"), rejections
}

// StripHTMLAnswers removes ```deneb-html documents from text, leaving a short
// "[웹 응답]" marker line, so prose-level preview/title extraction (work-feed
// cards, push previews, card titlers) never reads raw markup. An unclosed
// fence strips to EOF — mid-stream text must not leak partial markup either.
func StripHTMLAnswers(text string) string {
	if !hasHTMLAnswerFence(text) {
		return text
	}
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for i := 0; i < len(lines); i++ {
		if !isHTMLAnswerOpen(lines[i]) {
			out = append(out, lines[i])
			continue
		}
		for i+1 < len(lines) {
			i++
			if isFenceClose(strings.TrimSpace(lines[i])) {
				break
			}
		}
		out = append(out, "[웹 응답]")
	}
	return strings.Join(out, "\n")
}

func hasHTMLAnswerFence(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		if isHTMLAnswerOpen(line) {
			return true
		}
	}
	return false
}

// isHTMLAnswerOpen matches a strict own-line ```deneb-html opener. Unlike the
// deneb-ui opener there is deliberately no glued-prose or glued-body leniency:
// an HTML document starts on its own line, and looser matching would risk
// swallowing prose that merely mentions the fence.
func isHTMLAnswerOpen(line string) bool {
	t := strings.TrimSpace(line)
	if !strings.HasPrefix(t, "```") {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(strings.TrimLeft(t, "`")), HTMLFenceInfo)
}

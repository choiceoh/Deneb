// inbound_clarify.go — handle "clarify:" inline-button callbacks.
//
// When the agent uses the clarify tool, it sends a message whose inline
// keyboard has callback_data of the form "clarify:<idx>". This file
// intercepts those clicks, recovers the chosen option's text from the
// rendered message, and hands the agent a human-readable summary instead
// of the opaque "[Button: clarify:0]" fallback.
package server

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
)

// formatClarifyCallback returns a Korean user-message string to forward to
// the agent when cb was produced by a clarify-tool button, or "" if cb is
// not a clarify callback (so the caller can fall through to its generic
// dispatch).
func formatClarifyCallback(cb *telegram.CallbackQuery) string {
	if cb == nil || cb.Message == nil {
		return ""
	}
	if !strings.HasPrefix(cb.Data, tools.ClarifyCallbackPrefix) {
		return ""
	}
	idxStr := strings.TrimPrefix(cb.Data, tools.ClarifyCallbackPrefix)
	idx, err := strconv.Atoi(idxStr)
	if err != nil || idx < 0 {
		// Malformed clarify payload — fall back to generic dispatch.
		return ""
	}

	// Try to recover the option text from the rendered message body. The
	// clarify tool renders options as "N. <text>" lines (see
	// tools/clarify.go:buildClarifyMessage). If extraction fails (e.g. the
	// message was edited), we still send a usable message with just the
	// index so the agent can look it up in its own transcript.
	optionText := extractClarifyOptionText(cb.Message.Text, idx)
	if optionText == "" {
		return fmt.Sprintf("[유저 응답 (버튼): 선택지 %d번]", idx+1)
	}
	return fmt.Sprintf("[유저 응답 (버튼): 선택지 %d번 — %q]", idx+1, optionText)
}

// extractClarifyOptionText returns the text following "N. " on the line
// whose list number equals idx+1, or "" if no such line is present.
// Matches only plain numbered prefixes "N. " at the start of a (possibly
// indented) line so we don't accidentally grab content from the question
// or the button directive.
func extractClarifyOptionText(body string, idx int) string {
	wantPrefix := fmt.Sprintf("%d. ", idx+1)
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimLeft(raw, " \t")
		if strings.HasPrefix(line, wantPrefix) {
			return strings.TrimSpace(line[len(wantPrefix):])
		}
	}
	return ""
}

// stripClarifyKeyboard edits the bot's original clarify message to remove
// the inline keyboard and append a "(선택됨: N번)" marker. This prevents
// accidental double-taps and leaves a visible audit trail of what the user
// chose. Runs in its own goroutine; failures are logged at Debug because
// the user's choice still reaches the agent via the separate chat.send.
func (p *InboundProcessor) stripClarifyKeyboard(client *telegram.Client, cb *telegram.CallbackQuery) {
	defer func() {
		if r := recover(); r != nil {
			p.logger.Error("panic in stripClarifyKeyboard", "panic", r)
		}
	}()

	if cb == nil || cb.Message == nil || client == nil {
		return
	}
	idxStr := strings.TrimPrefix(cb.Data, tools.ClarifyCallbackPrefix)
	idx, err := strconv.Atoi(idxStr)
	if err != nil || idx < 0 {
		return
	}

	// Build the replacement text: original body with the directive stripped
	// and a "(선택됨: N번)" suffix appended. EditMessageText sends reply_markup
	// only if non-nil, so passing nil removes the keyboard.
	newText := buildClarifyResolvedText(cb.Message.Text, idx)
	if newText == "" {
		return // nothing sensible to write; leave the message alone
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := telegram.EditMessageText(ctx, client, cb.Message.Chat.ID, cb.Message.MessageID, newText, "", nil); err != nil {
		if telegram.IsMessageNotModifiedError(err) {
			return // already in the desired shape; not a real failure
		}
		// Logged at Debug: the agent still received the choice, so the user
		// is not left hanging. An operator digging into "why does my button
		// still show after a click?" will find this line.
		p.logger.Debug("failed to edit clarify message to remove keyboard",
			"chatId", cb.Message.Chat.ID, "msgId", cb.Message.MessageID, "error", err)
	}
}

// buildClarifyResolvedText returns the message body to show after a click:
// strips the "<!-- buttons: ... -->" directive (harmless if already
// stripped) and appends a Korean "선택됨" marker naming the chosen option.
func buildClarifyResolvedText(body string, idx int) string {
	if body == "" {
		return ""
	}
	// Strip the button directive if it's still visible in the rendered text
	// (it normally isn't — parseReplyButtons removes it before send — but a
	// plain-text fallback or a different send path might leak it through).
	if i := strings.LastIndex(body, "<!-- buttons:"); i >= 0 {
		body = strings.TrimRight(body[:i], " \t\n\r")
	}
	choice := extractClarifyOptionText(body, idx)
	if choice == "" {
		return strings.TrimRight(body, " \t\n\r") + fmt.Sprintf("\n\n(선택됨: %d번)", idx+1)
	}
	return strings.TrimRight(body, " \t\n\r") + fmt.Sprintf("\n\n(선택됨: %d번 — %s)", idx+1, choice)
}

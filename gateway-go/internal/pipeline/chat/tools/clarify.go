package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// Clarify option length cap — visible button label limit. Telegram allows
// longer labels, but shorter options are easier to tap on mobile and keep
// the keyboard layout compact. 40 chars is a reasonable ceiling for Korean.
const clarifyMaxOptionLen = 40

// Min/max options enforced on the clarify tool.
const (
	clarifyMinOptions = 2
	clarifyMaxOptions = 5
)

// ClarifyCallbackPrefix is the callback_data prefix for clarify button clicks.
// Format: "clarify:<index>" (e.g. "clarify:0", "clarify:3").
// Kept short so we stay well under Telegram's 64-byte callback_data limit.
const ClarifyCallbackPrefix = "clarify:"

// buildClarifyMessage formats the question + numbered options + button directive.
// The directive is parsed by server/reply_buttons.go:parseReplyButtons and
// converted into an InlineKeyboardMarkup when delivered to Telegram.
//
// Each button's callback_data encodes the option index so handleCallbackQuery
// can forward the user's choice back to the agent on a later turn.
func buildClarifyMessage(question string, options []string) (string, error) {
	var body strings.Builder
	body.WriteString(strings.TrimSpace(question))
	body.WriteString("\n\n")
	for i, opt := range options {
		fmt.Fprintf(&body, "%d. %s\n", i+1, opt)
	}

	// JSON array of rows: one row per option so the buttons stack vertically
	// on mobile (easier to tap than a wrapped horizontal row).
	rows := make([][]string, len(options))
	for i, opt := range options {
		// Format "label|callback_data" — parseReplyButtons splits on the
		// first "|". The label (visible button text) carries the full option.
		spec := fmt.Sprintf("%s|%s%d", opt, ClarifyCallbackPrefix, i)
		rows[i] = []string{spec}
	}
	rowsJSON, err := json.Marshal(rows)
	if err != nil {
		return "", fmt.Errorf("encode button rows: %w", err)
	}
	fmt.Fprintf(&body, "\n<!-- buttons: %s -->", string(rowsJSON))
	return body.String(), nil
}

// buildClarifyText formats just the question + numbered options, without the
// Telegram button directive. Used as a fallback on channels that don't render
// inline keyboards (e.g. the native client) so the agent can still ask — the
// user replies in free text and the agent reads it on the next turn.
func buildClarifyText(question string, options []string) string {
	var body strings.Builder
	body.WriteString(strings.TrimSpace(question))
	body.WriteString("\n\n")
	for i, opt := range options {
		fmt.Fprintf(&body, "%d. %s\n", i+1, opt)
	}
	body.WriteString("\n번호나 내용으로 답해 주세요.")
	return body.String()
}

// ToolClarify implements the clarify tool: the agent asks the user to resolve
// an ambiguity and receives the answer through a Telegram inline-keyboard
// button click. The tool sends the question + buttons via the current
// replyFunc and returns immediately — the agent's turn ends there. When the
// user taps a button, the inbound callback dispatcher injects the choice as
// a new user message on the next turn (see runtime/server/inbound.go).
func ToolClarify() ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Question string   `json:"question"`
			Options  []string `json:"options"`
		}
		if err := jsonutil.UnmarshalInto("clarify params", input, &p); err != nil {
			return "", err
		}

		q := strings.TrimSpace(p.Question)
		if q == "" {
			return "", fmt.Errorf("clarify: question is required")
		}

		// Trim each option — trailing whitespace creates sloppy button labels.
		cleaned := make([]string, 0, len(p.Options))
		for _, opt := range p.Options {
			t := strings.TrimSpace(opt)
			if t == "" {
				continue
			}
			cleaned = append(cleaned, t)
		}
		if len(cleaned) < clarifyMinOptions {
			return "", fmt.Errorf("clarify: need at least %d options, got %d", clarifyMinOptions, len(cleaned))
		}
		if len(cleaned) > clarifyMaxOptions {
			return "", fmt.Errorf("clarify: at most %d options allowed, got %d", clarifyMaxOptions, len(cleaned))
		}

		// Enforce per-option length cap in runes so Korean/emoji counts match
		// the visual label length the user will see.
		for i, opt := range cleaned {
			if utf8.RuneCountInString(opt) > clarifyMaxOptionLen {
				return "", fmt.Errorf("clarify: option %d exceeds %d characters (got %d)",
					i+1, clarifyMaxOptionLen, utf8.RuneCountInString(opt))
			}
		}

		// Delivery routing: the clarify message must land on the same channel
		// the agent is currently replying on, otherwise the button callback
		// can't be routed back.
		replyFn := toolctx.ReplyFuncFromContext(ctx)
		if replyFn == nil {
			return "", fmt.Errorf("clarify: channel not connected; cannot send button prompt")
		}
		delivery := toolctx.DeliveryFromContext(ctx)
		if delivery == nil || delivery.Channel == "" || delivery.To == "" {
			return "", fmt.Errorf("clarify: no active delivery target; cannot send button prompt")
		}
		// Inline keyboards are a Telegram-specific affordance. On other channels
		// the button directive is stripped by parseReplyButtons (only the
		// Telegram reply path parses it), so fall back to a plain-text numbered
		// question — the user replies in free text and the agent reads it next turn.
		if delivery.Channel != "telegram" {
			sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			if err := replyFn(sendCtx, delivery, buildClarifyText(q, cleaned)); err != nil {
				return "", fmt.Errorf("clarify: failed to send prompt: %w", err)
			}
			return fmt.Sprintf(
				"질문 전송됨 (선택지 %d개, 텍스트 모드 — %s 채널은 버튼 미지원). 이 턴을 종료하라 — 사용자 답변은 다음 턴에서 받는다.",
				len(cleaned), delivery.Channel,
			), nil
		}

		msg, err := buildClarifyMessage(q, cleaned)
		if err != nil {
			return "", err
		}

		sendCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if err := replyFn(sendCtx, delivery, msg); err != nil {
			return "", fmt.Errorf("clarify: failed to send prompt: %w", err)
		}

		// Return a status string for the agent. Mention that the turn should
		// end so the agent doesn't loop waiting for a synchronous answer.
		return fmt.Sprintf(
			"문의 전송됨 (선택지 %d개). 이 턴을 종료하라 — 사용자가 버튼을 누르면 다음 턴에서 선택 결과를 받게 된다.",
			len(cleaned),
		), nil
	}
}

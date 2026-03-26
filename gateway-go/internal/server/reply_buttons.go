// reply_buttons.go — Parse inline keyboard button directives from agent replies.
//
// Agents can include a <!-- buttons: [...] --> HTML comment at the end of their
// reply to attach Telegram inline keyboard buttons. The directive is stripped
// before the text is sent; the parsed buttons become an InlineKeyboardMarkup.
//
// Button format:
//
//	<!-- buttons: [["Yes|yes","No|no"],["Cancel|cancel"]] -->
//
// Each string is "label|callback_data". Each inner array is a keyboard row.
package server

import (
	"encoding/json"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/telegram"
)

const buttonDirectivePrefix = "<!-- buttons:"
const buttonDirectiveSuffix = "-->"

// parseReplyButtons extracts an optional <!-- buttons: [...] --> directive
// from the end of a reply text. Returns the cleaned text and keyboard (nil if
// no directive found or parsing fails).
func parseReplyButtons(text string) (string, *telegram.InlineKeyboardMarkup) {
	trimmed := strings.TrimRight(text, " \t\n\r")

	// Find the last occurrence of the directive.
	idx := strings.LastIndex(trimmed, buttonDirectivePrefix)
	if idx < 0 {
		return text, nil
	}

	rest := trimmed[idx+len(buttonDirectivePrefix):]
	endIdx := strings.Index(rest, buttonDirectiveSuffix)
	if endIdx < 0 {
		return text, nil
	}

	jsonStr := strings.TrimSpace(rest[:endIdx])
	cleanedText := strings.TrimRight(trimmed[:idx], " \t\n\r")

	// Parse the JSON array of rows.
	var rows [][]string
	if err := json.Unmarshal([]byte(jsonStr), &rows); err != nil {
		return text, nil
	}

	keyboard := buildKeyboardFromRows(rows)
	if keyboard == nil {
		return text, nil
	}

	return cleanedText, keyboard
}

// buildKeyboardFromRows converts string rows ("label|data") to InlineKeyboardMarkup.
func buildKeyboardFromRows(rows [][]string) *telegram.InlineKeyboardMarkup {
	if len(rows) == 0 {
		return nil
	}

	var kbRows [][]telegram.InlineKeyboardButton
	for _, row := range rows {
		if len(row) == 0 {
			continue
		}
		var buttons []telegram.InlineKeyboardButton
		for _, spec := range row {
			parts := strings.SplitN(spec, "|", 2)
			label := strings.TrimSpace(parts[0])
			data := label // default: callback_data = label
			if len(parts) == 2 {
				data = strings.TrimSpace(parts[1])
			}
			if label == "" {
				continue
			}
			// Telegram enforces 64-byte limit on callback_data.
			if len(data) > telegram.MaxCallbackData {
				data = data[:telegram.MaxCallbackData]
			}
			buttons = append(buttons, telegram.InlineKeyboardButton{
				Text:         label,
				CallbackData: data,
			})
		}
		if len(buttons) > 0 {
			kbRows = append(kbRows, buttons)
		}
	}

	if len(kbRows) == 0 {
		return nil
	}
	return &telegram.InlineKeyboardMarkup{InlineKeyboard: kbRows}
}

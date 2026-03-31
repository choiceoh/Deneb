package telegram

import "fmt"

// ReplyButton represents a Telegram inline keyboard button.
// Maps directly to Telegram Bot API's InlineKeyboardButton with
// callback_data for interaction handling.
type ReplyButton struct {
	Text string `json:"text"`
	Data string `json:"callback_data,omitempty"`
}

// Button action constants. These are parsed by HandleTelegramInteraction
// to dispatch the appropriate follow-up action.
const (
	ActionCommit  = "commit"
	ActionTest    = "test"
	ActionRevert  = "revert"
	ActionPush    = "push"
	ActionNext    = "new"
	ActionFix     = "fix"
	ActionDetails = "details"
	ActionRetry   = "retry"
)

// buttonDef is an internal helper for declaring button templates.
type buttonDef struct {
	text   string
	action string
}

// Button sets per outcome. Each outcome maps to a row of buttons
// that represent the most likely next actions for a vibe coder.
//
// Design rationale:
//   - Code change: user probably wants to commit, verify tests, or undo.
//   - Test pass: safe to commit+push or move on.
//   - Test fail: user needs the agent to fix it or show details.
//   - Build fail: same as test fail — fix or investigate.
//   - Commit: natural next step is push or start a new task.
//   - Error: retry or ask the agent to fix it.
//   - General: no buttons — conversational replies don't need actions.
var outcomeButtons = map[ReplyOutcome][]buttonDef{
	OutcomeCodeChange: {
		{text: "커밋", action: ActionCommit},
		{text: "테스트", action: ActionTest},
		{text: "되돌리기", action: ActionRevert},
	},
	OutcomeTestPass: {
		{text: "커밋+푸시", action: ActionPush},
		{text: "다음 작업", action: ActionNext},
	},
	OutcomeTestFail: {
		{text: "수정", action: ActionFix},
		{text: "자세히", action: ActionDetails},
	},
	OutcomeBuildFail: {
		{text: "수정", action: ActionFix},
		{text: "자세히", action: ActionDetails},
	},
	OutcomeCommit: {
		{text: "푸시", action: ActionPush},
		{text: "다음 작업", action: ActionNext},
	},
	OutcomeError: {
		{text: "재시도", action: ActionRetry},
		{text: "수정", action: ActionFix},
	},
	// OutcomeGeneral has no buttons — intentionally omitted from the map.
}

// ContextButtons returns appropriate action buttons based on the reply
// outcome. Each button's Data field encodes "action:sessionKey" so that
// HandleTelegramInteraction can route the callback.
//
// Returns nil for OutcomeGeneral (no buttons needed for conversational replies).
func ContextButtons(outcome ReplyOutcome, sessionKey string) [][]ReplyButton {
	defs, ok := outcomeButtons[outcome]
	if !ok || len(defs) == 0 {
		return nil
	}

	row := make([]ReplyButton, 0, len(defs))
	for _, d := range defs {
		row = append(row, ReplyButton{
			Text: d.text,
			Data: formatCallbackData(d.action, sessionKey),
		})
	}
	return [][]ReplyButton{row}
}

// formatCallbackData builds the "action:sessionKey" callback payload.
// Truncates to MaxCallbackData (64 bytes) to respect Telegram limits.
func formatCallbackData(action, sessionKey string) string {
	data := fmt.Sprintf("%s:%s", action, sessionKey)
	if len(data) > MaxCallbackData {
		data = data[:MaxCallbackData]
	}
	return data
}

// InlineKeyboard builds the Telegram API inline_keyboard structure
// from button rows. Returns nil if rows is empty, so callers can
// safely pass the result to the send API without nil checks.
func InlineKeyboard(rows [][]ReplyButton) [][]map[string]string {
	if len(rows) == 0 {
		return nil
	}
	keyboard := make([][]map[string]string, 0, len(rows))
	for _, row := range rows {
		kbRow := make([]map[string]string, 0, len(row))
		for _, btn := range row {
			entry := map[string]string{"text": btn.Text}
			if btn.Data != "" {
				entry["callback_data"] = btn.Data
			}
			kbRow = append(kbRow, entry)
		}
		keyboard = append(keyboard, kbRow)
	}
	return keyboard
}

// ParseCallbackData splits a callback_data string into action and
// session key. Returns empty strings if the format is invalid.
func ParseCallbackData(data string) (action, sessionKey string) {
	idx := -1
	for i := 0; i < len(data); i++ {
		if data[i] == ':' {
			idx = i
			break
		}
	}
	if idx < 0 {
		return "", ""
	}
	return data[:idx], data[idx+1:]
}

package discord

import "fmt"

// ActionRow wraps child components in a Discord action row.
func ActionRow(children ...Component) Component {
	return Component{
		Type:       ComponentActionRow,
		Components: children,
	}
}

// Button creates a Discord button component.
func Button(label, customID string, style int) Component {
	return Component{
		Type:     ComponentButton,
		Style:    style,
		Label:    label,
		CustomID: customID,
	}
}

// CodeActionButtons returns an action row with common code action buttons.
// Used after the agent shows a code change or diff.
func CodeActionButtons(sessionKey string) []Component {
	return []Component{
		ActionRow(
			Button("🧪 테스트 실행", fmt.Sprintf("test:%s", sessionKey), ButtonPrimary),
			Button("💾 커밋", fmt.Sprintf("commit:%s", sessionKey), ButtonSuccess),
			Button("📊 현황", fmt.Sprintf("dashboard:%s", sessionKey), ButtonSecondary),
			Button("↩️ 되돌리기", fmt.Sprintf("revert:%s", sessionKey), ButtonDanger),
		),
	}
}

// TestResultButtons returns an action row for test result follow-ups.
// Uses the smart test button set with auto-fix and full test options.
func TestResultButtons(sessionKey string) []Component {
	return SmartTestButtons(sessionKey, true)
}

// ConfirmButtons returns confirm/cancel buttons for destructive actions.
func ConfirmButtons(sessionKey, action string) []Component {
	return []Component{
		ActionRow(
			Button("✅ 확인", fmt.Sprintf("confirm:%s:%s", action, sessionKey), ButtonSuccess),
			Button("❌ 취소", fmt.Sprintf("cancel:%s:%s", action, sessionKey), ButtonDanger),
		),
	}
}

// AfterCommitButtons returns an action row with follow-up buttons after a successful commit.
// Shows push, PR creation, and new-task options for vibe coders.
func AfterCommitButtons(sessionKey string) []Component {
	return []Component{
		ActionRow(
			Button("🚀 푸시", fmt.Sprintf("push:%s", sessionKey), ButtonPrimary),
			Button("🔀 PR 생성", fmt.Sprintf("prcreate:%s", sessionKey), ButtonSuccess),
			Button("🆕 새 작업", fmt.Sprintf("new:%s", sessionKey), ButtonSecondary),
		),
	}
}

// MergeConflictButtons returns buttons for resolving a detected merge conflict.
// Shows options to auto-resolve, view conflict details, or abort the merge.
func MergeConflictButtons(sessionKey string) []Component {
	return []Component{
		ActionRow(
			Button("🔧 충돌 해결", fmt.Sprintf("mergefix:%s", sessionKey), ButtonPrimary),
			Button("📋 충돌 상세", fmt.Sprintf("mergedetail:%s", sessionKey), ButtonSecondary),
			Button("⛔ 병합 중단", fmt.Sprintf("mergeabort:%s", sessionKey), ButtonDanger),
		),
	}
}

// AfterPRCreateButtons returns follow-up buttons after PR creation.
func AfterPRCreateButtons(sessionKey string) []Component {
	return []Component{
		ActionRow(
			Button("📊 현황 보기", fmt.Sprintf("dashboard:%s", sessionKey), ButtonSecondary),
			Button("🆕 새 작업", fmt.Sprintf("new:%s", sessionKey), ButtonPrimary),
		),
	}
}

// MergeConflictCheckButtons returns a button to check for merge conflicts
// before merging a branch.
func MergeConflictCheckButtons(sessionKey string) []Component {
	return []Component{
		ActionRow(
			Button("🔍 충돌 검사", fmt.Sprintf("mergecheck:%s", sessionKey), ButtonPrimary),
		),
	}
}

// ParseButtonAction extracts the action and session key from a button custom_id.
// Format: "action:sessionKey" or "action:subaction:sessionKey".
// Returns action, sessionKey.
func ParseButtonAction(customID string) (action, sessionKey string) {
	// Find last colon to split action prefix from session key.
	// Session keys are "discord:<channelID>" so they contain a colon.
	// Button format: "test:discord:123456" → action="test", sessionKey="discord:123456"
	for i := 0; i < len(customID); i++ {
		if customID[i] == ':' {
			return customID[:i], customID[i+1:]
		}
	}
	return customID, ""
}

package toolport

import "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"

// FormatFetchActivationNotice preserves the toolport compatibility surface.
func FormatFetchActivationNotice(names []string) string {
	return chatport.FormatFetchActivationNotice(names)
}

// FormatSkillActivationNotice preserves the toolport compatibility surface.
func FormatSkillActivationNotice(names []string) string {
	return chatport.FormatSkillActivationNotice(names)
}

// FormatAlreadyActiveNotice preserves the toolport compatibility surface.
func FormatAlreadyActiveNotice(names []string) string {
	return chatport.FormatAlreadyActiveNotice(names)
}

// ParseActivationNotices delegates parsing to the neutral chat contract.
func ParseActivationNotices(content string) []string {
	return chatport.ParseActivationNotices(content)
}

// ExtractActivationNotices delegates canonical extraction to chatport.
func ExtractActivationNotices(content string) []string {
	return chatport.ExtractActivationNotices(content)
}

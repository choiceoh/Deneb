package tools

import "testing"

func TestIsSelfRecipient(t *testing.T) {
	self := []string{"self", "Self", " ME ", "me", "나", "나에게", "내게", "내 메일", "내메일", "본인"}
	for _, s := range self {
		if !isSelfRecipient(s) {
			t.Errorf("isSelfRecipient(%q) = false, want true", s)
		}
	}
	notSelf := []string{"", "boss@example.com", "김부장", "myself", "self@example.com", "팀"}
	for _, s := range notSelf {
		if isSelfRecipient(s) {
			t.Errorf("isSelfRecipient(%q) = true, want false", s)
		}
	}
}

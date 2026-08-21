package messageops

import "testing"

func TestNormalizeMessageAction(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{in: "", want: ""},
		{in: "SEND", want: "send"},
		{in: "보내기", want: "send"},
		{in: "전송", want: "send"},
		{in: "답장", want: "reply"},
		{in: "리액션", want: "react"},
		{in: "반응", want: "react"},
		{in: "thread_reply", want: "thread-reply"},
		{in: "  reply  ", want: "reply"},
	}
	for _, tt := range tests {
		if got := normalizeMessageAction(tt.in); got != tt.want {
			t.Errorf("normalizeMessageAction(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

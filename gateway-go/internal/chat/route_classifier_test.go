package chat

import "testing"

func TestClassifyRoute_DefaultConcurrent(t *testing.T) {
	cases := []string{
		"지금 뭐하고 있어?",
		"날씨 어때?",
		"이거 설명해줘",
		"How's it going?",
		"파일 구조가 어떻게 되어 있어?",
		"",
	}
	for _, msg := range cases {
		if got := classifyRoute(msg); got != RouteConcurrent {
			t.Errorf("classifyRoute(%q) = %d, want RouteConcurrent", msg, got)
		}
	}
}

func TestClassifyRoute_ExplicitInterrupt(t *testing.T) {
	cases := []string{
		"/kill",
		"/stop",
		"/reset",
		"/new",
		"중단",
		"그만",
		"멈춰",
		"취소",
		"stop",
		"cancel",
		"abort",
		"kill",
		"중지해",
		"스톱",
	}
	for _, msg := range cases {
		if got := classifyRoute(msg); got != RouteInterrupt {
			t.Errorf("classifyRoute(%q) = %d, want RouteInterrupt", msg, got)
		}
	}
}

func TestClassifyRoute_LongMessageWithKeyword(t *testing.T) {
	// Long messages containing interrupt keywords should NOT trigger interrupt.
	// Only short messages (≤20 chars) with keywords are treated as interrupts.
	long := "이 작업을 중단하고 다른 것을 해줄 수 있어? 새로운 기능을 추가하고 싶은데"
	if got := classifyRoute(long); got != RouteConcurrent {
		t.Errorf("classifyRoute(long msg with 중단) = %d, want RouteConcurrent", got)
	}
}

func TestClassifyRoute_SlashCommandsAlwaysInterrupt(t *testing.T) {
	// Slash commands should interrupt regardless of message length.
	cases := []string{
		"/kill please stop everything",
		"/reset and start fresh",
		"/new conversation",
	}
	for _, msg := range cases {
		if got := classifyRoute(msg); got != RouteInterrupt {
			t.Errorf("classifyRoute(%q) = %d, want RouteInterrupt", msg, got)
		}
	}
}

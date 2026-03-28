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
		"그만해줘",
		"작업 중단해",
	}
	for _, msg := range cases {
		if got := classifyRoute(msg); got != RouteInterrupt {
			t.Errorf("classifyRoute(%q) = %d, want RouteInterrupt", msg, got)
		}
	}
}

func TestClassifyRoute_LongMessageWithKeyword(t *testing.T) {
	// Long messages (> 30 runes) containing interrupt keywords should NOT trigger interrupt.
	// They're likely conversational context, not standalone commands.
	cases := []string{
		"이 작업을 중단하고 다른 것을 해줄 수 있어? 새로운 기능을 추가하고 싶은데",
		"중단 없이 계속 진행해줘, 잘 하고 있으니까",
		"can you stop using that library and switch to another approach instead?",
	}
	for _, msg := range cases {
		if got := classifyRoute(msg); got != RouteConcurrent {
			t.Errorf("classifyRoute(%q) = %d, want RouteConcurrent (long msg)", msg, got)
		}
	}
}

func TestClassifyRoute_SlashCommandsAlwaysInterrupt(t *testing.T) {
	// Slash commands should interrupt regardless of message length.
	cases := []string{
		"/kill please stop everything and start over",
		"/reset and start fresh with new approach",
		"/new conversation about something else entirely",
	}
	for _, msg := range cases {
		if got := classifyRoute(msg); got != RouteInterrupt {
			t.Errorf("classifyRoute(%q) = %d, want RouteInterrupt", msg, got)
		}
	}
}

func TestClassifyRoute_ShortNonInterrupt(t *testing.T) {
	// Short messages without interrupt keywords should be concurrent.
	cases := []string{
		"ㅋㅋ",
		"ok",
		"진행 상황은?",
		"고마워",
		"네",
	}
	for _, msg := range cases {
		if got := classifyRoute(msg); got != RouteConcurrent {
			t.Errorf("classifyRoute(%q) = %d, want RouteConcurrent", msg, got)
		}
	}
}

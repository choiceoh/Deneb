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

func TestClassifyRoute_NegatedInterrupt(t *testing.T) {
	// Negated interrupt keywords should NOT trigger interrupt.
	// "중단하지 마" = "don't stop", "취소하지마" = "don't cancel"
	cases := []string{
		"중단하지 마",
		"중단하지마",
		"멈추지 마",
		"취소하지 마",
		"취소하지마",
		"중지하지 마",
		"그만두지 마",
		"중단말고 계속",
	}
	for _, msg := range cases {
		if got := classifyRoute(msg); got != RouteConcurrent {
			t.Errorf("classifyRoute(%q) = %d, want RouteConcurrent (negated)", msg, got)
		}
	}
}

func TestClassifyRoute_LongMessageWithKeyword(t *testing.T) {
	// Long messages (> 30 runes) containing interrupt keywords should NOT trigger interrupt.
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

func TestIsNegated(t *testing.T) {
	tests := []struct {
		msg, kw string
		want    bool
	}{
		{"중단하지 마", "중단", true},
		{"중단하지마", "중단", true},
		{"취소하지 마", "취소", true},
		{"멈추지 마", "멈추", false}, // "멈추" is not "멈춰"
		{"중단해", "중단", false},
		{"중단", "중단", false},
		{"중단말고", "중단", true},
	}
	for _, tc := range tests {
		got := isNegated(tc.msg, tc.kw)
		if got != tc.want {
			t.Errorf("isNegated(%q, %q) = %v, want %v", tc.msg, tc.kw, got, tc.want)
		}
	}
}

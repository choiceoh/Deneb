package gmailpoll

import "testing"

func TestStripReasoningLeak(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain prose untouched", "발신자가 결제를 요청합니다.", "발신자가 결제를 요청합니다."},
		{"complete think block removed", "<think>먼저 이게 왜 왔는지...</think>핵심: 결제 기한 임박.", "핵심: 결제 기한 임박."},
		{"bracket thinking block removed", "[thinking]내부 추론[/thinking]요약입니다.", "요약입니다."},
		{"standalone markers removed", "결론<think>요약</think> 끝", "결론 끝"},
		{"multiline block removed", "<thinking>줄1\n줄2\n줄3</thinking>\n분석 결과", "\n분석 결과"},
		{"empty stays empty", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := stripReasoningLeak(c.in); got != c.want {
				t.Errorf("stripReasoningLeak(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestStripToolCallLeak(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"clean prose untouched", "발신: 이시연\n핵심: 결제 기한 임박.", "발신: 이시연\n핵심: 결제 기한 임박."},
		{
			"no markup leaves 하겠습니다 prose alone",
			"검토 후 회신하겠습니다.",
			"검토 후 회신하겠습니다.",
		},
		{
			"single block removed",
			"<tool_call>{\"name\": \"mail_archive\"}</tool_call>\n분석 보고",
			"분석 보고",
		},
		{
			// The reported leak: narration + several tool_call blocks before the report.
			"agent roleplay preamble dropped",
			"먼저 메일 목록을 확인하겠습니다.\n" +
				"<tool_call>{\"name\": \"mail_archive\", \"arguments\": {\"action\": \"list\"}}</tool_call>\n" +
				"이제 키워드를 추출하여 위키를 검색하겠습니다. 발신자는 이시연입니다.\n" +
				"<tool_call>{\"name\": \"wiki\", \"arguments\": {\"query\": \"비금 154kV\"}}</tool_call>\n" +
				"분석 보고\n발신: 이시연 → ZTT",
			"분석 보고\n발신: 이시연 → ZTT",
		},
		{
			"trailing markup removed, report preserved",
			"분석 보고\n핵심: 통관 승인.\n<tool_call>{\"name\": \"wiki\"}</tool_call>",
			"분석 보고\n핵심: 통관 승인.\n",
		},
		{"stray closing marker removed", "분석 결과</tool_call> 끝", "분석 결과 끝"},
		{"empty stays empty", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := stripToolCallLeak(c.in); got != c.want {
				t.Errorf("stripToolCallLeak(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

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

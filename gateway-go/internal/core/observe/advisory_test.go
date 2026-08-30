package observe

import "testing"

func TestIsAdvisory(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want bool
	}{
		// The live line that produced 3 of anomaly-watch's 7 HIGH findings on
		// 2026-08-30, and a no_effect skill candidate in genesis on 2026-07-25.
		{"observe-only 회귀 감지", "regression-watch: regression detected (observe-only)", true},
		{"advisory", "deadcode audit advisory: 3 new findings", true},
		{"dry-run", "wikirepair dry-run complete", true},
		{"will recover", "sidecar unreachable; will recover on next tick", true},
		{"no-op", "auto-deploy tick: no-op", true},

		// Narrowness is the point. Graceful degradation DOWNGRADES real defects,
		// so a working fallback is still a real upstream fault signal and must
		// keep reaching the diagnosis lanes.
		{"폴백 성공은 여전히 결함 신호", "wiki-dream: synthesis model failed; trying fallback", false},
		{"진짜 오류", "mail analysis 위키 저장 실패", false},
		{"빈 문자열", "", false},
		// "observed" merely contains the letters; the marker is a word.
		{"observed는 마커가 아니다", "observed 14 new pages during the sweep", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsAdvisory(LogLine{Msg: tc.msg}); got != tc.want {
				t.Fatalf("IsAdvisory(%q) = %v, want %v", tc.msg, got, tc.want)
			}
			if got := IsAdvisoryMessage(tc.msg); got != tc.want {
				t.Fatalf("IsAdvisoryMessage(%q) = %v, want %v", tc.msg, got, tc.want)
			}
		})
	}
}

// The marker is read from the message only: an `error` attr carries the
// underlying failure text, where these words would be coincidental.
func TestIsAdvisoryIgnoresAttrs(t *testing.T) {
	line := LogLine{
		Msg:   "wiki-verify: contradiction check failed",
		Attrs: map[string]string{"error": "upstream says this is advisory only"},
	}
	if IsAdvisory(line) {
		t.Fatal("an advisory word inside an error attr must not excuse the line")
	}
}

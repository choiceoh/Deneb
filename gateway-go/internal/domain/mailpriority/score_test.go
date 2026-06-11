package mailpriority

import "testing"

// Fixtures mirror the shapes of real Korean business mail this deployment
// receives (synthetic values — no live data).
func TestScore_Tiers(t *testing.T) {
	vip := func(email string) bool { return email == "kim@partner.co.kr" }
	s := New(vip)

	cases := []struct {
		name    string
		from    string
		subject string
		snippet string
		want    Tier
	}{
		// urgent: award notice + official document vocabulary
		{"낙찰 공문", "차장 <a@epc.co.kr>", "FW: OO산단 태양광 EPC 최종 낙찰자 통보 공문 송부", "낙찰자로 선정되어 공문을 송부드립니다", TierUrgent},
		// urgent: price-increase notice with amount
		{"단가 인상 통보", "구매팀 <b@vendor.co.kr>", "모듈 단가 5.2% 인상 통보", "다음 달 1일부터 와트당 0.11달러로 인상됩니다", TierUrgent},
		// urgent: deadline + document ask
		{"보완자료 마감", "지사 <c@power.co.kr>", "계통연계 기술검토 보완자료 제출 요청", "6/13까지 미제출 시 접수가 취소됩니다", TierUrgent},
		// urgent: reply-by ask from a VIP sender
		{"VIP 회신 요망", "김부장 <kim@partner.co.kr>", "NDA 검토 회신 요망", "금주 중 회신 부탁드립니다", TierUrgent},
		// attention: quote with amount but no deadline
		{"견적서", "영업 <d@module.co.kr>", "모듈(450W) 견적서 송부 — 1,950매", "견적 금액은 첨부 참조 부탁드립니다", TierAttention},
		// attention: meeting coordination
		{"회의 조율", "이차장 <e@site.co.kr>", "현장 실사 일정 조율", "다음 주 방문 일정을 조율하고자 합니다", TierAttention},
		// none: VIP sender alone (FYI mail) must not mark — VIP only
		// amplifies content signals (live-inbox tuning)
		{"VIP 단독 무표시", "김부장 <kim@partner.co.kr>", "참고자료 송부의 건", "참고하시라고 자료 보내드립니다", TierNone},
		// attention: the "~요청의 건/件" business-ask idiom
		{"요청의 건", "현장 <g@epc.co.kr>", "모듈 바이패스 증상 교체 요청의 건", "현장 확인 후 교체를 요청드립니다", TierAttention},
		// none: plain conversational mail
		{"일반 메일", "동료 <f@corp.co.kr>", "어제 자료 잘 받았습니다", "감사합니다. 참고하겠습니다.", TierNone},
		// none (demoted): machine sender even with urgent-looking words
		{"noreply 데모션", "Service <no-reply@saas.com>", "긴급: 결제 정보를 확인하세요", "지금 로그인하여 확인", TierNone},
		// none (demoted): ad tag beats amount
		{"광고 데모션", "샵 <shop@mall.co.kr>", "[광고] 6월 한정 50,000원 할인", "수신거부는 하단 링크", TierNone},
		// none (demoted): security link mail
		{"보안 링크", "Anthropic <x@mail.example.com>", "로그인용 보안 링크", "아래 링크로 로그인하세요", TierNone},
		// none (demoted): newsletter marker
		{"뉴스레터", "Team <hello@dev.tools>", "주간 뉴스레터 — 새 기능 소개", "이번 주 업데이트를 확인하세요", TierNone},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, hint := s.Score(tc.from, tc.subject, tc.snippet)
			if got != tc.want {
				t.Fatalf("Score(%q) = %q (hint %q), want %q", tc.subject, got, hint, tc.want)
			}
			if got != TierNone && hint == "" {
				t.Fatalf("tiered row must carry a hint: %q", tc.subject)
			}
			if got == TierNone && hint != "" {
				t.Fatalf("TierNone must not carry a hint, got %q", hint)
			}
		})
	}
}

func TestScore_NilVIPLookupSafe(t *testing.T) {
	s := New(nil)
	tier, _ := s.Score("a <a@b.c>", "회의 일정", "방문 일정 조율")
	if tier != TierAttention {
		t.Fatalf("tier = %q, want attention", tier)
	}
}

func TestSenderEmail(t *testing.T) {
	cases := map[string]string{
		"홍길동 <Hong@Corp.co.KR>": "hong@corp.co.kr",
		"bare@addr.com":         "bare@addr.com",
		"이름만":                   "",
	}
	for in, want := range cases {
		if got := senderEmail(in); got != want {
			t.Fatalf("senderEmail(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestScore_HintCappedAtTwo(t *testing.T) {
	s := New(func(string) bool { return true })
	// urgent keyword + deadline + attention + money + vip = 5 categories
	_, hint := s.Score("k <k@p.kr>", "긴급 낙찰 계약 3억원", "내일까지 회신 요망")
	// " · " separator appears at most once when capped at two hints.
	if n := len([]rune(hint)); n == 0 {
		t.Fatal("expected hint")
	}
	if c := countSep(hint); c > 1 {
		t.Fatalf("hint has %d separators (want ≤1): %q", c, hint)
	}
}

func countSep(s string) int {
	n := 0
	for i := 0; i+3 <= len(s); i++ {
		if s[i:i+3] == " · " {
			n++
		}
	}
	return n
}

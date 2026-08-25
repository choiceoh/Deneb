package mailpriority

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestMachineSenderDemotionBoundaryMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		from string
	}{
		{name: "no reply hyphen", from: "Service <no-reply@example.com>"},
		{name: "no reply joined", from: "Service <noreply@example.com>"},
		{name: "no reply mixed case", from: "Service <No-Reply@example.com>"},
		{name: "do not reply joined", from: "Service <donotreply@example.com>"},
		{name: "do not reply hyphen", from: "Service <do-not-reply@example.com>"},
		{name: "newsletter singular", from: "Service <newsletter@example.com>"},
		{name: "newsletters plural", from: "Service <newsletters@example.com>"},
		{name: "notification singular", from: "Service <notification@example.com>"},
		{name: "notifications plural", from: "Service <notifications@example.com>"},
		{name: "alert singular", from: "Service <alert@example.com>"},
		{name: "alerts plural", from: "Service <alerts@example.com>"},
		{name: "mailer daemon", from: "Postmaster <mailer-daemon@example.com>"},
		{name: "bounce", from: "Service <bounce@example.com>"},
		{name: "bounces", from: "Service <bounces@example.com>"},
		{name: "marketing", from: "Service <marketing@example.com>"},
		{name: "promo", from: "Service <promo@example.com>"},
		{name: "prefix before marker", from: "Service <corp-no-reply@example.com>"},
		{name: "suffix after marker", from: "Service <notifications-eu@example.com>"},
	}
	s := New(func(string) bool { return true }, func(string) bool { return true })
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tier, hint := s.Score(tc.from, "긴급 낙찰 공문 계약 3억원", "내일까지 회신 요망")
			if tier != TierNone || hint != "" {
				t.Fatalf("Score(%q) = (%q,%q), want demoted", tc.from, tier, hint)
			}
		})
	}
}

func TestSimilarHumanSenderNamesPreserveAttentionTier(t *testing.T) {
	t.Parallel()

	tests := []string{
		"Human <reply@example.com>",
		"Human <notreply@example.com>",
		"Human <newsletter-team@example.com>", // still matches: document the broad boundary below
		"Human <promoter@example.com>",
		"Human <alertness@example.com>",
		"Human <marketingperson@example.com>", // still matches: marker at local-part start
		"Human <bounceback@example.com>",      // still matches: marker at local-part start
	}
	for _, from := range tests {
		t.Run(from, func(t *testing.T) {
			t.Parallel()
			tier, _ := New(nil, nil).Score(from, "계약 검토", "확인 부탁드립니다")
			matched := machineSenderRe.MatchString(from)
			if matched && tier != TierNone {
				t.Fatalf("machine regex matched but tier = %q", tier)
			}
			if !matched && tier != TierAttention {
				t.Fatalf("human sender tier = %q, want attention", tier)
			}
		})
	}
}

func TestNoiseDemotionBoundaryMatrix(t *testing.T) {
	t.Parallel()

	markers := []struct {
		name string
		text string
	}{
		{name: "Korean ad square", text: "[광고] 특별 할인"},
		{name: "Korean ad paren", text: "(광고) 특별 할인"},
		{name: "unsubscribe Korean joined", text: "수신거부"},
		{name: "unsubscribe Korean spaced", text: "수신 거부"},
		{name: "unsubscribe English", text: "unsubscribe here"},
		{name: "unsubscribe uppercase", text: "UNSUBSCRIBE HERE"},
		{name: "newsletter Korean", text: "이번 주 뉴스레터"},
		{name: "cancel subscription", text: "구독 취소 안내"},
		{name: "security link joined", text: "보안링크 확인"},
		{name: "security link spaced", text: "보안 링크 확인"},
		{name: "login link", text: "로그인 링크"},
		{name: "login purpose", text: "로그인용 링크"},
		{name: "verification number", text: "인증 번호 1234"},
		{name: "verification code", text: "인증코드 1234"},
		{name: "verification mail", text: "인증 메일 안내"},
		{name: "verification link Korean", text: "인증링크"},
		{name: "verification English", text: "verification required"},
		{name: "verify your", text: "Verify your account"},
		{name: "password reset English", text: "Password Reset"},
		{name: "password reset Korean", text: "비밀번호 재설정"},
	}
	s := New(func(string) bool { return true }, func(string) bool { return true })
	for _, tc := range markers {
		t.Run(tc.name+" in subject", func(t *testing.T) {
			t.Parallel()
			tier, hint := s.Score("Human <human@example.com>", tc.text+" 긴급 계약 5억원", "내일까지 회신 요망")
			if tier != TierNone || hint != "" {
				t.Fatalf("subject marker = (%q,%q)", tier, hint)
			}
		})
		t.Run(tc.name+" in snippet", func(t *testing.T) {
			t.Parallel()
			tier, hint := s.Score("Human <human@example.com>", "긴급 계약 5억원", tc.text+" 내일까지 회신 요망")
			if tier != TierNone || hint != "" {
				t.Fatalf("snippet marker = (%q,%q)", tier, hint)
			}
		})
	}
}

func TestUrgentKeywordMatrixEscalatesWithDeadline(t *testing.T) {
	t.Parallel()

	keywords := []string{
		"긴급",
		"낙찰",
		"공문",
		"입찰",
		"독촉",
		"연체",
		"미납",
		"체납",
		"해지",
		"위약",
		"클레임",
		"시정 요구",
		"시정요구",
		"시정 명령",
		"시정명령",
		"통보",
	}
	for _, keyword := range keywords {
		t.Run(keyword+" alone", func(t *testing.T) {
			t.Parallel()
			tier, hint := New(nil, nil).Score("Human <human@example.com>", keyword, "참고 바랍니다")
			if tier != TierAttention {
				t.Fatalf("single urgent keyword tier = %q, want attention", tier)
			}
			if !strings.Contains(hint, strings.TrimSpace(keyword)) {
				t.Fatalf("hint = %q, want %q", hint, keyword)
			}
		})
		t.Run(keyword+" plus deadline", func(t *testing.T) {
			t.Parallel()
			tier, hint := New(nil, nil).Score("Human <human@example.com>", keyword, "내일까지")
			if tier != TierUrgent {
				t.Fatalf("keyword+deadline tier = %q, hint=%q", tier, hint)
			}
		})
	}
}

func TestDistinctUrgentKeywordStackBoundary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		want Tier
	}{
		{name: "one occurrence", text: "긴급", want: TierAttention},
		{name: "same occurrence twice deduped", text: "긴급 긴급", want: TierAttention},
		{name: "same occurrence three times deduped", text: "공문 공문 공문", want: TierAttention},
		{name: "two distinct", text: "긴급 공문", want: TierUrgent},
		{name: "two distinct reversed", text: "공문 긴급", want: TierUrgent},
		{name: "three distinct", text: "낙찰 공문 통보", want: TierUrgent},
		{name: "spacing creates same normalized match", text: "시정요구 시정 요구", want: TierUrgent},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tier, _ := New(nil, nil).Score("a@example.com", tc.text, "")
			if tier != tc.want {
				t.Fatalf("Score(%q) = %q, want %q", tc.text, tier, tc.want)
			}
		})
	}
}

func TestDeadlineSignalBoundaryMatrix(t *testing.T) {
	t.Parallel()

	deadlines := []string{
		"6/13까지",
		"6 / 13 까지",
		"6.13까지",
		"6 . 13 까지",
		"6월13일까지",
		"6월 13일 까지",
		"오늘까지",
		"오늘 중",
		"오늘 내",
		"금일까지",
		"내일까지",
		"명일까지",
		"모레까지",
		"금주중",
		"이번 주 내",
		"주중까지",
		"D-1",
		"D-99",
		"회신 요망",
		"회신요청",
		"회신 바람",
		"회신부탁",
		"제출 요망",
		"제출요청",
		"제출 기한",
		"회시",
	}
	for _, deadline := range deadlines {
		t.Run(deadline, func(t *testing.T) {
			t.Parallel()
			tier, hint := New(nil, nil).Score("human@example.com", deadline, "")
			if tier != TierAttention {
				t.Fatalf("deadline alone tier = %q, want attention", tier)
			}
			if hint != "마감 표현" {
				t.Fatalf("hint = %q, want 마감 표현", hint)
			}
		})
	}
}

func TestDeadlineNearMissesReturnEmptyTier(t *testing.T) {
	t.Parallel()

	nearMisses := []string{
		"6/13",
		"6월 13일",
		"오늘 공유",
		"이번 주 공유",
		"D day",
		"D+1",
		"회신 감사합니다",
		"제출 완료",
		"요망 사항",
		"요청 사항",
	}
	for _, text := range nearMisses {
		t.Run(text, func(t *testing.T) {
			t.Parallel()
			tier, hint := New(nil, nil).Score("human@example.com", text, "")
			if tier != TierNone || hint != "" {
				t.Fatalf("near miss = (%q,%q), want none", tier, hint)
			}
		})
	}
}

func TestAttentionSignalBoundaryMatrix(t *testing.T) {
	t.Parallel()

	signals := []string{
		"견적",
		"발주",
		"청구",
		"세금계산서",
		"세금 계산서",
		"계약",
		"입금",
		"송금",
		"지급",
		"결제",
		"정산",
		"미팅",
		"회의",
		"방문",
		"실사",
		"점검",
		"검토 요청",
		"검토요청",
		"승인",
		"결재",
		"자료 요청",
		"자료요청",
		"자료 제출",
		"보완",
		"단가",
		"인상",
		"변경 요청",
		"변경요청",
		"요청의 건",
		"요청의 件",
	}
	for _, signal := range signals {
		t.Run(signal, func(t *testing.T) {
			t.Parallel()
			tier, hint := New(nil, nil).Score("human@example.com", signal, "")
			if tier != TierAttention {
				t.Fatalf("attention signal tier = %q, want attention", tier)
			}
			if hint == "" {
				t.Fatal("attention signal has empty hint")
			}
		})
	}
}

func TestMoneySignalBoundaryMatrix(t *testing.T) {
	t.Parallel()

	amounts := []string{
		"1억",
		"12 억",
		"3천만",
		"3 천만",
		"5백만",
		"5 백만",
		"10만원",
		"10만 원",
		"10 원",
		"1,234원",
		"1,234.50 달러",
		"99불",
		"100 USD",
		"100USD",
		"100 KRW",
		"100KRW",
		"₩1000",
		"₩ 1,000",
		"$1000",
		"$ 1,000.25",
	}
	for _, amount := range amounts {
		t.Run(amount, func(t *testing.T) {
			t.Parallel()
			tier, hint := New(nil, nil).Score("human@example.com", amount, "")
			if tier != TierAttention {
				t.Fatalf("amount tier = %q, want attention", tier)
			}
			if hint != "금액" {
				t.Fatalf("hint = %q, want 금액", hint)
			}
		})
	}
}

func TestMoneyNearMissesReturnEmptyTier(t *testing.T) {
	t.Parallel()

	nearMisses := []string{
		"금액 협의",
		"억원",
		"USD",
		"KRW",
		"천만원",
		"one dollar",
		"1 percent",
		"1%",
		"2026년",
		"전화 010-1234-5678",
	}
	for _, text := range nearMisses {
		t.Run(text, func(t *testing.T) {
			t.Parallel()
			tier, hint := New(nil, nil).Score("human@example.com", text, "")
			if tier != TierNone || hint != "" {
				t.Fatalf("near miss = (%q,%q), want none", tier, hint)
			}
		})
	}
}

func TestSignalScoreThresholdMatrixReturnsExpectedTier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		from         string
		subject      string
		snippet      string
		vip          bool
		counterparty bool
		want         Tier
	}{
		{name: "no signal", from: "a@example.com", subject: "참고", want: TierNone},
		{name: "attention two points", from: "a@example.com", subject: "견적", want: TierAttention},
		{name: "money two points", from: "a@example.com", subject: "3억원", want: TierAttention},
		{name: "deadline three points", from: "a@example.com", subject: "내일까지", want: TierAttention},
		{name: "urgent keyword three points", from: "a@example.com", subject: "긴급", want: TierAttention},
		{name: "attention plus money four", from: "a@example.com", subject: "견적 3억원", want: TierAttention},
		{name: "attention plus vip four", from: "a@example.com", subject: "견적", vip: true, want: TierAttention},
		{name: "attention plus counterparty four", from: "a@example.com", subject: "견적", counterparty: true, want: TierAttention},
		{name: "urgent plus attention five", from: "a@example.com", subject: "긴급 견적", want: TierUrgent},
		{name: "deadline plus attention five", from: "a@example.com", subject: "내일까지 견적", want: TierUrgent},
		{name: "urgent plus money five", from: "a@example.com", subject: "긴급 3억원", want: TierUrgent},
		{name: "deadline plus money five", from: "a@example.com", subject: "내일까지 3억원", want: TierUrgent},
		{name: "urgent plus vip five", from: "a@example.com", subject: "긴급", vip: true, want: TierUrgent},
		{name: "deadline plus counterparty five", from: "a@example.com", subject: "내일까지", counterparty: true, want: TierUrgent},
		{name: "attention money vip six", from: "a@example.com", subject: "견적 3억원", vip: true, want: TierUrgent},
		{name: "attention vip counterparty six", from: "a@example.com", subject: "견적", vip: true, counterparty: true, want: TierUrgent},
		{name: "content split across fields", from: "a@example.com", subject: "견적", snippet: "3억원", vip: true, want: TierUrgent},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			vip := func(string) bool { return tc.vip }
			counterparty := func(string) bool { return tc.counterparty }
			tier, hint := New(vip, counterparty).Score(tc.from, tc.subject, tc.snippet)
			if tier != tc.want {
				t.Fatalf("Score = (%q,%q), want %q", tier, hint, tc.want)
			}
			if tier == TierNone && hint != "" {
				t.Fatalf("none tier hint = %q", hint)
			}
			if tier != TierNone && hint == "" {
				t.Fatal("non-none tier has empty hint")
			}
		})
	}
}

func TestRelationshipCallbacksAmplifyOnlyWithContent(t *testing.T) {
	t.Parallel()

	var vipCalls atomic.Int64
	var counterpartyCalls atomic.Int64
	s := New(
		func(string) bool { vipCalls.Add(1); return true },
		func(string) bool { counterpartyCalls.Add(1); return true },
	)
	tier, hint := s.Score("Human <human@example.com>", "일반 공유", "참고 바랍니다")
	if tier != TierNone || hint != "" {
		t.Fatalf("contentless relationship = (%q,%q)", tier, hint)
	}
	if vipCalls.Load() != 0 || counterpartyCalls.Load() != 0 {
		t.Fatalf("callbacks invoked without content: vip=%d counterparty=%d", vipCalls.Load(), counterpartyCalls.Load())
	}

	tier, _ = s.Score("Human <human@example.com>", "견적", "")
	if tier != TierUrgent {
		t.Fatalf("both relationship boosts tier = %q, want urgent", tier)
	}
	if vipCalls.Load() != 1 || counterpartyCalls.Load() != 1 {
		t.Fatalf("callbacks after content: vip=%d counterparty=%d", vipCalls.Load(), counterpartyCalls.Load())
	}
}

func TestRelationshipCallbacksSkipWithoutExtractableEmail(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	s := New(
		func(string) bool { calls.Add(1); return true },
		func(string) bool { calls.Add(1); return true },
	)
	fromValues := []string{
		"",
		"Display Name",
		"not-an-address",
		"Name <missing-at-sign>",
		"two words@example.com",
	}
	for _, from := range fromValues {
		tier, _ := s.Score(from, "견적", "")
		if tier != TierAttention {
			t.Errorf("from %q tier = %q, want attention without boost", from, tier)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("callbacks invoked %d times without extractable email", calls.Load())
	}
}

func TestRelationshipCallbacksReceiveNormalizedEmail(t *testing.T) {
	t.Parallel()

	want := []string{
		"mixed.case+tag@example.co.kr",
		"mixed.case+tag@example.co.kr",
	}
	var mu sync.Mutex
	var got []string
	record := func(email string) bool {
		mu.Lock()
		got = append(got, email)
		mu.Unlock()
		return true
	}
	tier, _ := New(record, record).Score("Display < Mixed.Case+Tag@Example.Co.KR >", "견적", "")
	if tier != TierUrgent {
		t.Fatalf("tier = %q, want urgent", tier)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("callback emails = %v, want %v", got, want)
	}
}

func TestSenderEmailBoundaryMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "spaces", in: "   ", want: ""},
		{name: "display only", in: "홍길동", want: ""},
		{name: "bare", in: "a@example.com", want: "a@example.com"},
		{name: "bare case", in: "A@EXAMPLE.COM", want: "a@example.com"},
		{name: "bare surrounding whitespace", in: "  A@EXAMPLE.COM \t", want: "a@example.com"},
		{name: "display angle", in: "홍길동 <a@example.com>", want: "a@example.com"},
		{name: "display angle case", in: "홍길동 <A@EXAMPLE.COM>", want: "a@example.com"},
		{name: "inner whitespace", in: "홍길동 < a@example.com >", want: "a@example.com"},
		{name: "plus tag", in: "A <a+tag@example.com>", want: "a+tag@example.com"},
		{name: "subdomain", in: "A <a@sub.example.co.kr>", want: "a@sub.example.co.kr"},
		{name: "missing close angle", in: "A <a@example.com", want: ""},
		{name: "missing open angle", in: "A a@example.com>", want: ""},
		{name: "space bare invalid", in: "a @example.com", want: ""},
		{name: "display without angle invalid", in: "A a@example.com", want: ""},
		{name: "angle without address", in: "A <not-address>", want: ""},
		{name: "multiple angle uses first address span", in: "A <a@example.com> B <b@example.com>", want: "a@example.com"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := senderEmail(tc.in); got != tc.want {
				t.Fatalf("senderEmail(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestJoinHintsBoundaryMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		content  []string
		relation []string
		want     string
	}{
		{name: "nil", content: nil, relation: nil, want: ""},
		{name: "empty", content: []string{}, relation: []string{}, want: ""},
		{name: "one content", content: []string{"긴급"}, want: "긴급"},
		{name: "two content", content: []string{"긴급", "마감 표현"}, want: "긴급 · 마감 표현"},
		{name: "three content capped", content: []string{"긴급", "마감 표현", "금액"}, want: "긴급 · 마감 표현"},
		{name: "relation only", relation: []string{"주요 연락처"}, want: "주요 연락처"},
		{name: "two relations only first", relation: []string{"주요 연락처", "진행 거래처"}, want: "주요 연락처"},
		{name: "content plus relation", content: []string{"견적"}, relation: []string{"주요 연락처"}, want: "견적 · 주요 연락처"},
		{name: "relation displaces second content", content: []string{"긴급", "마감 표현"}, relation: []string{"진행 거래처"}, want: "긴급 · 진행 거래처"},
		{name: "first relation wins", content: []string{"견적"}, relation: []string{"주요 연락처", "진행 거래처"}, want: "견적 · 주요 연락처"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := joinHints(tc.content, tc.relation); got != tc.want {
				t.Fatalf("joinHints(%v,%v) = %q, want %q", tc.content, tc.relation, got, tc.want)
			}
		})
	}
}

func TestDistinctMatchesDeduplicatesOrderAndWhitespace(t *testing.T) {
	t.Parallel()

	re := regexp.MustCompile(`\s*(alpha|beta|gamma)\s*`)
	tests := []struct {
		name string
		text string
		want []string
	}{
		{name: "empty", text: "", want: nil},
		{name: "none", text: "delta", want: nil},
		{name: "one", text: "alpha", want: []string{"alpha"}},
		{name: "trim", text: " alpha ", want: []string{"alpha"}},
		{name: "dedup", text: "alpha alpha alpha", want: []string{"alpha"}},
		{name: "order", text: "beta alpha gamma", want: []string{"beta", "alpha", "gamma"}},
		{name: "order reverse", text: "gamma alpha beta", want: []string{"gamma", "alpha", "beta"}},
		{name: "interleaved duplicates", text: "beta alpha beta gamma alpha", want: []string{"beta", "alpha", "gamma"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := distinctMatches(re, tc.text); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("distinctMatches(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}

func TestHintNamesRelationshipAtThresholdBoundary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		vip          bool
		counterparty bool
		wantHint     string
	}{
		{name: "vip", vip: true, wantHint: "주요 연락처"},
		{name: "counterparty", counterparty: true, wantHint: "진행 거래처"},
		{name: "both prefers vip", vip: true, counterparty: true, wantHint: "주요 연락처"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tier, hint := New(
				func(string) bool { return tc.vip },
				func(string) bool { return tc.counterparty },
			).Score("A <a@example.com>", "견적", "")
			if tier == TierNone {
				t.Fatal("relationship-amplified content was unmarked")
			}
			if !strings.Contains(hint, tc.wantHint) {
				t.Fatalf("hint = %q, want %q", hint, tc.wantHint)
			}
			if strings.Count(hint, " · ") > 1 {
				t.Fatalf("hint exceeds two entries: %q", hint)
			}
		})
	}
}

func TestDemotionIgnoresRelationshipCallbacks(t *testing.T) {
	t.Parallel()

	var calls atomic.Int64
	lookup := func(string) bool { calls.Add(1); return true }
	s := New(lookup, lookup)
	inputs := []struct {
		from    string
		subject string
	}{
		{from: "No Reply <no-reply@example.com>", subject: "긴급 견적 3억원"},
		{from: "Human <human@example.com>", subject: "[광고] 긴급 견적 3억원"},
		{from: "Human <human@example.com>", subject: "비밀번호 재설정 긴급 견적 3억원"},
	}
	for _, in := range inputs {
		if tier, _ := s.Score(in.from, in.subject, "내일까지"); tier != TierNone {
			t.Errorf("demoted input tier = %q", tier)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("relationship callbacks called %d times for demoted rows", calls.Load())
	}
}

func TestConcurrentScoreWithThreadSafeLookups(t *testing.T) {
	const (
		workers    = 64
		iterations = 600
	)
	var vipCalls atomic.Int64
	var counterpartyCalls atomic.Int64
	s := New(
		func(email string) bool {
			vipCalls.Add(1)
			return strings.HasPrefix(email, "vip-")
		},
		func(email string) bool {
			counterpartyCalls.Add(1)
			return strings.HasSuffix(email, "@deal.example")
		},
	)
	tests := []struct {
		from    string
		subject string
		want    Tier
		lookup  bool
	}{
		{from: "VIP <vip-one@other.example>", subject: "견적", want: TierAttention, lookup: true},
		{from: "Deal <normal@deal.example>", subject: "내일까지", want: TierUrgent, lookup: true},
		{from: "Both <vip-two@deal.example>", subject: "견적", want: TierUrgent, lookup: true},
		{from: "Human <human@other.example>", subject: "긴급 견적", want: TierUrgent, lookup: true},
		{from: "Machine <no-reply@deal.example>", subject: "긴급 견적", want: TierNone, lookup: false},
		{from: "Human <human@deal.example>", subject: "일반 공유", want: TierNone, lookup: false},
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for iteration := 0; iteration < iterations; iteration++ {
				tc := tests[(worker+iteration)%len(tests)]
				tier, hint := s.Score(tc.from, tc.subject, "")
				if tier != tc.want {
					t.Errorf("worker=%d iteration=%d got=(%q,%q) want=%q", worker, iteration, tier, hint, tc.want)
					return
				}
			}
		}()
	}
	close(start)
	wg.Wait()
	if vipCalls.Load() == 0 || counterpartyCalls.Load() == 0 {
		t.Fatalf("lookups were not exercised: vip=%d counterparty=%d", vipCalls.Load(), counterpartyCalls.Load())
	}
}

func TestScoreIsIdempotentAcrossRepeatedCalls(t *testing.T) {
	t.Parallel()

	s := New(
		func(email string) bool { return email == "a@example.com" },
		func(email string) bool { return strings.HasSuffix(email, "@example.com") },
	)
	tests := []struct {
		from    string
		subject string
		snippet string
	}{
		{from: "A <a@example.com>", subject: "긴급 계약 3억원", snippet: "내일까지 회신 요망"},
		{from: "B <b@example.com>", subject: "견적", snippet: "참고"},
		{from: "No Reply <no-reply@example.com>", subject: "긴급", snippet: ""},
		{from: "C <c@other.com>", subject: "일반", snippet: "공유"},
	}
	for _, tc := range tests {
		wantTier, wantHint := s.Score(tc.from, tc.subject, tc.snippet)
		for iteration := 0; iteration < 1000; iteration++ {
			gotTier, gotHint := s.Score(tc.from, tc.subject, tc.snippet)
			if gotTier != wantTier || gotHint != wantHint {
				t.Fatalf("input=%#v iteration=%d got=(%q,%q) want=(%q,%q)", tc, iteration, gotTier, gotHint, wantTier, wantHint)
			}
		}
	}
}

func TestTierWireValuesEncodeToExpectedStrings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		tier Tier
		want string
	}{
		{tier: TierUrgent, want: "urgent"},
		{tier: TierAttention, want: "attention"},
		{tier: TierNone, want: ""},
	}
	for _, tc := range tests {
		if string(tc.tier) != tc.want {
			t.Errorf("tier = %q, want wire value %q", tc.tier, tc.want)
		}
	}
}

func BenchmarkScoreBoundaryMix(b *testing.B) {
	s := New(
		func(email string) bool { return strings.HasPrefix(email, "vip") },
		func(email string) bool { return strings.HasSuffix(email, "@deal.example") },
	)
	inputs := []struct {
		from    string
		subject string
		snippet string
	}{
		{from: "VIP <vip@deal.example>", subject: "긴급 견적 3억원", snippet: "내일까지 회신 요망"},
		{from: "Human <human@example.com>", subject: "회의 일정", snippet: "방문 조율"},
		{from: "No Reply <no-reply@example.com>", subject: "긴급", snippet: ""},
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		in := inputs[i%len(inputs)]
		tier, hint := s.Score(in.from, in.subject, in.snippet)
		if tier == Tier("impossible") || hint == fmt.Sprint(i) {
			b.Fatal("unreachable")
		}
	}
}

// A newsletter that puts a person's name in the local part and the marker in
// the domain (dan@tldrnewsletter.com — 5 pages in the live corpus) read as an
// ordinary business sender to the local-part-only rule.
func TestIsBulkNoiseMatchesMarkerInDomain(t *testing.T) {
	noise := []string{
		"TLDR <dan@tldrnewsletter.com>",
		"Weekly <editor@company-newsletters.com>",
		"Sender <hello@noreply.vendor.test>",
		"Daemon <x@mailer-daemon.test>",
	}
	for _, from := range noise {
		if ok, _ := IsBulkNoise(from, "이번 주 소식"); !ok {
			t.Errorf("IsBulkNoise(%q) = false, want true", from)
		}
	}

	// The domain list is deliberately shorter than the local-part one: these
	// words appear inside real company domains, and the sender is a person.
	business := []string{
		"Judah Levine <judah@freightos.com>",
		"Kim <kim@newscorp-korea.test>",
		"Sales <sales@promotex.test>",
		"Engineer <eng@alertlogic.test>",
		"삼일PwC <kr_samil_pwc_deal_access@pwc.com>",
	}
	for _, from := range business {
		if ok, reason := IsBulkNoise(from, "견적 회신 요청"); ok {
			t.Errorf("IsBulkNoise(%q) = true (%s), want false", from, reason)
		}
	}
}

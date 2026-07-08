package server

import (
	"strings"
	"testing"
)

// TestStripCardPreamble locks the deneb-ui card contract: at most one short
// greeting line may precede the ```deneb-ui fence. The multi-line leak case is
// the real 2026-07-08 morning-letter incident (a truncated tool payload pushed
// the model to narrate its workaround + dump its notes ahead of the card).
func TestStripCardPreamble(t *testing.T) {
	// Card body plus a closing paragraph and the (intentional) model tag — all
	// of which live AFTER the fence and must survive untouched.
	const card = "```deneb-ui\n<column><text style=\"headline\">7월 8일 수요일</text></column>\n```\n\n**오늘 핵심:** 강진 신다산 계약이 가장 급합니다.\n\nglm-5.2"

	cases := []struct {
		name string
		text string
		want string
	}{
		{
			name: "clean one-line greeting is kept",
			text: "좋은 아침이에요 — 2026년 7월 8일 수요일. 강진 신다산 계약이 가장 급합니다.\n\n" + card,
			want: "좋은 아침이에요 — 2026년 7월 8일 수요일. 강진 신다산 계약이 가장 급합니다.\n\n" + card,
		},
		{
			name: "multi-line narration + data dump leak is stripped (2026-07-08)",
			text: "위키에서 주요 due 항목을 파악했습니다. morning_letter 원본 JSON에서 임박 마감 섹션 부분이 잘려있었는데, 핵심 데이터는 충분히 확보했습니다. 모닝레터 스킬 규칙에 따라 작성합니다.\n\n핵심 데이터 정리:\n- 날씨: 26°C\n- 환율: 1,530.80\n\n" + card,
			want: card,
		},
		{
			name: "no card fence is returned unchanged",
			text: "카드 없는 평문 응답입니다.",
			want: "카드 없는 평문 응답입니다.",
		},
		{
			name: "deliverable already opening with the fence is unchanged",
			text: card,
			want: card,
		},
		{
			name: "single oversized narration line is stripped",
			text: strings.Repeat("가", 130) + "\n\n" + card,
			want: card,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := stripCardPreamble(c.text); got != c.want {
				t.Fatalf("stripCardPreamble()\n got: %q\nwant: %q", got, c.want)
			}
		})
	}
}

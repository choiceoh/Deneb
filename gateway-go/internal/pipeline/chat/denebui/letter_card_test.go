package denebui

import "testing"

// Canonical morning/evening letter cards, mirroring the deneb-ui skeletons in
// skills/productivity/{morning,evening}-letter/SKILL.md. These tests are a
// server-side gate: the letter skeletons the agent copies+fills must stay
// schema-valid against the deneb-ui node spec, so a malformed template can't
// silently ship a broken card. Keep the JSON here in sync with the SKILL.md
// fences.

const morningLetterCard = `{
  "type": "column",
  "children": [
    { "type": "card", "children": [
      { "type": "row", "children": [
        { "type": "icon", "name": "sunny", "size": 16 },
        { "type": "text", "value": "날씨 · 광주", "style": "caption" } ] },
      { "type": "row", "children": [
        { "type": "text", "value": "18°", "style": "headline" },
        { "type": "text", "value": "체감 16°", "style": "caption" } ] },
      { "type": "text", "value": "최고 24° · 최저 14° · 강수 30%", "style": "caption" },
      { "type": "text", "value": "오후 소나기 가능 — 우산 챙기세요", "style": "body" } ] },

    { "type": "card", "children": [
      { "type": "row", "children": [
        { "type": "icon", "name": "payments", "size": 16 },
        { "type": "text", "value": "환율 · 구리", "style": "caption" } ] },
      { "type": "row", "children": [
        { "type": "stat", "value": "1,386", "label": "USD/KRW" },
        { "type": "stat", "value": "1,498", "label": "EUR/KRW" } ] },
      { "type": "stat", "value": "$9,540 /t", "label": "LME 구리" } ] },

    { "type": "card", "children": [
      { "type": "row", "children": [
        { "type": "icon", "name": "calendar", "size": 16 },
        { "type": "text", "value": "오늘 일정", "style": "caption" } ] },
      { "type": "list", "items": [
        { "type": "text", "value": "09:00 — 팀 스탠드업" },
        { "type": "text", "value": "14:00 — 거래처 미팅" } ] } ] },

    { "type": "card", "children": [
      { "type": "row", "children": [
        { "type": "icon", "name": "mail", "size": 16 },
        { "type": "text", "value": "전일 메일", "style": "caption" } ] },
      { "type": "list", "items": [
        { "type": "text", "value": "김부장 — 견적서 회신 요청" },
        { "type": "text", "value": "세무서 — 부가세 신고 안내" } ] } ] },

    { "type": "card", "children": [
      { "type": "row", "children": [
        { "type": "icon", "name": "alarm", "size": 16 },
        { "type": "text", "value": "임박 마감", "style": "caption" } ] },
      { "type": "row", "children": [
        { "type": "text", "value": "부가세 신고", "style": "body" },
        { "type": "badge", "value": "D-2" } ] } ] }
  ]
}`

const eveningLetterCard = `{
  "type": "column",
  "children": [
    { "type": "card", "children": [
      { "type": "row", "children": [
        { "type": "icon", "name": "calendar", "size": 16 },
        { "type": "text", "value": "내일 일정", "style": "caption" } ] },
      { "type": "list", "items": [
        { "type": "text", "value": "10:00 — 분기 리뷰" },
        { "type": "text", "value": "15:00 — 거래처 콜" } ] } ] },

    { "type": "card", "children": [
      { "type": "row", "children": [
        { "type": "icon", "name": "mail", "size": 16 },
        { "type": "text", "value": "챙길 메일", "style": "caption" } ] },
      { "type": "list", "items": [
        { "type": "text", "value": "이대리 — 내일 회의자료 공유" } ] } ] },

    { "type": "card", "children": [
      { "type": "row", "children": [
        { "type": "icon", "name": "alarm", "size": 16 },
        { "type": "text", "value": "임박 마감", "style": "caption" } ] },
      { "type": "row", "children": [
        { "type": "text", "value": "부가세 신고", "style": "body" },
        { "type": "badge", "value": "D-2" } ] } ] }
  ]
}`

func TestValidate_LetterCards(t *testing.T) {
	for name, body := range map[string]string{
		"morning": morningLetterCard,
		"evening": eveningLetterCard,
	} {
		t.Run(name, func(t *testing.T) {
			issues, err := Validate(body)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			if len(issues) != 0 {
				t.Errorf("letter card must be schema-valid, got %d issue(s): %v", len(issues), issues)
			}
		})
	}
}

// The delivered message is a plain head line followed by the deneb-ui fence.
// ExtractFences must recover exactly the card body, and it must validate — this
// guards the real on-the-wire shape the morning-letter skill emits.
func TestValidate_LetterMessageShape(t *testing.T) {
	msg := "좋은 아침이에요 — 6월 28일 토요일. 오후 소나기 · 부가세 신고 D-2\n\n" +
		"```deneb-ui\n" + morningLetterCard + "\n```\n"
	fences := ExtractFences(msg)
	if len(fences) != 1 {
		t.Fatalf("expected exactly 1 deneb-ui fence, got %d", len(fences))
	}
	issues, err := Validate(fences[0])
	if err != nil {
		t.Fatalf("parse error on extracted fence: %v", err)
	}
	if len(issues) != 0 {
		t.Errorf("extracted card must be valid, got issues: %v", issues)
	}
}

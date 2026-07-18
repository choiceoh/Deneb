package denebui

import "testing"

// Canonical legacy-authored morning and current evening letter examples in the
// labeled-HTML wire format. The production morning card is now assembled and
// validator-gated in tools/routine/morning_card.go; this fixture remains a wire
// compatibility check for existing transcripts. The evening example mirrors
// the evening_letter tool's model-authored output contract.
//
// The market stat values carry the AUTHORING form — digit-free letter tokens
// ("{{market:usd_krw}}") the proactive relay substitutes with fetched numbers
// at delivery (domain/market letter_tokens.go). The skeleton must validate in
// that pre-substitution shape, since that is what the model actually emits.

const morningLetterCardHTML = `<column>
  <text style="headline">7월 7일 화요일</text>
  <text style="caption">아침 레터 · 데네브</text>
  <hr/>
  <card>
    <row><icon name="sunny" size="16"/><text style="caption">날씨 · 광주</text></row>
    <row><text style="headline">18°</text><text style="caption">체감 16°</text></row>
    <text style="caption">최고 24° · 최저 14° · 강수 30%</text>
    <text style="body">오후 소나기 가능 — 우산 챙기세요</text>
  </card>
  <card>
    <row><icon name="payments" size="16"/><text style="caption">환율 · 구리</text></row>
    <row><stat value="{{market:usd_krw}}" label="USD/KRW"/><stat value="${{market:copper}} /t" label="LME 구리"/></row>
  </card>
  <card>
    <row><icon name="calendar" size="16"/><text style="caption">오늘 일정</text></row>
    <ul><li>09:00 — 팀 스탠드업</li><li>14:00 — 거래처 미팅</li></ul>
  </card>
  <card>
    <row><icon name="mail" size="16"/><text style="caption">전일 메일</text></row>
    <ul><li>김부장 — 견적서 회신 요청</li><li>세무서 — 부가세 신고 안내</li></ul>
  </card>
  <card>
    <row><icon name="alarm" size="16"/><text style="caption">임박 마감</text></row>
    <row><text style="body">부가세 신고</text><badge color="warning">D-2</badge></row>
    <row><text style="body">진코 선입금 상계</text><badge color="error">기한 초과</badge></row>
  </card>
</column>`

const eveningLetterCardHTML = `<column>
  <text style="headline">7월 7일 화요일 저녁</text>
  <text style="caption">이브닝 레터 · 데네브</text>
  <hr/>
  <card>
    <row><icon name="calendar" size="16"/><text style="caption">내일 일정</text></row>
    <ul><li>10:00 — 분기 리뷰</li><li>15:00 — 거래처 콜</li></ul>
  </card>
  <card>
    <row><icon name="mail" size="16"/><text style="caption">챙길 메일</text></row>
    <ul><li>이대리 — 내일 회의자료 공유</li></ul>
  </card>
  <card>
    <row><icon name="alarm" size="16"/><text style="caption">임박 마감</text></row>
    <row><text style="body">부가세 신고</text><badge color="warning">D-2</badge></row>
  </card>
</column>`

func TestValidateReturnsNoIssuesForLetterCards(t *testing.T) {
	for name, body := range map[string]string{
		"morning": morningLetterCardHTML,
		"evening": eveningLetterCardHTML,
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

// Structural spot checks: the HTML skeleton must decode into the same node
// shapes the legacy JSON produced (list items as text nodes, badge value from
// inner text, stat attrs) so the three client renderers keep working unchanged.
func TestParseHTML_MorningLetterShape(t *testing.T) {
	root := mustParseHTML(t, morningLetterCardHTML)
	if root["type"] != "column" {
		t.Fatalf("root = %v", root["type"])
	}
	children, _ := root["children"].([]any)
	if len(children) != 8 {
		t.Fatalf("want masthead(3) + 5 cards, got %d", len(children))
	}
	sched := children[5].(map[string]any)
	list := sched["children"].([]any)[1].(map[string]any)
	if list["type"] != "list" {
		t.Fatalf("list = %v", list)
	}
	items := list["items"].([]any)
	if len(items) != 2 || items[0].(map[string]any)["value"] != "09:00 — 팀 스탠드업" {
		t.Errorf("items = %v", items)
	}
	deadline := children[7].(map[string]any)
	badgeRow := deadline["children"].([]any)[1].(map[string]any)
	badge := badgeRow["children"].([]any)[1].(map[string]any)
	if badge["type"] != "badge" || badge["value"] != "D-2" {
		t.Errorf("badge = %v", badge)
	}
	// The urgency tint contract rides the color attr — assert the parser
	// preserves it, not just that the template validates (review catch).
	if badge["color"] != "warning" {
		t.Errorf("badge color = %v, want warning", badge["color"])
	}
	// Overdue rows carry the error tint (skeleton teaches by example — the
	// 2026-07-07 live letter left 기한 초과 rows untinted).
	overdueRow := deadline["children"].([]any)[2].(map[string]any)
	overdue := overdueRow["children"].([]any)[1].(map[string]any)
	if overdue["value"] != "기한 초과" || overdue["color"] != "error" {
		t.Errorf("overdue badge = %v, want 기한 초과/error", overdue)
	}
}

// The delivered message is a plain head line followed by the deneb-ui fence.
// ExtractFences must recover exactly the card body, and it must validate — this
// guards the real on-the-wire shape the morning-letter skill emits.
func TestValidateParsesAndValidatesLetterMessage(t *testing.T) {
	msg := "좋은 아침이에요 — 6월 28일 토요일. 오후 소나기 · 부가세 신고 D-2\n\n" +
		"```deneb-ui\n" + morningLetterCardHTML + "\n```\n"
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

// Legacy JSON letter card (the pre-2026-07 wire format) must keep validating —
// old transcripts render through the same schema.
func TestValidateWithLegacyJSONCard(t *testing.T) {
	legacy := `{"type":"column","children":[
	  {"type":"card","children":[
	    {"type":"row","children":[
	      {"type":"icon","name":"alarm","size":16},
	      {"type":"text","value":"임박 마감","style":"caption"}]},
	    {"type":"row","children":[
	      {"type":"text","value":"부가세 신고","style":"body"},
	      {"type":"badge","value":"D-2"}]}]}]}`
	issues, err := Validate(legacy)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(issues) != 0 {
		t.Errorf("legacy JSON card must stay valid, issues: %v", issues)
	}
}

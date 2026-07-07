package tools

import (
	"strings"
	"testing"

	// Test-only import: the prod layering rule (tools depends only on leaf
	// packages) holds for the binary; here the whole point is to gate the
	// server-assembled card against the real deneb-ui validator, exactly like
	// letter_card_test.go gates the letter skeletons.
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/denebui"
)

func weeklyCardFixture() weeklyEnvelope {
	return weeklyEnvelope{
		Office:      "기획조정실",
		Reporter:    "오선택 실장",
		WeekDone:    "26.06.29~26.07.05",
		WeekPlanned: "26.07.06~26.07.12",
		Groups: []weeklyGroup{
			{Sogan: "1팀", Label: "사업개발 (1팀)", Projects: []weeklyProject{
				{Title: "비금도 154kV", Capacity: "2.5MW", DoneLine: "케이블 발주 완료", PlannedLine: "L/C Draft 수신 확인"},
				{Title: "강진 신다산", DoneLine: "EPC V3 날인", PlannedLine: "법무 회신 반영"},
			}},
			{Sogan: "남도에코", Label: "남도에코에너지 (전선·케이블)", Projects: []weeklyProject{
				{Title: "JOCA 케이블", DoneLine: "300mm² 출하", PlannedLine: "150mm² 출하 대기"},
			}},
		},
		IssueRows: []weeklyIssueRow{
			{Title: "부가세 신고", Badge: "D-2", Overdue: false},
			{Title: "진코 선입금 상계", Badge: "기한 초과", Overdue: true},
		},
	}
}

func TestComposeWeeklyCard_ValidatesAndShapes(t *testing.T) {
	msg := composeWeeklyCard(weeklyCardFixture())

	if !strings.HasPrefix(msg, "📋 주간업무보고 — ") {
		t.Fatalf("head line missing: %q", msg[:40])
	}
	fences := denebui.ExtractFences(msg)
	if len(fences) != 1 {
		t.Fatalf("expected exactly 1 deneb-ui fence, got %d", len(fences))
	}
	issues, err := denebui.Validate(fences[0])
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("card must be schema-valid, got issues: %v", issues)
	}

	card := fences[0]
	for _, want := range []string{
		`<stat value="3건" label="진행 프로젝트"/>`,
		`<stat value="2건" label="현안"/>`,
		`<accordion title="사업개발 (1팀) · 2건">`,
		"**비금도 154kV(2.5MW)**",
		`<badge color="warning">D-2</badge>`,
		`<badge color="error">기한 초과</badge>`,
	} {
		if !strings.Contains(card, want) {
			t.Errorf("card missing %q", want)
		}
	}
}

func TestComposeWeeklyCard_EmptyWeekStillValidates(t *testing.T) {
	msg := composeWeeklyCard(weeklyEnvelope{
		Office: "기획조정실", Reporter: "오선택 실장",
		WeekDone: "26.06.29~26.07.05", WeekPlanned: "26.07.06~26.07.12",
	})
	fences := denebui.ExtractFences(msg)
	if len(fences) != 1 {
		t.Fatalf("fences = %d, want 1", len(fences))
	}
	issues, err := denebui.Validate(fences[0])
	if err != nil || len(issues) != 0 {
		t.Fatalf("empty-week card invalid: err=%v issues=%v", err, issues)
	}
	if !strings.Contains(fences[0], "보고할 프로젝트 활동이 없습니다") {
		t.Error("empty-week body missing")
	}
}

func TestComposeWeeklyCard_EscapesHostileText(t *testing.T) {
	env := weeklyCardFixture()
	// Backticks in a report line must not terminate the outer fence; angle
	// brackets and quotes must not corrupt markup or attributes.
	env.Groups[0].Projects[0].DoneLine = "코드 ```block``` 삽입 & <tag> 시도"
	env.Groups[0].Label = `사업"개발" <1팀>`
	msg := composeWeeklyCard(env)

	fences := denebui.ExtractFences(msg)
	if len(fences) != 1 {
		t.Fatalf("hostile text split the fence: %d fences", len(fences))
	}
	issues, err := denebui.Validate(fences[0])
	if err != nil {
		t.Fatalf("parse error on hostile text: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("hostile-text card invalid: %v", issues)
	}
}

package wiki

import "testing"

func TestDiaryHasProjectDemandAndSignal(t *testing.T) {
	if !diaryHasProjectDemand("화웨이 330KTL 프로젝트단가 MOQ 300대") {
		t.Fatal("MOQ + 단가 must be demand")
	}
	if !diaryHasProjectDemand("pl2-kia-epc-001 회신 기한 월요일") {
		t.Fatal("project code must be demand")
	}
	if diaryHasProjectDemand("오늘 날씨랑 유튜브 대담 요약") {
		t.Fatal("browse/weather must not be demand")
	}
	if !diaryHasProjectSignal("광주 캐노피 재견적 공급가액") {
		t.Fatal("재견적 must signal")
	}
	if diaryHasProjectSignal("당진 프로젝트 진행상황 알려줘") {
		t.Fatal("bare 프로젝트 근황 must not accelerate dreaming")
	}
}

func TestContainsProjectCode(t *testing.T) {
	if !containsProjectCode("관련 [[프로젝트/pl2-kia-epc-001]]") {
		t.Fatal("want code hit")
	}
	if containsProjectCode("please look at the plot") {
		t.Fatal("pl0- substring alone is not a code")
	}
}

func TestCritiqueNeeded(t *testing.T) {
	if critiqueNeeded(nil) {
		t.Fatal("empty batch")
	}
	if critiqueNeeded([]wikiUpdate{{Action: "update", Path: "프로젝트/x/대표.md"}}) {
		t.Fatal("single project update must skip the critic")
	}
	if !critiqueNeeded([]wikiUpdate{{Action: "update", Path: "기타/이란.md"}}) {
		t.Fatal("기타 must critique even at 1")
	}
	if !critiqueNeeded([]wikiUpdate{{Action: "create", Path: "업무/foo.md"}}) {
		t.Fatal("create must critique even at 1")
	}
	three := []wikiUpdate{{Path: "a"}, {Path: "b"}, {Path: "c"}}
	if !critiqueNeeded(three) {
		t.Fatal("3+ always critique")
	}
}

func TestApplyDemandGate(t *testing.T) {
	input := "기아 광주 재견적 공급가액 858백만 pl2-kia-epc-002"
	misc := []wikiUpdate{{Path: "기타/이란-미국.md", Title: "이란"}}
	kept, dropped := applyDemandGate(input, misc)
	if dropped != 1 || len(kept) != 0 {
		t.Fatalf("기타-only under demand: kept=%d dropped=%d", len(kept), dropped)
	}

	mixed := []wikiUpdate{
		{Path: "기타/이란.md"},
		{Path: "프로젝트/pl2-kia-epc-002/대표.md"},
	}
	kept, dropped = applyDemandGate(input, mixed)
	if dropped != 0 || len(kept) != 2 {
		t.Fatalf("project present must keep all: kept=%d dropped=%d", len(kept), dropped)
	}

	sys := []wikiUpdate{{Path: "시스템/기능-개선.md"}}
	kept, dropped = applyDemandGate(input, sys)
	if dropped != 0 || len(kept) != 1 {
		t.Fatalf("non-기타 without project stays: kept=%d dropped=%d", len(kept), dropped)
	}

	kept, dropped = applyDemandGate("유튜브 대담 요약", misc)
	if dropped != 0 || len(kept) != 1 {
		t.Fatalf("no demand: keep 기타, dropped=%d", dropped)
	}
}

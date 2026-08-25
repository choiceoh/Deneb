package wiki

import "testing"

// The divergent-signal cases are real ledger rows from ~/.deneb/wiki as of the
// 2026-08-25 audit: each key had accumulated a First date and Count from one
// signal, then came back attached to unrelated content and inherited that
// history — asserting a months-old recurring pattern for a first-ever
// observation.
func TestMergeThemeRowsGivesDivergentSignalItsOwnRow(t *testing.T) {
	rows := []themeRow{{
		Key: "srv2-vllm-endpoint-setup", Signal: "srv2 vLLM 엔드포인트 설정 작업이 반복됨",
		First: "2026-07-25", Last: "2026-08-01", Count: 4, Status: "활성",
	}}
	got := mergeThemeRows(rows, []ThemeSignal{{
		Key:    "srv2-vllm-endpoint-setup",
		Signal: "현대차 출고센터 EPC 텀시트가 모비스와 동일 — 발전량보증·PR보증 조건만 별도 검토",
	}}, "2026-08-25")

	if len(got) != 2 {
		t.Fatalf("row count = %d, want 2 (original preserved + divergent split off)", len(got))
	}
	byKey := map[string]themeRow{}
	for _, r := range got {
		byKey[r.Key] = r
	}
	orig, ok := byKey["srv2-vllm-endpoint-setup"]
	if !ok {
		t.Fatal("original row was consumed; its earned history must survive")
	}
	if orig.Signal != "srv2 vLLM 엔드포인트 설정 작업이 반복됨" {
		t.Fatalf("original signal overwritten: %q", orig.Signal)
	}
	if orig.Count != 4 || orig.First != "2026-07-25" {
		t.Fatalf("original history altered: count=%d first=%s", orig.Count, orig.First)
	}
	split, ok := byKey["srv2-vllm-endpoint-setup-2"]
	if !ok {
		t.Fatalf("divergent signal got no row of its own; keys=%v", byKey)
	}
	if split.Count != 1 || split.First != "2026-08-25" {
		t.Fatalf("divergent row inherited history: count=%d first=%s", split.Count, split.First)
	}
}

// Rewording across cycles is the intended behaviour and must still merge —
// otherwise the ledger fragments into near-duplicate rows and never reaches 정착.
func TestMergeThemeRowsStillMergesRewordedSignal(t *testing.T) {
	rows := []themeRow{{
		Key:    "morning-briefing-collection",
		Signal: "모닝레터 수집 루틴이 매일 진행되며 날씨·환율·동 가격·일정·프로젝트·메일 요약이 반복됨",
		First:  "2026-07-24", Last: "2026-08-24", Count: 18, Status: "활성",
	}}
	got := mergeThemeRows(rows, []ThemeSignal{{
		Key:    "morning-briefing-collection",
		Signal: "모닝레터 수집 루틴이 매일 진행되며 날씨·환율·동 가격·일정·프로젝트·메일·임박 마감 요약이 반복된다",
	}}, "2026-08-25")

	if len(got) != 1 {
		t.Fatalf("row count = %d, want 1 (reword must merge, not split): %+v", len(got), got)
	}
	if got[0].Count != 19 {
		t.Fatalf("count = %d, want 19 (new observation day)", got[0].Count)
	}
	if got[0].First != "2026-07-24" {
		t.Fatalf("first = %s, want 2026-07-24 preserved", got[0].First)
	}
}

// Same signal seen twice in one day stays one observation — the split must not
// break same-day idempotency.
func TestMergeThemeRowsSameDayRerunIsIdempotent(t *testing.T) {
	sig := ThemeSignal{Key: "epc-contract-finalization", Signal: "EPC/O&M 계약 체결 후 선급금·기자재 발주 단계로 진행"}
	rows := mergeThemeRows(nil, []ThemeSignal{sig}, "2026-08-25")
	rows = mergeThemeRows(rows, []ThemeSignal{sig}, "2026-08-25")

	if len(rows) != 1 {
		t.Fatalf("row count = %d, want 1", len(rows))
	}
	if rows[0].Count != 1 {
		t.Fatalf("count = %d, want 1 on same-day re-run", rows[0].Count)
	}
}

func TestSameThemeSignalSeparatesRewordFromTopicSwap(t *testing.T) {
	tests := []struct {
		name     string
		stored   string
		incoming string
		want     bool
	}{{
		name:     "inflection change",
		stored:   "주간 분석/이슈 문서 작성 패턴 반복됨",
		incoming: "주간 분석/이슈 문서 작성 패턴이 반복된다",
		want:     true,
	}, {
		name:     "detail appended",
		stored:   "기아 광주 캐노피 견적 재요청·조정 반복",
		incoming: "기아 광주 캐노피 견적 재요청·조정 반복, 통합견적 회신 대기",
		want:     true,
	}, {
		name:     "topic swap: infra key onto EPC content",
		stored:   "srv2 vLLM 엔드포인트 설정 작업이 반복됨",
		incoming: "현대차 출고센터 EPC 텀시트가 모비스와 동일 — 발전량보증·PR보증 조건만 별도 검토",
		want:     false,
	}, {
		name:     "topic swap: dashboard key onto module bypass",
		stored:   "태양광 모니터링 대시보드 위젯 개발이 반복 요청됨",
		incoming: "SKN 이천 모듈 바이패스 사후 조치 추적",
		want:     false,
	}, {
		name:     "topic swap: vendor payment key onto SaaS subscription",
		stored:   "가온전자 대금 지급 지연이 반복 발생",
		incoming: "결제 실패로 인한 인프라 구독 중단 위험 지속",
		want:     false,
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sameThemeSignal(tt.stored, tt.incoming); got != tt.want {
				t.Fatalf("sameThemeSignal(%q, %q) = %v, want %v", tt.stored, tt.incoming, got, tt.want)
			}
		})
	}
}

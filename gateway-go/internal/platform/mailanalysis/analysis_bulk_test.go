package mailanalysis

import (
	"errors"
	"testing"
)

// Opening lines are verbatim from ~/.deneb/wiki as of the 2026-08-25 audit.
func TestAnalysisNonBusinessRejectsSelfDeclaredBulk(t *testing.T) {
	bulk := []string{
		"Cursor 구독 결제 실패 알림입니다. 업무 메일이 아닙니다.",
		"TLDR 테크 뉴스레터(7/20호) — 업무와 무관한 자동 발송 메일입니다.",
		"광고 메일 — DeepL Voice 14일 무료 체험 안내(외국어 번역 서비스).",
		"법무법인 율촌의 정기 뉴스레터예요. 상법 시행령 개정안 공포 안내인데 즉시 조치할 사안은 아닙니다.",
		"GitKraken(개발용 Git GUI 툴)이 보낸 자동 마케팅 메일입니다.",
		"Plaud 자동 전사 알림입니다. 업무 메일이 아니라 짧은 음성 메모의 자동 전사 결과입니다.",
		"Supernote 고객지원팀이 보낸 **기기 소프트웨어 업데이트 안내 메일**입니다. 업무와 무관한 개인 기기 관련 참고 메일입니다.",
		"**참고 — 산군 인사이트 뉴스레터(건설업계 동향)**",
	}
	for _, body := range bulk {
		if err := AnalysisNonBusiness(body); err == nil {
			t.Errorf("AnalysisNonBusiness(%q) = nil, want bulk", body)
		} else if !errors.Is(err, ErrAnalysisBulkMail) {
			t.Errorf("wrong error for %q: %v", body, err)
		}
	}
}

// The contrast case is the one that matters: an analysis of a REAL mail that
// mentions the day's junk in passing. Deleting this page would have thrown away
// a GM-level price-negotiation escalation.
func TestAnalysisNonBusinessKeepsBusinessMail(t *testing.T) {
	business := []string{
		"오늘 메일 24건 중 광고·테스트성(율촌 뉴스레터, 삼일PwC, Signal 알림, 테스트 메일 등)을 제외하고, 이 건은 진코솔라 본사 GM급이 직접 가격 협상 교착을 뚫으려는 에스컬레이션이라 깊이 분석했어요.",
		"기아 광주 캐노피 통합견적 회신 요청입니다. 8/24까지 제출해야 합니다.",
		"당진 솔라빌리지 EPC 계약금액이 1,041억에서 1,042.78억으로 갱신됐습니다.",
		// A later paragraph may legitimately discuss advertising spend; only the
		// opening line is consulted, so this must survive.
		"현대차 출고센터 EPC 텀시트 검토 요청입니다.\n\n광고비 정산 항목은 별도 계약으로 처리하기로 했습니다.",
		// Verdict word present but buried past the window in a business sentence.
		"발주처가 요구한 사양 변경 내역을 정리했고, 회신 기한은 금요일이며 홍보 메일과는 무관한 정식 공문입니다.",
	}
	for _, body := range business {
		if err := AnalysisNonBusiness(body); err != nil {
			t.Errorf("AnalysisNonBusiness(%q) = %v, want nil", body, err)
		}
	}
}

// An empty or narration-only body is AnalysisUsable's problem, not this one —
// the two gates must not both claim the same page.
func TestAnalysisNonBusinessIgnoresEmptyBody(t *testing.T) {
	for _, body := range []string{"", "   ", "### 분석 결과\n"} {
		if err := AnalysisNonBusiness(body); err != nil {
			t.Errorf("AnalysisNonBusiness(%q) = %v, want nil", body, err)
		}
	}
}

package proactive

import "testing"

func TestIsNarrationOnlyProactive(t *testing.T) {
	suppress := []string{
		// Whole-body working-narration / self-talk (observed leaks).
		"이제 분석 보고를 정리해.",
		"다 모였어. 모닝레터 작성할게.",
		"gmail 도구를 활성화하고 가장 최근 메일을 읽어오겠습니다.",
		"이제 위키에서 관련 프로젝트 맥락을 확인해볼게요.",
		"오늘 모닝레터 준비 완료했습니다. 주요 맥락을 반영해 작성했어요.",
		// Next-step narration citing mail INDEX positions — the "9, 10번" digits are
		// not factual tells, so the meta signal ("파악됐습니다") must still win. This is
		// the exact leak the ordinal-strip fixes (Huawei ESS 문의 card).
		"이제 전체 회의 내용이 파악됐습니다. 마지막으로 메일 9, 10번(Huawei ESS 문의)을 확인합니다.",
		"위키 맥락 확보 완료. 이제 메일 3번을 읽어올게요.",
		// Stray markup / tool fragments.
		"<pre>",
		"<tool>",
		"</thinking>",
		// Bare generic labels.
		"분석",
		"메일 분석",
		"이메일 분석",
		"업무 리포트",
		"",
	}
	for _, s := range suppress {
		if !isNarrationOnlyProactive(s) {
			t.Errorf("isNarrationOnlyProactive(%q) = false, want true (should suppress)", s)
		}
	}

	keep := []string{
		// Real reports — markdown structure.
		"## 📬 메일 분석 리포트\n\n### 무림피앤피 울산공장 과업지시서 송부\n**🟡 확인 필요**",
		"📊 6/11(목) 메일 종합 분석\n🔴 긴급 — 인하공전 계약이행보증증권 발행 요청",
		// Short note carrying a factual tell (a date/number).
		"현대차 가견적서 6/25 방문 확정",
		// Meta signal present, but a real factual tell that is NOT a "번" index
		// (an amount) survives the ordinal strip and keeps the card — the fix must
		// not swallow a thin-but-substantive brief just because it opens with 완료.
		"분석 완료. 총 계약금 3억, 지체상금 조항 확인 필요.",
		// Phone/location proactive note (no narration signal).
		"출근 확인 — 본사 WiFi 접속 감지됨.",
		"집에 들어오셨네요. 🏠",
		"회사 도착 확인 — 13:00 JA 이용원 상무 미팅 시작입니다.",
	}
	for _, s := range keep {
		if isNarrationOnlyProactive(s) {
			t.Errorf("isNarrationOnlyProactive(%q) = true, want false (should keep)", s)
		}
	}
}

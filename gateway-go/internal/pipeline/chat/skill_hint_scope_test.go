package chat

import "testing"

// Every fixture below is a real production message shape (2026-08 transcripts).
func TestSkillTriggerScope_DropsMachineInsertedPayload(t *testing.T) {
	for _, tc := range []struct{ name, msg, want string }{
		{"첨부 목록", "📎 첨부 파일을 저장했습니다. 아래 목록의 계약서를 읽어라", ""},
		{"공유 문서", "📄 공유 문서에서 추출한 텍스트 (부록8-1-4 EPC 계약서): 제1조 …", ""},
		{"OCR", "📷 공유 이미지에서 추출한 텍스트 (OCR): 공급계약 조건 …", ""},
		{"녹음", "🎙️ 공유 녹음을 받아썼습니다 (화자분리·타임스탬프). 계약서 얘기 …", ""},
		{"작업 영역", "[작업 영역 — 현재 내용] [메일 60건] - ● 계약서 송부의 건 …", ""},
		{"thinking", "[thinking] 데이터 수집 완료. 계약서 20건 정리 …", ""},
		{"업무 리포트", "이 업무 리포트를 바탕으로 지금 바로 처리할 다음 계약 검토 …", ""},
		{"전사", "00:00:05 Speaker 1  계약서 관련해서 보고드리겠습니다.", ""},

		// The operator's own words survive — the share flow puts them first.
		{
			"공유 맥락 유지", "📲 공유 맥락:\n이 계약서 검토해줘\n\n📄 공유 문서에서 추출한 텍스트 (x.docx): …",
			"📲 공유 맥락:\n이 계약서 검토해줘",
		},
		{"직접 발화", "새만금 공사계약서 갖고있나", "새만금 공사계약서 갖고있나"},
		{"발화 뒤 첨부", "이 계약서 좀 봐줘\n\n📎 첨부 파일을 저장했습니다.", "이 계약서 좀 봐줘"},
	} {
		if got := skillTriggerScope(tc.msg); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

// The scope must not silence a genuine ask that merely mentions an attachment.
func TestSkillTriggerScope_KeepsPlainMessages(t *testing.T) {
	for _, msg := range []string{
		"팩트체크 해줘: 지구에서 달까지 평균 거리가 38만 km 맞아?",
		"지식 인터뷰: '핵심 고객 프로파일' 도메인을 인터뷰로 정리하자.",
		"매출 카드로 정리",
	} {
		if got := skillTriggerScope(msg); got != msg {
			t.Errorf("일반 발화가 잘림: %q → %q", msg, got)
		}
	}
}

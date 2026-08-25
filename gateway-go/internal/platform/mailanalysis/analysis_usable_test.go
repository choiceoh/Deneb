package mailanalysis

import (
	"errors"
	"testing"
)

// Fixtures are verbatim analysis bodies from the 2026-08-25 corpus audit: the
// rejects are pages that actually reached ~/.deneb/wiki, the accepts are real
// analyses from the same corpus. Keeping both halves real is the point — the
// accept cases are what stop this guard from eating short-but-valid reports
// (a one-line newsletter dismissal is legitimate and must survive).
func TestAnalysisUsableRejectsDegradedBodies(t *testing.T) {
	tests := []struct {
		name string
		body string
		want error
	}{{
		name: "wiki update announcement",
		body: "맥락 확보 완료. 위키 로그에 오늘 진행 사실을 먼저 기록하고 보고드릴게요.",
		want: ErrAnalysisNarration,
	}, {
		name: "context read announcement",
		body: "핵심 페이지를 열어 맥락을 보강할게요.",
		want: ErrAnalysisNarration,
	}, {
		name: "precondition plus intent",
		body: "맥락 확인 완료. 7/21 수정 송부에 이은 후속이네요. 위키에 진행 변화부터 반영하고 보고드릴게요.",
		want: ErrAnalysisNarration,
	}, {
		name: "finding promised but not delivered",
		body: "PO 첨부에서 중요한 사실을 확인했어요. 위키 갱신 후 보고드릴게요.",
		want: ErrAnalysisNarration,
	}, {
		name: "page read announcement",
		body: "맥락이 확인됐습니다. 남도에코 기본 페이지를 읽고 반기 실적을 갱신하겠습니다.",
		want: ErrAnalysisNarration,
	}, {
		name: "analysis promised",
		body: "맥락 확인 끝났어요. 위키에 먼저 기록하고 분석을 정리할게요.",
		want: ErrAnalysisNarration,
	}, {
		name: "tool failure chatter",
		body: "파일은 맞는데 edit 도구가 워크스페이스 밖 경로를 못 잡네요. 쉘로 직접 반영합니다.",
		want: ErrAnalysisNarration,
	}, {
		// Intent to inspect source material, with no wiki/맥락 keyword at all —
		// only the volitional "…열어볼게요" marks it.
		name: "intent to open the attachment",
		body: "새 견적서 PDF 2건(N16-260723-01/02)이 첨부돼 있네요. 실제 견적 내용을 열어볼게요.",
		want: ErrAnalysisNarration,
	}, {
		// The attachment trailer is appended after synthesis, so it must not
		// rescue a narration stub by inflating the body length.
		name: "narration rescued by attachment trailer",
		body: "위키 맥락 확인 완료 — 핵심 변화(계약금액 구조·착공예정일)를 프로젝트 로그와 대표 페이지에 먼저 기록합니다.\n\n" +
			"📎 분량이 커 일부만 반영된 첨부: [해봄에너지] 공사도급계약서(12711241.v8)_Clean_HRE v3.docx — 전체가 필요하면 채팅에서 원본을 열어 확인하세요.",
		want: ErrAnalysisNarration,
	}, {
		name: "heading with nothing under it",
		body: "### 분석 결과",
		want: ErrAnalysisEmpty,
	}, {
		name: "empty",
		body: "   \n\n  ",
		want: ErrAnalysisEmpty,
	}, {
		name: "timeout error as body",
		body: "응답 생성이 시간 초과로 중단됐어요. 잠시 후에 다시 시도해 주세요.\n\n" +
			"📎 분량이 커 일부만 반영된 첨부: 해봄에너지 예정공정표.xlsx — 전체가 필요하면 채팅에서 원본을 열어 확인하세요.",
		want: ErrAnalysisErrorText,
	}, {
		name: "api refusal as body",
		body: "The request was rejected because it was considered high risk",
		want: ErrAnalysisErrorText,
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := AnalysisUsable(tt.body)
			if err == nil {
				t.Fatalf("AnalysisUsable() = nil, want %v", tt.want)
			}
			if !errors.Is(err, tt.want) {
				t.Fatalf("AnalysisUsable() = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestAnalysisUsableAcceptsRealReports(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{{
		// The shortest legitimate shape in the corpus: a newsletter dismissal.
		// It talks about the MAIL, not about the assistant's next action.
		name: "newsletter dismissal one-liner",
		body: "TLDR AI 뉴스레터(AI 업계 소식 요약 구독지) — 업무와 무관한 자동 발신 메일이므로 분석 생략. AI 트렌드 구독용으로 받아두신 것이라면 유지, 필요 없으면 수신거부 대상입니다.",
	}, {
		name: "short marketing dismissal",
		body: "광고 메일 — DeepL Voice 14일 무료 체험 안내(외국어 번역 SaaS 프로모션). 업무 무관, 조치 필요 없음.",
	}, {
		name: "payroll notice",
		body: "참고 — 7월 급여명세서 통지. 업무 분석 불필요.\n\n김성환(1팀 대리)이 그룹웨어 링크로 7월분 급여명세서를 보냈습니다.",
	}, {
		// The attachment footer addresses the READER in the imperative. It must
		// not read as the model's own intent to go look at something.
		name: "short report carrying the attachment footer",
		body: "참고 — Supernote 고객지원팀이 보낸 기기 소프트웨어 업데이트 안내 메일입니다. 업무와 무관한 개인 기기 관련 참고 메일입니다.\n\n" +
			"📎 분량이 커 일부만 반영된 첨부: manual.pdf — 전체가 필요하면 채팅에서 원본을 열어 확인하세요.",
	}, {
		// A real report that CLOSES with a past-tense wiki note — the narration
		// rule must not fire on it.
		name: "report ending with past-tense wiki note",
		body: "**확인** — 진영상사 조범석 대표가 6/12 발주된 고흥 해밀 솔라케이블(₩15.73억)의 선급금 미입금과 " +
			"계약서 미서명 상태를 알리고 있습니다. 현장은 다음주 월요일(6/22) 초도 납품을 요청했지만, 자금과 서류가 없으면 " +
			"출고가 안 되는 상황입니다.\n\n위키 업데이트 완료했습니다.",
	}, {
		// Opens with a context line but then delivers the analysis: length keeps
		// it out of the narration window.
		name: "report opening with context line",
		body: "맥락 확인 완료. 이 메일은 이시연 주임이 발신한 우리 측 메일의 사본이며, 7/30 LC 초본 검토 단계 이후 " +
			"LC 개설 완료 및 납기 앞당김 요청이라는 중요 진전입니다.\n\n## 분석\n\n7/29 광주은행 평동공단센터에 LC 개설을 " +
			"공식 요청한 이후 약 5일 만에 LC 개설이 완료되었고, 이시연 주임이 개설 신청 서류를 Mia에게 전달했습니다. " +
			"케이블 LC 금액은 $1,380,120입니다. MC4 커넥터 $182,250는 여전히 별도 PI·LC 대기 상태입니다.",
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := AnalysisUsable(tt.body); err != nil {
				t.Fatalf("AnalysisUsable() = %v, want nil", err)
			}
		})
	}
}

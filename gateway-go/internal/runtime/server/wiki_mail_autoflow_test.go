package server

import (
	"errors"
	"testing"
)

type fakeMeetingLister struct {
	pages []string
	err   error
	calls int
}

func (f *fakeMeetingLister) ListPages(string) ([]string, error) {
	f.calls++
	return f.pages, f.err
}

// liveMeetingPages are real 회의록 paths from the 2026-08-29 corpus. The
// cross-project pairs are the point: the mail analyzer filed these mails under a
// DIFFERENT project than the meeting service chose, so a project-scoped check
// would report them uncovered.
var liveMeetingPages = []string{
	"프로젝트/pl1-gsn-dev-001/회의록/07-10-회의-새만금-태양광-시공-기자재-입찰-및-지체상금-협의-ac3a3dd2.md",
	"프로젝트/pl1-gsn-dev-001/회의록/08-24-주간-회의-태양광-ess-프로젝트-인허가-계약-자재-조달-이슈-49ce2403.md",
	"프로젝트/회의록/07-08-회의-영농형-태양광-사업-인허가-보상-epc-추진-협의-2a8db606.md",
	"프로젝트/nde-ztt-cbl-001/대표.md",
	"프로젝트/nde-ztt-cbl-001/메일분석/abc123.md",
}

func TestAutoFlowMeetingCovered(t *testing.T) {
	tests := []struct {
		name    string
		from    string
		subject string
		want    string
	}{{
		name:    "회의록이 있으면 덮인 것으로 본다",
		from:    "Plaud <no-reply@plaud.ai>",
		subject: "[Plaud-AutoFlow] 07-10 회의: 새만금 태양광 시공·기자재 입찰 및 지체상금 협의",
		want:    "프로젝트/pl1-gsn-dev-001/회의록/07-10-회의-새만금-태양광-시공-기자재-입찰-및-지체상금-협의-ac3a3dd2.md",
	}, {
		// The live mail for this meeting is filed under com-sds-epc-001 while the
		// 회의록 sits under pl1-gsn-dev-001. Project scoping would miss it.
		name:    "프로젝트가 달라도 제목 슬러그로 찾는다",
		from:    "no-reply@plaud.ai",
		subject: "[Plaud-AutoFlow] 08-24 주간 회의: 태양광·ESS 프로젝트 인허가, 계약, 자재 조달 이슈",
		want:    "프로젝트/pl1-gsn-dev-001/회의록/08-24-주간-회의-태양광-ess-프로젝트-인허가-계약-자재-조달-이슈-49ce2403.md",
	}, {
		name:    "카테고리 버킷의 회의록도 센다",
		from:    "no-reply@plaud.ai",
		subject: "[Plaud-AutoFlow] 07-08 회의: 영농형 태양광 사업 인허가·보상·EPC 추진 협의",
		want:    "프로젝트/회의록/07-08-회의-영농형-태양광-사업-인허가-보상-epc-추진-협의-2a8db606.md",
	}, {
		// The auth-fallback case: MCP never ran, so the mail page is the only
		// record this meeting will get.
		name:    "회의록이 없으면 폴백이 살아 기록한다",
		from:    "no-reply@plaud.ai",
		subject: "[Plaud-AutoFlow] 07-27 주간 회의: 태양광·해저케이블·도급계약 현안",
		want:    "",
	}, {
		name:    "일반 업무 메일은 무영향",
		from:    "kim@topsolar.kr",
		subject: "07-10 회의: 새만금 태양광 시공·기자재 입찰 및 지체상금 협의",
		want:    "",
	}, {
		name:    "plaud.ai의 다른 메일은 무영향",
		from:    "no-reply@plaud.ai",
		subject: "결제 영수증 안내",
		want:    "",
	}, {
		name:    "전달된 메일은 발신자가 달라 무영향",
		from:    "boss@topsolar.kr",
		subject: "[Plaud-AutoFlow] 07-10 회의: 새만금 태양광 시공·기자재 입찰 및 지체상금 협의",
		want:    "",
	}, {
		name:    "제목이 접두사뿐이면 슬러그가 없어 기록한다",
		from:    "no-reply@plaud.ai",
		subject: "[Plaud-AutoFlow]",
		want:    "",
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeMeetingLister{pages: liveMeetingPages}
			if got := autoFlowMeetingCovered(store, tc.from, tc.subject); got != tc.want {
				t.Fatalf("autoFlowMeetingCovered() = %q, want %q", got, tc.want)
			}
		})
	}
}

// A store that cannot answer must not be read as "covered": the whole point of
// the gate is to drop a DUPLICATE, and a listing error is not evidence of one.
func TestAutoFlowMeetingCoveredWritesOnListError(t *testing.T) {
	store := &fakeMeetingLister{err: errors.New("boom")}
	got := autoFlowMeetingCovered(store, "no-reply@plaud.ai",
		"[Plaud-AutoFlow] 07-10 회의: 새만금 태양광 시공·기자재 입찰 및 지체상금 협의")
	if got != "" {
		t.Fatalf("listing error must not suppress the page, got %q", got)
	}
}

// Non-AutoFlow mail is the overwhelming majority; it must not pay for a full
// page listing on every analysis.
func TestAutoFlowMeetingCoveredSkipsListingForOrdinaryMail(t *testing.T) {
	store := &fakeMeetingLister{pages: liveMeetingPages}
	if got := autoFlowMeetingCovered(store, "kim@topsolar.kr", "견적 회신"); got != "" {
		t.Fatalf("want no coverage, got %q", got)
	}
	if store.calls != 0 {
		t.Fatalf("ordinary mail listed wiki pages %d times, want 0", store.calls)
	}
}

func TestAutoFlowMeetingCoveredNilStore(t *testing.T) {
	if got := autoFlowMeetingCovered(nil, "no-reply@plaud.ai", "[Plaud-AutoFlow] 07-10 회의: 테스트"); got != "" {
		t.Fatalf("want %q, got %q", "", got)
	}
}

// The suffix rule must not let one meeting's slug swallow a longer, different
// meeting whose title starts the same way.
func TestAutoFlowMeetingCoveredRejectsPrefixOfLongerMeeting(t *testing.T) {
	store := &fakeMeetingLister{pages: []string{
		"프로젝트/p/회의록/08-10-회의-태양광-공사-인허가-지연-및-비용-협의-추가-안건-1ee7f203.md",
	}}
	got := autoFlowMeetingCovered(store, "no-reply@plaud.ai",
		"[Plaud-AutoFlow] 08-10 회의: 태양광 공사 인허가 지연 및 비용 협의")
	if got != "" {
		t.Fatalf("longer meeting must not cover a shorter slug, got %q", got)
	}
}

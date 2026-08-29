package wiki

import "testing"

func TestMeetingSlug(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{
			"한글 회의명", "07-10 회의: 새만금 태양광 시공·기자재 입찰 및 지체상금 협의",
			"07-10-회의-새만금-태양광-시공-기자재-입찰-및-지체상금-협의",
		},
		{
			"영문은 소문자로", "08-24 주간 회의: 태양광·ESS 프로젝트 인허가",
			"08-24-주간-회의-태양광-ess-프로젝트-인허가",
		},
		{"기호만 있으면 빈 슬러그", "!!! ???", ""},
		{"앞뒤 하이픈 제거", "  회의  ", "회의"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := MeetingSlug(tc.in); got != tc.want {
				t.Fatalf("MeetingSlug(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// The bound is what keeps a filename from growing without limit; it counts
// hyphens too, so a long title is cut mid-word by design.
func TestMeetingSlugBounded(t *testing.T) {
	long := "08-21 사업 협의: 새만금 태양광 ESS 구매와 화웨이 유통 전략 그리고 더 긴 제목이 계속 이어지는 경우"
	got := MeetingSlug(long)
	if n := len([]rune(got)); n > meetingSlugRunes {
		t.Fatalf("slug is %d runes, want <= %d: %q", n, meetingSlugRunes, got)
	}
}

func TestMeetingPageCoveringSlug(t *testing.T) {
	pages := []string{
		"프로젝트/pl1-gsn-dev-001/회의록/07-10-회의-새만금-태양광-시공-기자재-입찰-및-지체상금-협의-ac3a3dd2.md",
		"프로젝트/회의록/07-08-회의-영농형-태양광-사업-인허가-보상-epc-추진-협의-2a8db606.md",
		"프로젝트/pl1-gsn-dev-001/회의록/07-10-회의-새만금-시공-기자재-입찰-지체상금.md",
		"프로젝트/nde-ztt-cbl-001/메일분석/abc123.md",
		"프로젝트/nde-ztt-cbl-001/대표.md",
	}
	tests := []struct{ name, slug, want string }{
		{
			"프로젝트 폴더 회의록", "07-10-회의-새만금-태양광-시공-기자재-입찰-및-지체상금-협의",
			"프로젝트/pl1-gsn-dev-001/회의록/07-10-회의-새만금-태양광-시공-기자재-입찰-및-지체상금-협의-ac3a3dd2.md",
		},
		{
			"카테고리 버킷 회의록", "07-08-회의-영농형-태양광-사업-인허가-보상-epc-추진-협의",
			"프로젝트/회의록/07-08-회의-영농형-태양광-사업-인허가-보상-epc-추진-협의-2a8db606.md",
		},
		{
			"id 꼬리 없는 회의록", "07-10-회의-새만금-시공-기자재-입찰-지체상금",
			"프로젝트/pl1-gsn-dev-001/회의록/07-10-회의-새만금-시공-기자재-입찰-지체상금.md",
		},
		{"없으면 빈 문자열", "08-31-회의-없는-것", ""},
		{"빈 슬러그는 아무것도 덮지 않는다", "", ""},
		// The suffix must be the recording-id tail, so a longer meeting whose
		// title merely starts the same way does not cover a shorter one.
		{"더 긴 제목은 덮지 않는다", "07-10-회의-새만금", ""},
		// 메일분석/대표 are not meeting records however their stem reads.
		{"회의록 폴더 밖은 세지 않는다", "abc123", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := MeetingPageCoveringSlug(pages, tc.slug); got != tc.want {
				t.Fatalf("MeetingPageCoveringSlug(%q) = %q, want %q", tc.slug, got, tc.want)
			}
		})
	}
}

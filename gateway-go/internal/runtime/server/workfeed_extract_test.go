package server

import (
	"strings"
	"testing"
)

func TestExtractCardTitle(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{"bold heading line", "**📧 최근 메일 분석 보고**\n---\n본문", "📧 최근 메일 분석 보고"},
		{"atx heading", "## 📧 최신 메일 분석 보고\n**분석 대상**: fred@x", "📧 최신 메일 분석 보고"},
		{"leading hrule skipped", "---\n\n## 📧 JOCA Cable 최신 메일 분석 보고\n발신: x", "📧 JOCA Cable 최신 메일 분석 보고"},
		{"generic heading folds sub-heading", "## 분석\n\n### 왜 지금 왔는가\n본문", "분석 — 왜 지금 왔는가"},
		{"generic title kept when body follows", "## 분석\n\n이 메일은 대한전선 건이다.", "분석"},
		{"emoji prefix with bold", "🐾 **모닝레터 — 2026년 6월 6일(토)**\n내용", "🐾 모닝레터 — 2026년 6월 6일(토)"},
		{"plain first line", "오늘 처리할 업무가 3건 있습니다.\n자세히는 아래.", "오늘 처리할 업무가 3건 있습니다."},
		{"empty", "", ""},
		{"blank only", "\n\n   \n", ""},
		{"markers only", "---\n***\n___", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := extractCardTitle(tc.content)
			if got != tc.want {
				t.Errorf("extractCardTitle = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestExtractCardTitle_MailSubject covers the feed-card rule that a generic
// "메일 분석 리포트/보고" heading (redundant with the 📬 card icon) is replaced by
// the email's actual subject pulled from the body — a 제목 table row, a specific
// sub-heading, or a bold subject line — while report scaffolding (메일 개요, 발신,
// 중요도, …) and batch/daily summaries are left alone.
func TestExtractCardTitle_MailSubject(t *testing.T) {
	cases := []struct {
		name      string
		content   string
		wantHas   string // the title must contain this subject fragment
		wantNotEq string // …and must not be this generic heading
	}{
		{
			"sub-heading subject",
			"## 📬 메일 분석 리포트\n\n### 무림피앤피 울산공장 — 중앙조달(전기공사) 과업지시서+도면 송부\n**🟡 확인 필요**",
			"무림피앤피 울산공장",
			"📬 메일 분석 리포트",
		},
		{
			"bold-line subject (table has no 제목 row)",
			"## 📬 메일 분석 리포트\n\n**📧 무림P&P 울산공장 물품 제안요청서 및 규격서 송부 (revised)**\n\n| 항목 | 내용 |\n|---|---|\n| **발신** | 김대희 |",
			"무림P&P 울산공장 물품 제안요청서",
			"📬 메일 분석 리포트",
		},
		{
			"제목 table row (newsletter, unescapes \\_)",
			"📬 **새 메일 분석 리포트**\n\n## 메일 개요\n\n| 항목 | 내용 |\n|---|---|\n| **발신** | 성창석 |\n| **제목** | Korean Tax Update\\_Samil Commentary June 2026 |\n| **시간** | 13:39 |",
			"Korean Tax Update", // clamped to ≤20 runes (was the full subject)
			"새 메일 분석 리포트",
		},
		{
			"skips 메일 개요 / 발신 scaffolding to reach subject",
			"## 📬 메일 분석 리포트\n\n### 수신 메일 개요\n\n**발신**: 김대희\n\n### 고흥 해밀 솔라케이블 발주 — 진영상사 (🟡 확인필요)",
			"고흥 해밀 솔라케이블",
			"📬 메일 분석 리포트",
		},
		{
			"skips 기본 정보 / 발신자 / 프로젝트 맥락 sections to reach subject",
			"## 📬 메일 분석 보고\n\n### 메일 기본 정보\n\n**발신자**: 김대희\n\n### 프로젝트 맥락 (부산8호태양광)\n\n### 세창스틸 1·2공장 태양광 견적서",
			"세창스틸",
			"📬 메일 분석 보고",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := extractCardTitle(tc.content)
			if !strings.Contains(got, tc.wantHas) {
				t.Errorf("title = %q, want it to contain %q", got, tc.wantHas)
			}
			if got == tc.wantNotEq {
				t.Errorf("title stayed generic %q, want the subject", got)
			}
			// Mail subjects are clamped short (mailSubjectMaxRunes + the "..." suffix).
			if n := len([]rune(got)); n > mailSubjectMaxRunes+3 {
				t.Errorf("title %q is %d runes, want <= %d", got, n, mailSubjectMaxRunes+3)
			}
		})
	}

	// A batch/daily summary lacks 리포트/보고, so its own heading is kept.
	if got, _ := extractCardTitle("# 📬 6/15(월) 당일 메일 종합 분석\n\n## 【수신】 1건\n### 무림피앤피 울산공장"); !strings.Contains(got, "당일 메일 종합 분석") {
		t.Errorf("daily-summary title = %q, want it kept", got)
	}
}

func TestExtractCardTitle_ClipsLong(t *testing.T) {
	long := "## " + strings.Repeat("가", 60)
	got, _ := extractCardTitle(long)
	if n := len([]rune(got)); n != 43 { // 40 runes + "..."
		t.Fatalf("clipped len = %d (%q), want 43", n, got)
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("want ellipsis suffix, got %q", got)
	}
}

func TestTightenMailSubject(t *testing.T) {
	cases := map[string]string{
		"Re: 가견적서 재송부":         "가견적서 재송부",         // reply prefix
		"RE: FW: 과업지시서":        "과업지시서",            // repeated reply/forward prefixes
		"[회신] 케이블 발주":          "케이블 발주",           // bracketed reply tag
		"📧 무림P&P 규격서 송부":       "무림P&P 규격서 송부",     // leading decorative emoji
		"📧 Re: 케이블 발주":         "케이블 발주",           // emoji + reply prefix, any order
		"회신: 진영상사 발주 건":        "진영상사 발주 건",        // "회신:" stripped; bare "건" (not "의 건") is kept
		"태양광 가견적서 송부의 건":       "태양광 가견적서 송부",      // trailing "…의 건"
		"규격서 검토 요청 드립니다":       "규격서 검토",           // trailing politeness
		"도면 재송부 부탁드립니다":        "도면 재송부",           // trailing politeness (no space)
		"확인 바랍니다":              "확인",               // trailing politeness
		"광명역 배치도 송부의 件":        "광명역 배치도 송부",       // hanja 件 (not just hangul 건)
		"검토 요망":                "검토",               // trailing "요망"
		"현대차 가견적서 (재송부)":       "현대차 가견적서",         // trailing filler parenthetical
		"제안서 (2차)":             "제안서",              // numbered filler parenthetical
		"회의 (긴급)":              "회의 (긴급)",          // non-filler parenthetical → kept
		"제목: PROIRITY Updated": "PROIRITY Updated", // leading "제목:" field label dropped
		"[긴급] 단가 확인":           "[긴급] 단가 확인",       // [긴급] is NOT a reply tag → kept
		"보통 제목":                "보통 제목",            // nothing to strip ("제목" without colon)
	}
	for in, want := range cases {
		if got := tightenMailSubject(in); got != want {
			t.Errorf("tightenMailSubject(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestExtractCardTitle_AnalysisLabelPrefix covers the "📧 메일 분석: <subject>" form
// (a generic label with the subject inline after the colon) — observed in real feed
// titles. The label is dropped and the subject leads, tightened and ≤20 runes.
func TestExtractCardTitle_AnalysisLabelPrefix(t *testing.T) {
	cases := []struct {
		name    string
		content string
		wantHas string
	}{
		{
			"emoji + 메일 분석: + trailing (재송부)",
			"## 📧 메일 분석: 현대자동차 울산공장 생기센터 태양광 가견적서 (재송부)",
			"현대자동차 울산공장",
		},
		{
			"메일 분석: + 송부의 件",
			"### 메일 분석: 광명역 B주차장 태양광 배치도 송부의 件",
			"광명역",
		},
		{
			"bare 분석: label",
			"📧 분석: 무림피앤피 2차 태양광 물품 제안요청서",
			"무림피앤피",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := extractCardTitle(tc.content)
			if !strings.Contains(got, tc.wantHas) {
				t.Errorf("title = %q, want it to contain %q", got, tc.wantHas)
			}
			if strings.Contains(got, "분석") {
				t.Errorf("title = %q, want the analysis label dropped", got)
			}
			if n := len([]rune(got)); n > mailSubjectMaxRunes+3 {
				t.Errorf("title %q is %d runes, want <= %d", got, n, mailSubjectMaxRunes+3)
			}
		})
	}
}

func TestClipRunesWord(t *testing.T) {
	// Within the limit: returned unchanged, no ellipsis.
	if got := clipRunesWord("짧은 제목", 20); got != "짧은 제목" {
		t.Errorf("clipRunesWord short = %q, want unchanged", got)
	}
	// Over the limit with a near-the-end space: clean word break (no split 어절).
	if got := clipRunesWord("현대차 울산공장 태양광 가견적서 재송부", 20); got != "현대차 울산공장 태양광 가견적서..." {
		t.Errorf("clipRunesWord boundary = %q, want a clean word break", got)
	}
	// A long trailing token that straddles the limit: hard-cut to keep more of it
	// rather than dropping the whole token.
	if got := clipRunesWord("Korean Tax Update_Samil Commentary", 20); got != "Korean Tax Update_Sa..." {
		t.Errorf("clipRunesWord long-token = %q, want a hard cut", got)
	}
}

func TestExtractCardSummary(t *testing.T) {
	cases := []struct {
		name       string
		content    string
		wantHas    []string
		wantNotHas []string
	}{
		{
			"sub-heading enriches summary",
			"## 📧 메일 분석 보고\n\n### 🔴 긴급\n대한전선 착수보고회 D-2 자료 검토 필요",
			[]string{"긴급", "대한전선"},
			[]string{"##", "###"},
		},
		{
			"unwraps bold label",
			"## 📧 최신 메일 분석 보고\n\n**분석 대상**: fred@jocacable.com → 2026-06-08",
			[]string{"분석 대상", "fred@jocacable.com"},
			[]string{"**"},
		},
		{
			"leading hrule then body",
			"---\n\n## 📧 JOCA Cable 최신 메일 분석 보고\n\n**발신**: fred@jocacable.com",
			[]string{"발신", "fred@jocacable.com"},
			[]string{"---", "**", "##"},
		},
		{
			"bullet list body",
			"## 📬 메일 요약\n\n- **발신**: Fred Lee (JOCA)\n- **제목**: solar cable 가격",
			[]string{"발신", "Fred Lee", "제목"},
			[]string{"- ", "**"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, srcLine := extractCardTitle(tc.content)
			got := extractCardSummary(tc.content, srcLine)
			for _, sub := range tc.wantHas {
				if !strings.Contains(got, sub) {
					t.Errorf("summary %q missing %q", got, sub)
				}
			}
			for _, sub := range tc.wantNotHas {
				if strings.Contains(got, sub) {
					t.Errorf("summary %q leaked marker %q", got, sub)
				}
			}
		})
	}
}

func TestExtractCardSummary_FallsBackToTitleLine(t *testing.T) {
	content := "### 분석 결과"
	_, srcLine := extractCardTitle(content)
	if got := extractCardSummary(content, srcLine); got != "분석 결과" {
		t.Errorf("summary fallback = %q, want 분석 결과", got)
	}
}

func TestExtractCardSummary_SkipsTableSeparator(t *testing.T) {
	content := "## 비교\n\n| 구분 | 값 |\n|---|---|\n| A | 1 |"
	_, srcLine := extractCardTitle(content)
	got := extractCardSummary(content, srcLine)
	if strings.Contains(got, "|") || strings.Contains(got, "---") {
		t.Errorf("summary leaked table markup: %q", got)
	}
	if !strings.Contains(got, "구분") {
		t.Errorf("summary %q should carry table cell text", got)
	}
}

func TestStripMarkdownLine(t *testing.T) {
	cases := map[string]string{
		"## 📧 메일":        "📧 메일",
		"- **발신**: Fred": "발신: Fred",
		"1. 메일 본문 요약":    "메일 본문 요약",
		"> 인용문":          "인용문",
		"**굵게**":         "굵게",
		"`코드`":           "코드",
		"| 항목 | 값 |":     "항목 값",
		"평범한 텍스트":        "평범한 텍스트",
	}
	for in, want := range cases {
		if got := stripMarkdownLine(in); got != want {
			t.Errorf("stripMarkdownLine(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsHorizontalRule(t *testing.T) {
	for _, s := range []string{"---", "***", "___", "===", "------"} {
		if !isHorizontalRule(s) {
			t.Errorf("isHorizontalRule(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"--", "## 분석", "a---", "- 항목", ""} {
		if isHorizontalRule(s) {
			t.Errorf("isHorizontalRule(%q) = true, want false", s)
		}
	}
}

func TestIsTableSeparator(t *testing.T) {
	for _, s := range []string{"|---|---|", "| --- | :--: |", "|:--|"} {
		if !isTableSeparator(s) {
			t.Errorf("isTableSeparator(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"| 항목 | 값 |", "표 없음", "---"} {
		if isTableSeparator(s) {
			t.Errorf("isTableSeparator(%q) = true, want false", s)
		}
	}
}

func TestSubstantiveText(t *testing.T) {
	// Markers, emoji, and whitespace removed; Hangul preserved.
	if got := substantiveText("## 📧 알림\n\n- 변경 없음\n---"); got != "알림변경없음" {
		t.Errorf("substantiveText = %q, want 알림변경없음", got)
	}
	if got := substantiveText("🔴 긴급 🐾 보고"); got != "긴급보고" {
		t.Errorf("substantiveText = %q, want 긴급보고", got)
	}
}

func TestClipRunes(t *testing.T) {
	if got := clipRunes("짧음", 10); got != "짧음" {
		t.Errorf("clipRunes short = %q, want 짧음", got)
	}
	if got := clipRunes("가나다라마", 3); got != "가나다..." {
		t.Errorf("clipRunes = %q, want 가나다...", got)
	}
	if got := clipRunes("무제한", 0); got != "무제한" {
		t.Errorf("clipRunes(0) = %q, want 무제한", got)
	}
}

package chat

import (
	"strings"
	"testing"
)

func TestCleanSessionTitleNormalizesRawText(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "세금계산서 정리", "세금계산서 정리"},
		{"strips 제목 prefix", "제목: 오늘 일정 요약", "오늘 일정 요약"},
		{"strips echoed speaker tag", "사용자: 견적 검토 요청", "견적 검토 요청"},
		{"strips echoed assistant tag", "어시스턴트: 견적 검토 결과", "견적 검토 결과"},
		{"strips wrapping quotes", "\"메일 분석 결과\"", "메일 분석 결과"},
		{"strips smart quotes", "“프로젝트 현황”", "프로젝트 현황"},
		{"first line only", "회의록 정리\n추가 설명은 무시", "회의록 정리"},
		{"trailing period", "예산 검토.", "예산 검토"},
		{"collapses whitespace", "  탑솔라   계약   검토  ", "탑솔라 계약 검토"},
		{"empty", "   ", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := cleanSessionTitle(c.in); got != c.want {
				t.Errorf("cleanSessionTitle(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestCleanSessionTitleTruncatesLongTitles(t *testing.T) {
	long := strings.Repeat("가", 100)
	got := cleanSessionTitle(long)
	if n := len([]rune(got)); n > sessionTitleLabelCap {
		t.Errorf("title rune length = %d, want <= %d", n, sessionTitleLabelCap)
	}
}

func TestIsAutoTitleSessionReturnsTrueForConversationKeys(t *testing.T) {
	cases := map[string]bool{
		// Per-conversation native chats are titled.
		"client:main:9f1c2a":       true, // 업무 explicit new chat (child of the home)
		"client:main:wf-mail-argo": true, // 업무 work-card side-conversation
		"chat:9f1c2a":              true, // legacy 챗봇 conversation (titleable when continued)
		// The 업무 home keeps its fixed identity.
		"client:main": false,
		// Bare prefixes (empty suffix) are not conversations.
		"chat:":   false,
		"client:": false,
		// Other namespaces are never auto-titled.
		"cron:mail-analysis": false,
		"system:boot":        false,
		"":                   false,
	}
	for key, want := range cases {
		if got := isAutoTitleSession(key); got != want {
			t.Errorf("isAutoTitleSession(%q) = %v, want %v", key, got, want)
		}
	}
}

func TestDocumentHintsExtractsSharedFileNames(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "share marker with spaced korean filename",
			in:   "📲 공유 맥락: 누구 말이 맞아? 📄 공유 문서에서 추출한 텍스트 (230213_양명에너지 주식양수도계약.docx): 계약 내용...",
			want: []string{"230213_양명에너지 주식양수도계약.docx"},
		},
		{
			name: "caps at two distinct names",
			in:   "첨부: 견적서.xlsx 그리고 계약서.pdf 그리고 발주서.pdf",
			want: []string{"견적서.xlsx", "계약서.pdf"},
		},
		{
			name: "no document",
			in:   "그냥 안부 인사예요",
			want: nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := documentHints(c.in)
			if len(got) != len(c.want) {
				t.Fatalf("documentHints(%q) = %v, want %v", c.in, got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Errorf("documentHints[%d] = %q, want %q", i, got[i], c.want[i])
				}
			}
		})
	}
}

func TestFirstLineReturnsTextBeforeNewline(t *testing.T) {
	cases := map[string]string{
		"a\nb":   "a",
		"a\r\nb": "a",
		"single": "single",
		"":       "",
	}
	for in, want := range cases {
		if got := firstLine(in); got != want {
			t.Errorf("firstLine(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCapRunesTruncatesByRuneCount(t *testing.T) {
	if got := capRunes("hello", 3); got != "hel" {
		t.Errorf("capRunes truncate = %q, want %q", got, "hel")
	}
	// CJK runes must be counted as single runes, not bytes.
	if got := capRunes("한국어제목", 3); got != "한국어" {
		t.Errorf("capRunes CJK = %q, want %q", got, "한국어")
	}
	if got := capRunes("  short  ", 50); got != "short" {
		t.Errorf("capRunes trims = %q, want %q", got, "short")
	}
}

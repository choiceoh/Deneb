package telegram

import (
	"strings"
	"testing"
)

func TestNormalizeForTelegram_HTMLTags(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		mustHave []string
		mustNot  []string
	}{
		{
			name:     "bold tag → markdown",
			in:       `그 점이 정말 <b>매우 놀라운</b> 포인트죠.`,
			mustHave: []string{"**매우 놀라운**"},
			mustNot:  []string{"<b>", "</b>"},
		},
		{
			name:     "italic tag → markdown",
			in:       `이건 <i>업데이트</i>를 넘어선 변화.`,
			mustHave: []string{"*업데이트*"},
			mustNot:  []string{"<i>", "</i>"},
		},
		{
			name:     "strong tag",
			in:       `<strong>핵심</strong>입니다.`,
			mustHave: []string{"**핵심**"},
			mustNot:  []string{"<strong>"},
		},
		{
			name:     "named + numeric entities",
			in:       `엔티티 &#x27;hi&#x27; &quot;world&quot; &amp; 끝`,
			mustHave: []string{"'hi'", `"world"`, "& 끝"},
			mustNot:  []string{"&#x27;", "&quot;", "&amp;"},
		},
		{
			name:     "anchor → markdown link",
			in:       `<a href="https://example.com">예시</a>`,
			mustHave: []string{"[예시](https://example.com)"},
			mustNot:  []string{"<a", "</a>"},
		},
		{
			name:     "plain text untouched",
			in:       `평문은 그대로 유지되어야 합니다`,
			mustHave: []string{"평문은 그대로 유지되어야 합니다"},
		},
		{
			name:     "early-return on no special chars",
			in:       `숫자 123 그리고 한글만 있는 문장`,
			mustHave: []string{"숫자 123 그리고 한글만 있는 문장"},
		},
	}
	for _, c := range cases {
		got := normalizeForTelegram(c.in)
		for _, must := range c.mustHave {
			if !strings.Contains(got, must) {
				t.Errorf("[%s] expected %q in output, got %q", c.name, must, got)
			}
		}
		for _, not := range c.mustNot {
			if strings.Contains(got, not) {
				t.Errorf("[%s] did not expect %q in output, got %q", c.name, not, got)
			}
		}
	}
}

func TestNormalizeForTelegram_MarkdownTable(t *testing.T) {
	in := `여기 비교표:

| 모델 | 속도 | MTP |
|------|------|-----|
| Qwen3.6 | 60 tok/s | 90% |
| Qwen3.5 | 45 tok/s | 76% |

이게 결과입니다.`
	got := normalizeForTelegram(in)
	mustHave := []string{
		"**모델**: Qwen3.6",
		"**속도**: 60 tok/s",
		"**MTP**: 90%",
		"**모델**: Qwen3.5",
		"이게 결과입니다.",
	}
	mustNot := []string{
		"|------|",
		"| 모델 |",
		"|---|",
	}
	for _, m := range mustHave {
		if !strings.Contains(got, m) {
			t.Errorf("expected %q in output, got:\n%s", m, got)
		}
	}
	for _, n := range mustNot {
		if strings.Contains(got, n) {
			t.Errorf("did not expect %q in output, got:\n%s", n, got)
		}
	}
}

func TestNormalizeForTelegram_PreservesCodeContent(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "fenced block with html",
			in:   "예시:\n```html\n<b>example</b>\n```\n끝.",
			want: "<b>example</b>",
		},
		{
			name: "inline code with html",
			in:   "이렇게 쓰지 마: `<b>text</b>` — 별표를 쓰세요.",
			want: "`<b>text</b>`",
		},
		{
			name: "inline code with entity",
			in:   "엔티티 예: `&#x27;` 는 어포스트로피입니다.",
			want: "`&#x27;`",
		},
		{
			name: "fenced block with markdown table",
			in:   "코드 예제:\n```\n| A | B |\n|---|---|\n| 1 | 2 |\n```\n끝.",
			want: "| A | B |",
		},
	}
	for _, c := range cases {
		got := normalizeForTelegram(c.in)
		if !strings.Contains(got, c.want) {
			t.Errorf("[%s] expected %q in output, got:\n%s", c.name, c.want, got)
		}
	}
}

func TestNormalizeForTelegram_NoFalsePositiveOnPipes(t *testing.T) {
	// Plain sentence with pipes — no separator row → must not be treated as a table.
	in := "선택지: A | B | C 중 하나를 고르세요."
	got := normalizeForTelegram(in)
	if !strings.Contains(got, "선택지: A | B | C") {
		t.Errorf("plain pipe sentence was mangled: %q", got)
	}
}

func TestNormalizeForTelegram_EntityEscapedHTML(t *testing.T) {
	// 모델이 raw <b> 대신 entity-encoded &lt;b&gt;로 출력하는 케이스.
	// UnescapeString이 정규식 앞에서 도는지 회귀 보호.
	in := `값은 &lt;b&gt;42&lt;/b&gt; 이고 &lt;i&gt;정확함&lt;/i&gt;.`
	got := normalizeForTelegram(in)
	if strings.Contains(got, "&lt;") || strings.Contains(got, "&gt;") {
		t.Errorf("expected entities to be decoded and rewritten, got: %s", got)
	}
	if !strings.Contains(got, "**42**") {
		t.Errorf("expected **42** in output, got: %s", got)
	}
	if !strings.Contains(got, "*정확함*") {
		t.Errorf("expected *정확함* in output, got: %s", got)
	}
}

func TestNormalizeForTelegram_UserBugReport(t *testing.T) {
	// 실제 사용자 텔레그램에서 raw HTML이 노출된 응답 sample.
	in := `같다"는 인상은 <b>데이터 해석의 차이</b>에서 비롯되었을 가능성이 큽니다.

• <b>화웨이</b>:
  • <b>결과</b>: 운영자가 &quot;고장&quot;이라고 신고하지 않아도 시스템이 스스로 복구하므로, <b>전산상 &#x27;고장 건수&#x27;가 낮게 기록</b>될 수 있습니다.

<blockquote><b>핵심</b>: <b>화웨이는 &quot;잘못 고장 안 난 척&quot; 하지만, 썬그로우는 &quot;고장 나면 확실하게 잡아서 고친다&quot;</b>는 차이가 있습니다.</blockquote>

• <b>하드웨어 고장률</b>: <b>썬그로우 &lt; 화웨이</b>`
	got := normalizeForTelegram(in)
	mustNot := []string{"<b>", "</b>", "<blockquote>", "&quot;", "&#x27;", "&lt;", "&gt;", "&amp;"}
	for _, n := range mustNot {
		if strings.Contains(got, n) {
			t.Errorf("expected %q to be normalized away, got:\n%s", n, got)
		}
	}
	mustHave := []string{"**데이터 해석의 차이**", "**화웨이**", "**썬그로우 < 화웨이**", `"잘못 고장 안 난 척"`, "'고장 건수'"}
	for _, m := range mustHave {
		if !strings.Contains(got, m) {
			t.Errorf("expected %q in output, got:\n%s", m, got)
		}
	}
}

func TestMarkdownToTelegramHTML_NormalizesBeforeRender(t *testing.T) {
	// End-to-end: model emits raw <b> in markdown text, MarkdownToTelegramHTML
	// should produce the proper Telegram <b> tag (not literal "&lt;b&gt;").
	in := `<b>중요</b> 메시지`
	out := MarkdownToTelegramHTML(in)
	if !strings.Contains(out, "<b>중요</b>") {
		t.Errorf("expected <b>중요</b> in output, got: %s", out)
	}
	if strings.Contains(out, "&lt;b&gt;") {
		t.Errorf("did not expect HTML-escaped tag, got: %s", out)
	}
}

func TestMarkdownToTelegramHTML_FlattensTables(t *testing.T) {
	in := "| A | B |\n|---|---|\n| 1 | 2 |"
	out := MarkdownToTelegramHTML(in)
	// After flattening: "- **A**: 1 / **B**: 2"
	// MarkdownToTelegramHTML should render that as a bullet with HTML <b> tags.
	if !strings.Contains(out, "<b>A</b>") {
		t.Errorf("expected bold header A in output, got: %s", out)
	}
	if !strings.Contains(out, "1") || !strings.Contains(out, "2") {
		t.Errorf("expected cell values in output, got: %s", out)
	}
	if strings.Contains(out, "|---|") {
		t.Errorf("table separator should not survive normalization, got: %s", out)
	}
}

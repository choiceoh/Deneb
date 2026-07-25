package denebui

import (
	"log/slog"
	"strings"
	"testing"
)

func TestNormalizeFinalReplyHTMLAnswers(t *testing.T) {
	t.Parallel()

	doc := "<!doctype html>\n<html><body><h1>보고</h1><button onclick=\"deneb.send('확인')\">확인</button></body></html>"
	valid := "요약입니다.\n```deneb-html\n" + doc + "\n```\n끝."

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "one valid html answer is kept verbatim",
			in:   valid,
			want: valid,
		},
		{
			name: "unclosed fence at EOF is closed",
			in:   "```deneb-html\n<div>진행 요약</div>",
			want: "```deneb-html\n<div>진행 요약</div>\n```",
		},
		{
			name: "second html fence degrades to a code block",
			in:   "```deneb-html\n<div>하나</div>\n```\n```deneb-html\n<div>둘</div>\n```",
			want: "```deneb-html\n<div>하나</div>\n```\n```html\n<div>둘</div>\n```",
		},
		{
			name: "non-markup body degrades to a code block",
			in:   "```deneb-html\n그냥 텍스트\n```",
			want: "```html\n그냥 텍스트\n```",
		},
		{
			name: "prose mention of the fence stays prose",
			in:   "```deneb-html 펜스는 이렇게 씁니다 — 라고 설명만 하는 문장.",
			want: "```deneb-html 펜스는 이렇게 씁니다 — 라고 설명만 하는 문장.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeFinalReply(tt.in, "client:main", slog.New(slog.DiscardHandler))
			if got != tt.want {
				t.Fatalf("NormalizeFinalReply() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeFinalReplyHTMLAnswerOversize(t *testing.T) {
	t.Parallel()
	doc := "<div>" + strings.Repeat("가", MaxHTMLAnswerBytes) + "</div>"
	got := NormalizeFinalReply("```deneb-html\n"+doc+"\n```", "client:main", slog.New(slog.DiscardHandler))
	if !strings.HasPrefix(got, "```html\n") {
		t.Fatalf("oversized document must degrade to a ```html code block, got prefix %q", got[:20])
	}
}

func TestNormalizeFinalReplyHTMLAnswerIsIdempotent(t *testing.T) {
	t.Parallel()
	in := "설명\n```deneb-html\n<div id=\"x\"><script>var a='1';deneb.send(a);</script></div>\n```"
	once := NormalizeFinalReply(in, "client:main", slog.New(slog.DiscardHandler))
	twice := NormalizeFinalReply(once, "client:main", slog.New(slog.DiscardHandler))
	if twice != once {
		t.Fatalf("second normalization changed output:\nonce: %q\ntwice: %q", once, twice)
	}
}

func TestNormalizeFinalReplyRecoverableCardIssues(t *testing.T) {
	t.Parallel()

	// Unknown-but-unwrapped tags are content-preserving: the card must deliver
	// as a card, not degrade to plain text. (title/label/spacer graduated to
	// real aliases — use a genuinely unknown tag to keep exercising unwrap.)
	in := "```deneb-ui\n<column><card><wrapper-x>대한전선 실사</wrapper-x><text>본문</text></card></column>\n```"
	got := NormalizeFinalReply(in, "client:main", slog.New(slog.DiscardHandler))
	if len(ExtractFences(got)) != 1 {
		t.Fatalf("card with only unknown-tag issues must stay a card, got %q", got)
	}

	// A structural violation (interactive input without id) must still degrade.
	bad := "```deneb-ui\n<column><input/></column>\n```"
	if got := NormalizeFinalReply(bad, "client:main", slog.New(slog.DiscardHandler)); len(ExtractFences(got)) != 0 {
		t.Fatalf("id-missing interactive card must degrade, got %q", got)
	}
}

func TestIssueRecoverable(t *testing.T) {
	t.Parallel()
	issues, err := Validate("<column><whatever>내용</whatever></column>")
	if err != nil || len(issues) == 0 {
		t.Fatalf("expected unknown-tag issues, got issues=%v err=%v", issues, err)
	}
	for _, is := range issues {
		if !is.Recoverable() {
			t.Fatalf("unknown-tag issue must be recoverable: %v", is)
		}
	}
	issues, err = Validate("<column><input/></column>")
	if err != nil || len(issues) == 0 {
		t.Fatalf("expected id-missing issue, got issues=%v err=%v", issues, err)
	}
	for _, is := range issues {
		if is.Recoverable() {
			t.Fatalf("id-missing issue must NOT be recoverable: %v", is)
		}
	}
}

// A wrong value for a presentation enum must not cost the operator the whole
// card: every renderer falls back to a default (Kotlin styleOf → null,
// andromeda TEXT_STYLE[...] ?? body), so the server must not be stricter than
// the clients it serves. Live 2026-07: cards were rejected before delivery on
// `invalid style` / `invalid variant` alone.
func TestIssueRecoverable_InvalidPresentationEnumsDeliverAsCard(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		`<column><text style="완전히엉뚱">본문</text></column>`,
		`<column><button variant="ghost" id="b">버튼</button></column>`,
		`<column><alert severity="아무거나">경고</alert></column>`,
	} {
		issues, err := Validate(body)
		if err != nil || len(issues) == 0 {
			t.Fatalf("%s: expected an enum issue, got issues=%v err=%v", body, issues, err)
		}
		for _, is := range issues {
			if !is.Recoverable() {
				t.Fatalf("%s: presentation-enum issue must be recoverable: %v", body, is)
			}
		}
		card := "```deneb-ui\n" + body + "\n```"
		if got := NormalizeFinalReply(card, "client:main", slog.New(slog.DiscardHandler)); len(ExtractFences(got)) != 1 {
			t.Fatalf("%s: must still deliver as a card, got %q", body, got)
		}
	}

	// A broken ACTION is behavior, not looks — it must still degrade. Only the
	// legacy JSON body can express one: the HTML action attributes
	// (event/href/toggle/copy) each map to a valid type by construction.
	bad := `{"type":"button","label":"버튼","action":{"type":"launch_missiles"}}`
	issues, err := Validate(bad)
	if err != nil || len(issues) == 0 {
		t.Fatalf("expected an action issue, got issues=%v err=%v", issues, err)
	}
	for _, is := range issues {
		if is.Recoverable() {
			t.Fatalf("unknown action type must NOT be recoverable: %v", is)
		}
	}
}

func TestStripHTMLAnswers(t *testing.T) {
	t.Parallel()
	in := "요약 문장.\n```deneb-html\n<!doctype html><div>본문</div>\n```\n마무리."
	got := StripHTMLAnswers(in)
	if got != "요약 문장.\n[웹 응답]\n마무리." {
		t.Fatalf("StripHTMLAnswers() = %q", got)
	}
	// Unclosed fence (mid-stream / truncated) strips to EOF — no markup leaks.
	if got := StripHTMLAnswers("머리\n```deneb-html\n<div>부분"); got != "머리\n[웹 응답]" {
		t.Fatalf("unclosed strip = %q", got)
	}
	// No fence → untouched (no allocation-churn path).
	if got := StripHTMLAnswers("그냥 프로즈"); got != "그냥 프로즈" {
		t.Fatalf("plain text changed: %q", got)
	}
}

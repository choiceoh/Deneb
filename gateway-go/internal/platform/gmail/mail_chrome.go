package gmail

import (
	"regexp"
	"strings"
)

// Marketing-mail chrome and reply-quote heuristics. These run *after*
// htmlToText so they see the same text the operator sees in the Mini App;
// that means the cues can be Korean or English (newsletters in this inbox
// are mixed) and target the rendered visible text rather than HTML.
//
// Word-boundary note: Go's `\b` is ASCII-only — Hangul characters fall
// outside the `\w` class, so `\b<한글>` never matches. English patterns
// keep `\b` to avoid substring matches; Korean patterns drop it.
var (
	// mailPreambleREs — one-line "View in browser" / "이메일이 안 보이시면" /
	// "[광고]" banners that newsletters put at the very top. We only
	// strip when the match lands inside the first headWindow bytes so a
	// mid-body mention doesn't truncate the actual content.
	mailPreambleREs = []*regexp.Regexp{
		// "View in browser" family (en).
		regexp.MustCompile(`(?im)^.*\bview\s+(this\s+email\s+)?in\s+(your\s+)?browser\b.*$`),
		regexp.MustCompile(`(?im)^.*\bview\s+this\s+(email|message|newsletter)\s+online\b.*$`),
		regexp.MustCompile(`(?im)^.*\bview\s+(it\s+)?(online|in\s+a\s+browser)\b.*$`),
		regexp.MustCompile(`(?im)^.*\b(can(?:not|'t|t)?|cannot|unable\s+to)\s+(see|view|read|display)\s+this\s+(email|message|newsletter)\b.*$`),
		regexp.MustCompile(`(?im)^.*\b(having|have)\s+(trouble|problems?)\s+(viewing|seeing|reading|displaying)\b.*$`),
		regexp.MustCompile(`(?im)^.*\bnot\s+(rendering|displaying)\s+(correctly|properly)\b.*$`),
		regexp.MustCompile(`(?im)^.*\b(click|tap)\s+here\s+to\s+(view|read|see)\b.*$`),
		// "이메일이 안 보임" family (ko).
		regexp.MustCompile(`(?im)^.*(이)?메일이?\s*(잘\s*)?(안\s*)?보이지\s*않.*$`),
		regexp.MustCompile(`(?im)^.*(이)?메일이?\s*제대로\s*보이지\s*않.*$`),
		regexp.MustCompile(`(?im)^.*(이)?메일이?\s*깨져\s*보이.*$`),
		regexp.MustCompile(`(?im)^.*웹\s*에서\s*(보기|보시).*$`),
		regexp.MustCompile(`(?im)^.*온라인(에서|으로)\s*보[기시].*$`),
		regexp.MustCompile(`(?im)^.*브라우저(에서|로)\s*보[기시].*$`),
		// Ad markers — conservative: line must be (almost) entirely the
		// marker so we don't cut a real first paragraph that happens to
		// open with "광고".
		regexp.MustCompile(`(?im)^\s*[\[\(]\s*(광고|AD|Ad|광고\s*메일|Advertisement)\s*[\]\)]\s*$`),
	}

	// mailFooterREs — unsubscribe / copyright / disclaimer / signature
	// cues. Each pattern is run independently and the earliest match in
	// the bottom half of the body wins. The "©" branch matches both
	// `© 2026` and `(c) 2026`.
	mailFooterREs = []*regexp.Regexp{
		// Unsubscribe / preferences (en).
		regexp.MustCompile(`(?im)^.*\b(unsubscribe|email\s+preferences|stop\s+receiving|manage\s+(your\s+)?subscriptions?|update\s+(your\s+)?preferences)\b.*$`),
		regexp.MustCompile(`(?im)^.*\bno\s+longer\s+(wish|want)\s+to\s+receive\b.*$`),
		regexp.MustCompile(`(?im)^.*\byou(?:'re|\s+are|\s+were)?\s+receiv(?:ing|ed)\s+this\s+(email|message|newsletter)\s+because\b.*$`),
		// Unsubscribe / disclaimer (ko).
		regexp.MustCompile(`(?im)^.*(수신\s*거부|수신거부|구독\s*해지|구독해지|수신\s*동의\s*철회).*$`),
		regexp.MustCompile(`(?im)^.*(더\s*이상|더이상)\s*수신을?\s*원(하지|치)\s*않.*$`),
		regexp.MustCompile(`(?im)^.*이\s*(이)?메일을?\s*받으신?\s*이유.*$`),
		// Auto-send / do-not-reply (en).
		regexp.MustCompile(`(?im)^.*\b(do\s+not|please\s+do\s+not|don'?t)\s+reply\s+to\s+this\b.*$`),
		regexp.MustCompile(`(?im)^.*\bno-?reply\b.*$`),
		regexp.MustCompile(`(?im)^.*\bautomat(ed|ically)\s+(generated|sent)\b.*$`),
		regexp.MustCompile(`(?im)^.*\bthis\s+is\s+an\s+automated\b.*$`),
		// Auto-send / do-not-reply (ko).
		regexp.MustCompile(`(?im)^.*(이|본)\s*(이)?메일은?\s.{0,40}(자동|발신\s*전용).*$`),
		regexp.MustCompile(`(?im)^.*자동(으로)?\s*(발송|전송|생성)된?.*(이|본)?\s*(이)?메일.*$`),
		regexp.MustCompile(`(?im)^.*(회신|답장)(하지|을\s*하지)\s*마(세요|십시오|시기).*$`),
		regexp.MustCompile(`(?im)^.*발신\s*전용.*$`),
		// Company / business registration (ko).
		regexp.MustCompile(`(?im)^.*사업자\s*등록\s*번호.*$`),
		regexp.MustCompile(`(?im)^.*통신판매업\s*신고.*$`),
		// Privacy / terms (en/ko). These can appear in body, but as a
		// dedicated *line* in the bottom half they're almost always the
		// boilerplate footer.
		regexp.MustCompile(`(?im)^.*\bprivacy\s+(policy|notice|statement)\b.*$`),
		regexp.MustCompile(`(?im)^.*\bterms\s+of\s+(service|use)\b.*$`),
		regexp.MustCompile(`(?im)^.*개인정보\s*(처리|취급)\s*방침.*$`),
		regexp.MustCompile(`(?im)^.*이용\s*약관.*$`),
		// Copyright (en).
		regexp.MustCompile(`(?im)^.*(©|\(c\))\s*\d{4}.*$`),
		regexp.MustCompile(`(?im)^.*\bcopyright\s+(\(c\)|©)?\s*\d{4}.*$`),
		regexp.MustCompile(`(?im)^.*\ball\s+rights\s+reserved\b.*$`),
		// Mobile signature (en/ko).
		regexp.MustCompile(`(?im)^\s*sent\s+from\s+my\s+(iphone|ipad|android|galaxy|samsung|mobile|phone|smartphone)\b.*$`),
		regexp.MustCompile(`(?im)^\s*get\s+outlook\s+for\s+(ios|android)\s*$`),
		// RFC 3676 signature delimiter — a line that is exactly "-- "
		// (two dashes + space) or "--" alone. Some clients trim the
		// trailing space.
		regexp.MustCompile(`(?m)^-- ?$`),
	}

	// mailSeparatorRE — a line containing only separator characters
	// (≥5 of them). Marketing templates love these as section dividers,
	// but in a <pre> view they're noise. Replaced with a blank line so
	// the trailing blank-line collapser still does its job.
	mailSeparatorRE = regexp.MustCompile(`(?m)^\s*[\-=_*─━–—•·～~]{5,}\s*$`)

	// mailReplyQuoteREs — markers that begin a quoted-reply / forwarded
	// section. The earliest match cuts the body, but only if the surviving
	// prefix has at least minReplyVisible visible characters — otherwise
	// the reply itself is shorter than the noise we'd preserve, suggesting
	// the marker is mid-body commentary rather than a real quote opener.
	mailReplyQuoteREs = []*regexp.Regexp{
		// Gmail-style: "On Mon, Jan 1, 2026 at 1:23 PM, Alice <a@b.com> wrote:"
		regexp.MustCompile(`(?im)^\s*On\s+.{4,200}\s+wrote\s*[:：]\s*$`),
		// Korean Gmail: "2026년 5월 27일 (화) 오후 1:23, Alice <a@b.com>님이 작성:"
		regexp.MustCompile(`(?im)^\s*\d{4}년\s+\d{1,2}월.{0,200}(작성|썼습니다|보냄)\s*[:：]?\s*$`),
		// Outlook / Gmail forward divider lines.
		regexp.MustCompile(`(?im)^\s*[-_]{3,}\s*(Original\s+Message|Forwarded\s+(message|Message)|원본\s*(메시지|메일)|전달된?\s*(메시지|메일))\s*[-_]{3,}\s*$`),
		// Korean Outlook-style header block opener. Tightened by requiring
		// it to look like a header line ("보낸 사람: <name/email>") rather
		// than a mid-body mention.
		regexp.MustCompile(`(?im)^\s*보낸\s*사람\s*[:：]\s*\S.{0,200}$`),
		// "[원문]" / "[Original message]" bracket markers.
		regexp.MustCompile(`(?im)^\s*\[\s*(Original\s+message|원문\s*(메시지|메일)?|인용)\s*\]\s*$`),
	}
)

const (
	// preambleHeadWindow — only preamble cues found within this many
	// bytes from the top of the body are considered. Larger than the
	// previous 500 because some newsletters wrap their banner inside a
	// short logo + tagline header that pushes the "view online" line
	// past 500 bytes.
	preambleHeadWindow = 800

	// minReplyVisible — minimum non-whitespace rune count the surviving
	// prefix must have for stripMailReplyQuote to fire. Below this the
	// "reply" is shorter than the quoted history; keep both rather than
	// risk discarding the actual content.
	minReplyVisible = 50

	// shortBodyFloor — bodies under this byte count skip all chrome
	// stripping. OTPs, alerts, and one-line replies live here, and a
	// pattern misfire on them would discard the entire message.
	shortBodyFloor = 200
)

// stripMailChrome trims marketing chrome (top banner, bottom footer,
// section separators) and quoted-reply tails from an already-rendered
// text body. Safe by design: the chrome phase has a 75% safety gate and
// the reply-quote phase has a visible-length floor on the surviving
// prefix.
func stripMailChrome(s string) string {
	if len(s) < shortBodyFloor {
		// Short bodies (one-line replies, OTPs, alerts) don't have
		// chrome to strip and are the population where a misfire
		// would do the most damage.
		return s
	}
	original := s

	// Phase 1: marketing chrome (preamble + footer + separators) with
	// 75% safety abort — if heuristics carve away >75% we fall back to
	// the original input rather than silently lose real content.
	stripped := stripMailPreamble(s)
	stripped = stripMailFooter(stripped)
	stripped = mailSeparatorRE.ReplaceAllString(stripped, "")
	stripped = htmlBlankRE.ReplaceAllString(stripped, "\n\n")
	stripped = strings.TrimSpace(stripped)
	if len(stripped) >= len(original)/4 {
		s = stripped
	} else {
		s = original
	}

	// Phase 2: reply-quote tail. Markers here are explicit ("----- Original
	// Message -----", "On X wrote:", "보낸 사람:") so we let this cut even
	// when it removes >75% of the body — that's exactly the case it's
	// meant to handle (short reply, long quoted history).
	s = stripMailReplyQuote(s)
	return s
}

// stripMailPreamble removes everything up to and including the matched
// preamble line (view-in-browser banner, ad marker, etc.), but only when
// the line sits in the first preambleHeadWindow bytes of the body. Returns
// the input unchanged on no-match. The first pattern to match wins; we
// don't try to find the *latest* preamble because that risks chewing into
// real content.
func stripMailPreamble(s string) string {
	headEnd := preambleHeadWindow
	if headEnd > len(s) {
		headEnd = len(s)
	}
	head := s[:headEnd]
	for _, re := range mailPreambleREs {
		if loc := re.FindStringIndex(head); loc != nil {
			rest := s[loc[1]:]
			return strings.TrimLeft(rest, "\n \t")
		}
	}
	return s
}

// stripMailFooter finds the earliest footer cue that sits in the bottom
// half of the body and discards from there to the end. Bottom-half gate
// prevents a "Copyright" mention inside the actual content from cutting
// the body short. Returns the input unchanged when no cue lands in the
// safe zone.
func stripMailFooter(s string) string {
	bottomStart := len(s) / 2
	cutAt := -1
	for _, re := range mailFooterREs {
		// FindAllStringIndex is O(N) but the bodies are at most a
		// few KB after htmlToText, so this is fine.
		for _, m := range re.FindAllStringIndex(s, -1) {
			if m[0] < bottomStart {
				continue
			}
			if cutAt == -1 || m[0] < cutAt {
				cutAt = m[0]
			}
			break
		}
	}
	if cutAt < 0 {
		return s
	}
	return strings.TrimRight(s[:cutAt], " \t\n")
}

// stripMailReplyQuote cuts the body at the earliest reply / forward
// marker. The cut is only applied when the surviving prefix has at least
// minReplyVisible visible (non-whitespace) characters — otherwise the
// "reply" is shorter than the noise we'd preserve, which usually means
// the user is forwarding without commentary and we should keep the body
// intact so the operator can read the forwarded content.
func stripMailReplyQuote(s string) string {
	cutAt := -1
	for _, re := range mailReplyQuoteREs {
		if loc := re.FindStringIndex(s); loc != nil {
			if cutAt == -1 || loc[0] < cutAt {
				cutAt = loc[0]
			}
		}
	}
	if cutAt < 0 {
		return s
	}
	prefix := strings.TrimRight(s[:cutAt], " \t\n")
	if visibleRuneCount(prefix) < minReplyVisible {
		return s
	}
	return prefix
}

// visibleRuneCount counts non-whitespace runes — a proxy for "real
// content" that survives blank-line padding and indent characters.
func visibleRuneCount(s string) int {
	n := 0
	for _, r := range s {
		switch r {
		case ' ', '\t', '\n', '\r':
			continue
		}
		n++
	}
	return n
}

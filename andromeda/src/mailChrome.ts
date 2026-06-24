// Marketing-mail chrome + reply-quote stripping — a faithful TypeScript port of
// the Deneb gateway's `gmail/mail_chrome.go`. The gateway runs this on its own
// htmlToText output, but Andromeda renders the detail body (HTML→Markdown) on the
// client, which bypasses that path — so we re-apply the same heuristics here.
//
// Operates on already-rendered text (Markdown/plain), so the cues are line-based
// and can be Korean or English. `\b` is ASCII-only in JS (as in Go), so Hangul
// patterns drop it while English ones keep it to avoid substring matches.
//
// Safe by design: bodies under SHORT_BODY_FLOOR skip everything (OTPs, one-line
// replies), the chrome phase aborts if it would carve away >75%, and the reply
// cut needs MIN_REPLY_VISIBLE visible chars left or it keeps the body intact.

const byteLen = (s: string): number => new TextEncoder().encode(s).length;

// Only preamble cues within this many chars of the top are considered.
const PREAMBLE_HEAD_WINDOW = 800;
// Min non-whitespace runes the surviving reply prefix must keep to cut a quote.
const MIN_REPLY_VISIBLE = 50;
// Bodies under this byte count skip all stripping.
const SHORT_BODY_FLOOR = 200;

// One-line "View in browser" / "이메일이 안 보이시면" / "[광고]" banners at the top.
const MAIL_PREAMBLE_RES: RegExp[] = [
  /^.*\bview\s+(this\s+email\s+)?in\s+(your\s+)?browser\b.*$/im,
  /^.*\bview\s+this\s+(email|message|newsletter)\s+online\b.*$/im,
  /^.*\bview\s+(it\s+)?(online|in\s+a\s+browser)\b.*$/im,
  /^.*\b(can(?:not|'t|t)?|cannot|unable\s+to)\s+(see|view|read|display)\s+this\s+(email|message|newsletter)\b.*$/im,
  /^.*\b(having|have)\s+(trouble|problems?)\s+(viewing|seeing|reading|displaying)\b.*$/im,
  /^.*\bnot\s+(rendering|displaying)\s+(correctly|properly)\b.*$/im,
  /^.*\b(click|tap)\s+here\s+to\s+(view|read|see)\b.*$/im,
  /^.*(이)?메일이?\s*(잘\s*)?(안\s*)?보이지\s*않.*$/im,
  /^.*(이)?메일이?\s*제대로\s*보이지\s*않.*$/im,
  /^.*(이)?메일이?\s*깨져\s*보이.*$/im,
  /^.*웹\s*에서\s*(보기|보시).*$/im,
  /^.*온라인(에서|으로)\s*보[기시].*$/im,
  /^.*브라우저(에서|로)\s*보[기시].*$/im,
  /^\s*[[(]\s*(광고|AD|Ad|광고\s*메일|Advertisement)\s*[\])]\s*$/im,
];

// Unsubscribe / copyright / disclaimer / signature cues. Global so matchAll can
// scan for the earliest one in the bottom half of the body.
const MAIL_FOOTER_RES: RegExp[] = [
  /^.*\b(unsubscribe|email\s+preferences|stop\s+receiving|manage\s+(your\s+)?subscriptions?|update\s+(your\s+)?preferences)\b.*$/gim,
  /^.*\bno\s+longer\s+(wish|want)\s+to\s+receive\b.*$/gim,
  /^.*\byou(?:'re|\s+are|\s+were)?\s+receiv(?:ing|ed)\s+this\s+(email|message|newsletter)\s+because\b.*$/gim,
  /^.*(수신\s*거부|수신거부|구독\s*해지|구독해지|수신\s*동의\s*철회).*$/gim,
  /^.*(더\s*이상|더이상)\s*수신을?\s*원(하지|치)\s*않.*$/gim,
  /^.*이\s*(이)?메일을?\s*받으신?\s*이유.*$/gim,
  /^.*\b(do\s+not|please\s+do\s+not|don'?t)\s+reply\s+to\s+this\b.*$/gim,
  /^.*\bno-?reply\b.*$/gim,
  /^.*\bautomat(ed|ically)\s+(generated|sent)\b.*$/gim,
  /^.*\bthis\s+is\s+an\s+automated\b.*$/gim,
  /^.*(이|본)\s*(이)?메일은?\s.{0,40}(자동|발신\s*전용).*$/gim,
  /^.*자동(으로)?\s*(발송|전송|생성)된?.*(이|본)?\s*(이)?메일.*$/gim,
  /^.*(회신|답장)(하지|을\s*하지)\s*마(세요|십시오|시기).*$/gim,
  /^.*발신\s*전용.*$/gim,
  /^.*사업자\s*등록\s*번호.*$/gim,
  /^.*통신판매업\s*신고.*$/gim,
  /^.*\bprivacy\s+(policy|notice|statement)\b.*$/gim,
  /^.*\bterms\s+of\s+(service|use)\b.*$/gim,
  /^.*개인정보\s*(처리|취급)\s*방침.*$/gim,
  /^.*이용\s*약관.*$/gim,
  /^.*(©|\(c\))\s*\d{4}.*$/gim,
  /^.*\bcopyright\s+(\(c\)|©)?\s*\d{4}.*$/gim,
  /^.*\ball\s+rights\s+reserved\b.*$/gim,
  /^\s*sent\s+from\s+my\s+(iphone|ipad|android|galaxy|samsung|mobile|phone|smartphone)\b.*$/gim,
  /^\s*get\s+outlook\s+for\s+(ios|android)\s*$/gim,
  // RFC 3676 signature delimiter — a line that is exactly "-- " or "--".
  /^-- ?$/gm,
];

// A line of only separator characters (≥5). Replaced with "" — section noise.
const MAIL_SEPARATOR_RE = /^\s*[-=_*─━–—•·～~]{5,}\s*$/gm;
// Collapse 3+ blank lines down to one, matching the gateway's htmlBlankRE.
const HTML_BLANK_RE = /\n{3,}/g;

// Markers that begin a quoted-reply / forwarded section.
const MAIL_REPLY_QUOTE_RES: RegExp[] = [
  // Gmail: "On Mon, Jan 1, 2026 at 1:23 PM, Alice <a@b.com> wrote:"
  /^\s*On\s+.{4,200}\s+wrote\s*[:：]\s*$/im,
  // Korean Gmail: "2026년 5월 27일 (화) 오후 1:23, Alice <a@b.com>님이 작성:"
  /^\s*\d{4}년\s+\d{1,2}월.{0,200}(작성|썼습니다|보냄)\s*[:：]?\s*$/im,
  // Outlook / Gmail forward divider lines.
  /^\s*[-_]{3,}\s*(Original\s+Message|Forwarded\s+(message|Message)|원본\s*(메시지|메일)|전달된?\s*(메시지|메일))\s*[-_]{3,}\s*$/im,
  // Korean Outlook-style header opener — header-like "보낸 사람: <name/email>".
  /^\s*보낸\s*사람\s*[:：]\s*\S.{0,200}$/im,
  // "[원문]" / "[Original message]" bracket markers.
  /^\s*\[\s*(Original\s+message|원문\s*(메시지|메일)?|인용)\s*\]\s*$/im,
];

// Counts non-whitespace runes — a proxy for "real content" that survives
// blank-line padding and indent characters.
export function visibleRuneCount(s: string): number {
  let n = 0;
  for (const r of s) {
    if (r === " " || r === "\t" || r === "\n" || r === "\r") continue;
    n++;
  }
  return n;
}

// Removes everything up to and including the first preamble line, but only when
// the line sits in the first PREAMBLE_HEAD_WINDOW chars. Unchanged on no-match.
function stripMailPreamble(s: string): string {
  const head = s.slice(0, PREAMBLE_HEAD_WINDOW);
  for (const re of MAIL_PREAMBLE_RES) {
    re.lastIndex = 0;
    const m = re.exec(head);
    if (m) return s.slice(m.index + m[0].length).replace(/^[\n \t]+/, "");
  }
  return s;
}

// Finds the earliest footer cue in the bottom half of the body and discards from
// there to the end. The bottom-half gate keeps a "Copyright" inside real content
// from cutting the body short. Unchanged when no cue lands in the safe zone.
function stripMailFooter(s: string): string {
  const bottomStart = s.length / 2;
  let cutAt = -1;
  for (const re of MAIL_FOOTER_RES) {
    for (const m of s.matchAll(re)) {
      const idx = m.index ?? 0;
      if (idx < bottomStart) continue;
      if (cutAt === -1 || idx < cutAt) cutAt = idx;
      break;
    }
  }
  if (cutAt < 0) return s;
  return s.slice(0, cutAt).replace(/[ \t\n]+$/, "");
}

// Cuts the body at the earliest reply / forward marker, but only when the
// surviving prefix has at least MIN_REPLY_VISIBLE visible chars — otherwise the
// "reply" is shorter than the quoted history (a bare forward), so keep it whole.
function stripMailReplyQuote(s: string): string {
  let cutAt = -1;
  for (const re of MAIL_REPLY_QUOTE_RES) {
    re.lastIndex = 0;
    const m = re.exec(s);
    if (m && (cutAt === -1 || m.index < cutAt)) cutAt = m.index;
  }
  if (cutAt < 0) return s;
  const prefix = s.slice(0, cutAt).replace(/[ \t\n]+$/, "");
  if (visibleRuneCount(prefix) < MIN_REPLY_VISIBLE) return s;
  return prefix;
}

// Trims marketing chrome (top banner, bottom footer/signature, separators) and
// quoted-reply tails from an already-rendered text body.
export function stripMailChrome(s: string): string {
  if (byteLen(s) < SHORT_BODY_FLOOR) return s;
  const original = s;

  // Phase 1: marketing chrome with a 75% safety abort.
  let stripped = stripMailPreamble(s);
  stripped = stripMailFooter(stripped);
  stripped = stripped.replace(MAIL_SEPARATOR_RE, "");
  stripped = stripped.replace(HTML_BLANK_RE, "\n\n");
  stripped = stripped.trim();
  s = byteLen(stripped) >= byteLen(original) / 4 ? stripped : original;

  // Phase 2: reply-quote tail — explicit markers, so this may cut >75%.
  return stripMailReplyQuote(s);
}

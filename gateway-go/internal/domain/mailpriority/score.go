// Package mailpriority scores inbox rows into glanceable priority tiers using
// cheap, local-only heuristics — no LLM call. The signals are tuned for the
// Korean business mail this deployment actually receives: deadline phrasing,
// award/official-notice vocabulary, money amounts, machine-sender demotion,
// and an optional VIP (address-book) lookup.
//
// Design intent (Stage 1 of the mail priority queue): the score must be cheap
// enough to run inline on every miniapp.gmail.list_recent row, so the native
// inbox can show a 🔴/🟡 marker without waiting on analysis. Deeper LLM-based
// triage stays in the analysis pipeline; this package only answers "should
// this row catch the eye first?".
package mailpriority

import (
	"regexp"
	"strings"
)

// Tier is the glanceable priority bucket for one mail row.
type Tier string

const (
	// TierUrgent marks act-now mail: deadlines, award/official notices,
	// payment problems. Rendered red in the native inbox.
	TierUrgent Tier = "urgent"
	// TierAttention marks business-relevant mail that needs eyes soon:
	// quotes, invoices, meeting requests, document asks. Rendered amber.
	TierAttention Tier = "attention"
	// TierNone is the default: routine or demoted (machine/noise) mail.
	TierNone Tier = ""
)

// Scorer evaluates mail rows. Construct once and reuse; Score is safe for
// concurrent use (regexes are package-level, vip must be thread-safe).
type Scorer struct {
	// vip reports whether a sender address belongs to the user's address
	// book. Nil disables the signal (tests, contacts store unavailable).
	vip func(email string) bool
}

// New returns a Scorer. vip may be nil.
func New(vip func(email string) bool) *Scorer {
	return &Scorer{vip: vip}
}

// Signal categories. Each category counts at most once so a keyword-stuffed
// subject cannot inflate itself into urgent on one signal class alone —
// urgent requires at least two strong categories (or one strong + two weak).
const (
	pointsUrgentKeyword = 3
	pointsUrgentStack   = 2
	pointsDeadline      = 3
	pointsAttention     = 2
	pointsMoney         = 2
	pointsVIP           = 2

	thresholdUrgent    = 5
	thresholdAttention = 2
)

var (
	// Machine senders: the local part of the from-address alone marks the
	// mail as routine (password links, newsletters, system notifications).
	machineSenderRe = regexp.MustCompile(`(?i)\b(no-?reply|noreply|donotreply|do-not-reply|newsletters?|notifications?|alerts?|mailer-daemon|bounce[s]?|marketing|promo)[^@]*@`)

	// Noise / security-link markers: advertising tags and login/verification
	// mail. These demote to TierNone outright — a "[광고]" subject with an
	// amount in it is still an ad.
	noiseRe = regexp.MustCompile(`(?i)\[광고\]|\(광고\)|수신\s*거부|unsubscribe|뉴스레터|구독\s*취소|보안\s*링크|로그인\s*(용|링크)|인증\s*(번호|코드|메일|링크)|verification|verify\s+your|password\s+reset|비밀번호\s*재설정`)

	// Act-now vocabulary: award notices, official documents, payment
	// problems, contract termination. One hit is +3.
	urgentKeywordRe = regexp.MustCompile(`긴급|낙찰|공문|입찰|독촉|연체|미납|체납|해지|위약|클레임|시정\s*(요구|명령)|통보`)

	// Deadline phrasing: an explicit date/day expression bound to
	// 까지/중/내, a D-day counter, or a reply-by ask. One hit is +3.
	deadlineRe = regexp.MustCompile(`(\d{1,2}\s*[/.월]\s*\d{1,2}\s*일?|오늘|금일|내일|명일|모레|금주|이번\s*주|주중)\s*(중|내|까지)|D-\d|회신\s*(요망|요청|바람|부탁)|제출\s*(요망|요청|기한)|회시`)

	// Business-action vocabulary: needs eyes but not necessarily today.
	// One hit is +2.
	attentionRe = regexp.MustCompile(`견적|발주|청구|세금\s*계산서|계약|입금|송금|지급|결제|정산|미팅|회의|방문|실사|점검|검토\s*요청|승인|결재|자료\s*(요청|제출)|보완|단가|인상|변경\s*요청|요청의\s*[건件]`)

	// Money amounts: a digit run followed by a Korean/Western currency
	// unit. +2 — amounts alongside an action keyword push toward urgent.
	moneyRe = regexp.MustCompile(`\d[\d,.]*\s*(억|천만|백만|만\s*원|원|달러|불|USD|KRW|₩)|[$₩]\s*\d`)

	// "Name <addr>" → addr; bare addresses pass through.
	addrRe = regexp.MustCompile(`<([^<>]+@[^<>]+)>`)
)

// Score rates one inbox row from its list-time fields (no body fetch) and
// returns the tier plus a short Korean hint naming the strongest signals —
// empty hint for TierNone.
func (s *Scorer) Score(from, subject, snippet string) (Tier, string) {
	text := subject + " " + snippet
	email := senderEmail(from)

	// Demotions win outright: machine senders and ad/security-link mail
	// are routine no matter what vocabulary they carry.
	if machineSenderRe.MatchString(from) || noiseRe.MatchString(text) {
		return TierNone, ""
	}

	score := 0
	var hints []string

	if kws := distinctMatches(urgentKeywordRe, text); len(kws) > 0 {
		score += pointsUrgentKeyword
		// A second distinct act-now keyword (낙찰+공문, 독촉+연체) is the
		// signature of an official notice — worth a stacking bonus so it
		// reaches urgent on its own, while one stray keyword stays amber.
		if len(kws) > 1 {
			score += pointsUrgentStack
		}
		hints = append(hints, kws[0])
	}
	if deadlineRe.MatchString(text) {
		score += pointsDeadline
		hints = append(hints, "마감 표현")
	}
	if m := attentionRe.FindString(text); m != "" {
		score += pointsAttention
		hints = append(hints, strings.TrimSpace(m))
	}
	if moneyRe.MatchString(text) {
		score += pointsMoney
		hints = append(hints, "금액")
	}
	// VIP amplifies content signals rather than initiating one: with 2.8K
	// synced contacts, "sender is in the address book" alone would mark
	// nearly every business row 🟡 and destroy the marker's glanceability
	// (observed on the live inbox). FYI mail from a VIP stays unmarked.
	if score > 0 && s.vip != nil && email != "" && s.vip(email) {
		score += pointsVIP
		hints = append(hints, "주요 연락처")
	}

	switch {
	case score >= thresholdUrgent:
		return TierUrgent, joinHints(hints)
	case score >= thresholdAttention:
		return TierAttention, joinHints(hints)
	default:
		return TierNone, ""
	}
}

// joinHints keeps the marker glanceable: at most two signal names.
func joinHints(h []string) string {
	if len(h) > 2 {
		h = h[:2]
	}
	return strings.Join(h, " · ")
}

// distinctMatches returns the deduped matches of re in text, in order.
func distinctMatches(re *regexp.Regexp, text string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range re.FindAllString(text, -1) {
		m = strings.TrimSpace(m)
		if m == "" || seen[m] {
			continue
		}
		seen[m] = true
		out = append(out, m)
	}
	return out
}

// senderEmail extracts the address from a "Name <addr>" header value,
// lowercased; returns "" when no address is present.
func senderEmail(from string) string {
	if m := addrRe.FindStringSubmatch(from); len(m) == 2 {
		return strings.ToLower(strings.TrimSpace(m[1]))
	}
	f := strings.TrimSpace(from)
	if strings.Contains(f, "@") && !strings.ContainsAny(f, " <>") {
		return strings.ToLower(f)
	}
	return ""
}

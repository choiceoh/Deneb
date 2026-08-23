package mailarchive

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type archiveQuery struct {
	Criteria string
	// Text is the normalized free-text part of the query. It is retained beside
	// the IMAP criteria so returned evidence can center the snippet on the match.
	Text          string
	DefaultView   bool
	HasAttachment bool
	InboxOnly     bool
	// SentSince/SentBefore are the half-open Date-header window for after:/before:
	// day-pager queries ([SentSince, SentBefore)). Kept beside Criteria so a
	// SENTSINCE/SENTBEFORE IMAP rejection can fall back to a broader SEARCH and
	// still post-filter rows to the requested day (see uidSearchSentAware).
	SentSince  time.Time
	SentBefore time.Time
	// Degraded is a non-empty reason when an unparseable operator was dropped and the
	// query fell back to a bounded recent view (instead of erroring). The caller logs
	// it — there is no Gmail fallback to silently absorb the mismatch anymore.
	Degraded string
}

// parseArchiveQuery maps a Gmail-style query into an IMAP search spec. It never
// errors: an operator the archive can't honor is dropped and the query degrades to a
// bounded recent view (spec.Degraded names the reason). With the Gmail fallback gone,
// hard-failing here would blank the inbox on an unfamiliar token — degrade instead.
func parseArchiveQuery(query string, now time.Time) archiveQuery {
	q := strings.TrimSpace(query)
	defaultView := isDefaultArchiveViewQuery(q)
	inboxOnly := inInboxRe.MatchString(q) && !defaultView
	if inAnywhereRe.MatchString(q) {
		inboxOnly = false
	}
	since := time.Time{}
	if defaultView {
		since = now.Add(-defaultNativeLookback)
	}
	// newer_than:Nd → SINCE now-N (lower bound). A malformed amount is ignored.
	if m := newerThanRe.FindStringSubmatch(q); m != nil {
		if d := relativeShift(now, m[1], m[2]); !d.IsZero() {
			since = d
		}
	}
	// older_than:Nd → BEFORE now-N (upper bound), symmetric with newer_than.
	until := time.Time{}
	if m := olderThanRe.FindStringSubmatch(q); m != nil {
		if d := relativeShift(now, m[1], m[2]); !d.IsZero() {
			until = d
		}
	}
	// Absolute date-range scoping for the day-pager (after:YYYY/M/D before:YYYY/M/D).
	// These bound the message's SENT date (Date: header), not its IMAP INTERNALDATE
	// (delivery time): the client buckets mail by the Date header, while INTERNALDATE
	// can cluster on one day (a bulk import delivers many sent-days at once), so
	// SINCE/BEFORE on INTERNALDATE matched nothing per day. SENTSINCE/SENTBEFORE match
	// the Date header. after: lower-bounds (inclusive), before: upper-bounds
	// (exclusive), so a per-day [after:D before:D+1] window = exactly D.
	sentSince := time.Time{}
	if m := afterDateRe.FindStringSubmatch(q); m != nil {
		if d, ok := parseArchiveQueryDate(m[1], m[2], m[3], now.Location()); ok {
			sentSince = d
		}
	}
	sentBefore := time.Time{}
	if m := beforeDateRe.FindStringSubmatch(q); m != nil {
		if d, ok := parseArchiveQueryDate(m[1], m[2], m[3], now.Location()); ok {
			sentBefore = d
		}
	}

	hasAttachment := hasAttachmentRe.MatchString(q)
	from := extractFromQuery(q)
	text := normalizeArchiveTextQuery(q)
	hasUnsupported := unsupportedOperatorRe.MatchString(text)
	text = unsupportedOperatorRe.ReplaceAllString(text, " ")
	text = strings.TrimSpace(strings.Join(strings.Fields(text), " "))

	// Graceful degradation: if the query is ONLY an operator we don't understand (no
	// usable date bound, sender, or free text), drop it and serve a bounded recent
	// view rather than an empty/errored list.
	degraded := ""
	if hasUnsupported && from == "" && text == "" && since.IsZero() && until.IsZero() && sentSince.IsZero() && sentBefore.IsZero() && !defaultView {
		since = now.Add(-defaultNativeLookback)
		degraded = "unsupported operator dropped; showing recent"
	}

	var parts []string
	if !since.IsZero() {
		parts = append(parts, "SINCE "+imapSinceDate(since))
	}
	if !until.IsZero() {
		parts = append(parts, "BEFORE "+imapSinceDate(until))
	}
	if !sentSince.IsZero() {
		parts = append(parts, "SENTSINCE "+imapSinceDate(sentSince))
	}
	if !sentBefore.IsZero() {
		parts = append(parts, "SENTBEFORE "+imapSinceDate(sentBefore))
	}
	switch {
	case from != "" && text != "":
		parts = append(parts, "FROM "+quote(from), "TEXT "+quote(text))
	case from != "":
		parts = append(parts, "FROM "+quote(from))
	case text != "":
		parts = append(parts, fmt.Sprintf("OR OR FROM %s SUBJECT %s TEXT %s", quote(text), quote(text), quote(text)))
	}
	if len(parts) == 0 {
		parts = append(parts, "ALL")
	}
	return archiveQuery{
		Criteria:      strings.Join(parts, " "),
		Text:          text,
		DefaultView:   defaultView,
		HasAttachment: hasAttachment,
		InboxOnly:     inboxOnly,
		SentSince:     sentSince,
		SentBefore:    sentBefore,
		Degraded:      degraded,
	}
}

// relativeShift returns now shifted back by N d/m/y, or the zero time for a
// malformed amount/unit (the caller then leaves that bound unset).
func relativeShift(now time.Time, nStr, unit string) time.Time {
	n, err := strconv.Atoi(nStr)
	if err != nil || n <= 0 {
		return time.Time{}
	}
	switch strings.ToLower(unit) {
	case "d":
		return now.Add(-time.Duration(n) * 24 * time.Hour)
	case "m":
		return now.AddDate(0, -n, 0)
	case "y":
		return now.AddDate(-n, 0, 0)
	}
	return time.Time{}
}

func isDefaultArchiveViewQuery(q string) bool {
	q = strings.TrimSpace(q)
	return q == "" ||
		strings.EqualFold(q, "{in:inbox is:unread} newer_than:30d") ||
		// Older native builds sent the previous default explicitly. Treat it as
		// the native recent view while still honoring its 7-day lookback.
		strings.EqualFold(q, "{in:inbox is:unread} newer_than:7d")
}

var (
	newerThanRe        = regexp.MustCompile(`(?i)\bnewer_than:(\d+)([dmy])\b`)
	olderThanRe        = regexp.MustCompile(`(?i)\bolder_than:(\d+)([dmy])\b`)
	fromQueryRe        = regexp.MustCompile(`(?i)\bfrom:(?:"([^"]+)"|([^\s}]+))`)
	hasAttachmentRe    = regexp.MustCompile(`(?i)\bhas:attachment\b`)
	inInboxRe          = regexp.MustCompile(`(?i)\bin:inbox\b`)
	inAnywhereRe       = regexp.MustCompile(`(?i)\bin:anywhere\b`)
	afterDateRe        = regexp.MustCompile(`(?i)\bafter:(\d{4})/(\d{1,2})/(\d{1,2})\b`)
	beforeDateRe       = regexp.MustCompile(`(?i)\bbefore:(\d{4})/(\d{1,2})/(\d{1,2})\b`)
	stripQuerySyntaxRe = regexp.MustCompile(`(?i)[{}]|\bin:(?:inbox|anywhere)\b|\bis:unread\b|\b(?:newer|older)_than:\d+[dmy]\b|\b(?:after|before):\d{4}/\d{1,2}/\d{1,2}\b|\bhas:attachment\b|\bfrom:(?:"[^"]+"|[^\s}]+)`)
	// newer_than/older_than and after:/before: are parsed into SINCE/BEFORE above, so
	// they are NOT unsupported. What remains are operators with no archive mapping.
	unsupportedOperatorRe = regexp.MustCompile(`(?i)\b(?:is|in|label|has|category):[^\s}]+`)
)

// parseArchiveQueryDate parses a Gmail-style YYYY/M/D token into a local-midnight
// time. ok=false for a malformed date — the caller then leaves that bound unset.
func parseArchiveQueryDate(yy, mm, dd string, loc *time.Location) (time.Time, bool) {
	y, err1 := strconv.Atoi(yy)
	mo, err2 := strconv.Atoi(mm)
	d, err3 := strconv.Atoi(dd)
	if err1 != nil || err2 != nil || err3 != nil || mo < 1 || mo > 12 || d < 1 || d > 31 {
		return time.Time{}, false
	}
	return time.Date(y, time.Month(mo), d, 0, 0, 0, 0, loc), true
}

func extractFromQuery(query string) string {
	m := fromQueryRe.FindStringSubmatch(query)
	if m == nil {
		return ""
	}
	if strings.TrimSpace(m[1]) != "" {
		return strings.TrimSpace(m[1])
	}
	return strings.TrimSpace(m[2])
}

func normalizeArchiveTextQuery(query string) string {
	text := stripQuerySyntaxRe.ReplaceAllString(query, " ")
	return strings.TrimSpace(strings.Join(strings.Fields(text), " "))
}

func archiveSearchMailboxes(configured []string, spec archiveQuery) []string {
	if !spec.InboxOnly {
		return configured
	}
	var out []string
	for _, mailbox := range configured {
		if strings.EqualFold(strings.TrimSpace(mailbox), "INBOX") {
			out = append(out, mailbox)
		}
	}
	return out
}

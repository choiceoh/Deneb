// person_conflicts.go — advisory scan for person pages whose listed emails
// disagree with a recent mail-analysis From. The recall lane surfaces the same
// mismatch inline as a ⚠ evidence marker (chat/recall/recall_mail_conflict.go,
// #4593) but only when that person happens to be recalled; this scan gives the
// discrepancy a durable decision surface (the wiki-maint feed card) without
// picking a winner — the stale side is repaired by the operator or the dreamer,
// never by this scan.
package wiki

import (
	"context"
	"net/mail"
	"regexp"
	"strings"
)

// PersonMailConflict is one detected wiki↔mail identity mismatch.
type PersonMailConflict struct {
	// PagePath is the 인물 page carrying the (possibly stale) emails.
	PagePath string
	// Title is the page title used to match mail analyses.
	Title string
	// WikiEmails are the page's listed canonical emails (fm ∪ body).
	WikiEmails []string
	// MailFrom is the disagreeing From address found in a mail analysis.
	MailFrom string
	// MailPath is the mail-analysis page that carried MailFrom.
	MailPath string
}

// personConflictFreemail mirrors the recall lane's freemail set: a freemail
// domain never counts as "the company domain both sides share", so a person
// whose wiki lists only freemail addresses conflicts with any new freemail
// From too.
var personConflictFreemail = map[string]bool{
	"gmail.com": true, "naver.com": true, "daum.net": true, "hanmail.net": true,
	"outlook.com": true, "hotmail.com": true, "icloud.com": true, "yahoo.com": true,
}

var personConflictEmailRe = regexp.MustCompile(`(?i)[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}`)

const (
	// personConflictScanMax bounds the scan (pages checked per run) and the
	// mail hits examined per person — the task is advisory, not exhaustive.
	personConflictScanMax  = 300
	personConflictHitsMax  = 3
	personConflictListMax  = 10
	personConflictTitleMin = 2
)

// PersonMailConflicts scans 인물 pages for wiki↔mail From mismatches, at most
// limit results. A page conflicts when a mail analysis about that person shows
// a From that is neither one of the page's listed emails nor on a domain the
// page already owns (non-freemail). Same rule as the recall marker — kept as a
// domain method so the feed card and the recall evidence cannot drift apart on
// what counts as a conflict.
func (s *Store) PersonMailConflicts(ctx context.Context, limit int) []PersonMailConflict {
	if s == nil {
		return nil
	}
	if limit <= 0 || limit > personConflictListMax {
		limit = personConflictListMax
	}
	paths, err := s.ListPages("인물")
	if err != nil {
		return nil
	}
	var out []PersonMailConflict
	scanned := 0
	for _, path := range paths {
		if scanned >= personConflictScanMax || len(out) >= limit {
			break
		}
		if ctx.Err() != nil {
			return out
		}
		scanned++
		if conflict, ok := s.PersonMailConflictFor(ctx, path); ok {
			out = append(out, conflict)
		}
	}
	return out
}

// PersonMailConflictFor evaluates ONE page. Split out of the scan so the card
// lane can re-check the exact finding a posted card names — the scan is bounded
// (personConflictScanMax/ListMax), so a page missing from its output is not
// evidence that page is clean, and retiring a card on that basis would drop a
// live finding.
func (s *Store) PersonMailConflictFor(ctx context.Context, path string) (PersonMailConflict, bool) {
	if s == nil {
		return PersonMailConflict{}, false
	}
	page, err := s.ReadPage(path)
	if err != nil || page == nil || page.Meta.Archived {
		return PersonMailConflict{}, false
	}
	title := strings.TrimSpace(page.Meta.Title)
	emails := personConflictEmails(page)
	if title == "" || len([]rune(title)) < personConflictTitleMin || len(emails) == 0 {
		return PersonMailConflict{}, false
	}
	report, err := s.SearchWithOptions(ctx, title, personConflictHitsMax, QueryOptions{ExcludeFactResults: true})
	if err != nil {
		return PersonMailConflict{}, false
	}
	for _, hit := range report.Results {
		if !IsMailAnalysisPath(hit.Path) {
			continue
		}
		// Read the PAGE, never the search hit: hit.Content is a matching
		// snippet, so "the first address in it" is routinely a recipient or a
		// quoted address. That is what filed 강동민 against a 태한 RECIPIENT and
		// 김건호 against a mail 박종원 sent (live cards, 2026-08-25).
		mailPage, merr := s.ReadPage(hit.Path)
		if merr != nil || mailPage == nil {
			continue
		}
		// The analysis must be about this person AS THE SENDER, not merely
		// mention the name somewhere in the body.
		if !personConflictSenderIs(title, mailPage) {
			continue
		}
		from := personConflictFrom(mailPage.Body)
		if from == "" {
			continue
		}
		if personConflictMismatch(emails, from) {
			// The operator may have judged this exact address already ("that
			// 박준영 is a job applicant, not our 에이디에프 박준영"). Without a
			// durable answer the lane only had a 30-day snooze, so a homonym
			// came back every month forever (person_identity_ack.go).
			if identityEvidenceReviewed(page.Meta, []string{mailConflictAckToken(from)}) {
				continue
			}
			return PersonMailConflict{
				PagePath:   path,
				Title:      title,
				WikiEmails: emails,
				MailFrom:   from,
				MailPath:   hit.Path,
			}, true
		}
	}
	return PersonMailConflict{}, false
}

// personConflictEmails collects the page's canonical emails: frontmatter ∪ body.
func personConflictEmails(page *Page) []string {
	seen := map[string]bool{}
	var out []string
	add := func(list []string) {
		for _, e := range list {
			e = strings.ToLower(strings.TrimSpace(e))
			if e == "" || seen[e] {
				continue
			}
			seen[e] = true
			out = append(out, e)
		}
	}
	add(page.Meta.Emails)
	add(personConflictEmailRe.FindAllString(page.Body, -1))
	return out
}

// personConflictAbout reports whether the mail text is about the person — the
// recall lane's mailAboutPerson rule.
func personConflictAbout(title, mailText string) bool {
	return strings.Contains(mailText, title)
}

// personConflictSenderIs reports whether the analysis is about mail this person
// SENT. Evidence is the sender line itself (the one carrying the From address)
// or the page summary (`"박종원" 메일 분석`) — never a mention anywhere in the
// body, which is how a person ended up "conflicting" with the address of
// whoever they were being discussed with.
func personConflictSenderIs(title string, mailPage *Page) bool {
	if mailPage == nil || strings.TrimSpace(title) == "" {
		return false
	}
	if strings.Contains(mailPage.Meta.Summary, title) {
		return true
	}
	return strings.Contains(personConflictSenderLine(mailPage.Body), title)
}

// personConflictFrom extracts the mail analysis's From address: a "From:" /
// "**발신**" line if present, else the first email in the text.
func personConflictFrom(mailText string) string {
	return personConflictParseAddress(personConflictSenderLine(mailText))
}

// personConflictSenderLine returns the first line that both looks like a sender
// header and actually carries an address. Returning the LINE (not just the
// address) lets the sender check and the address agree on one piece of evidence.
//
// There is deliberately no "first address anywhere" fallback: an analysis with
// no parsable From header says nothing about who sent it, and guessing filed
// real cards against recipients (2026-08-25).
func personConflictSenderLine(mailText string) string {
	for _, line := range strings.Split(mailText, "\n") {
		trim := strings.TrimSpace(line)
		lower := strings.ToLower(trim)
		if strings.HasPrefix(lower, "> from:") || strings.HasPrefix(lower, "from:") ||
			strings.Contains(lower, "**발신**") || strings.HasPrefix(lower, "발신") {
			if personConflictParseAddress(trim) != "" {
				return trim
			}
		}
	}
	return ""
}

func personConflictParseAddress(raw string) string {
	raw = strings.TrimSpace(raw)
	if addr, err := mail.ParseAddress(raw); err == nil && addr.Address != "" {
		return strings.ToLower(strings.TrimSpace(addr.Address))
	}
	if found := personConflictEmailRe.FindAllString(raw, 1); len(found) > 0 {
		return strings.ToLower(found[0])
	}
	return ""
}

// personConflictMismatch is the conflict rule: the From is neither listed on
// the page nor on a non-freemail domain the page already owns.
func personConflictMismatch(listed []string, from string) bool {
	from = strings.ToLower(strings.TrimSpace(from))
	if from == "" {
		return false
	}
	owned := map[string]bool{}
	for _, e := range listed {
		if e == from {
			return false
		}
		if d := personConflictDomain(e); d != "" && !personConflictFreemail[d] {
			owned[d] = true
		}
	}
	if d := personConflictDomain(from); d != "" && owned[d] {
		return false
	}
	return true
}

func personConflictDomain(addr string) string {
	at := strings.LastIndex(addr, "@")
	if at < 0 || at+1 >= len(addr) {
		return ""
	}
	return strings.ToLower(addr[at+1:])
}

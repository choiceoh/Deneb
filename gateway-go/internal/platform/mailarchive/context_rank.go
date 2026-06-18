package mailarchive

import (
	"regexp"
	"sort"
	"strings"
	"time"
)

var (
	projectDeadlineSignalRe = regexp.MustCompile(`(?i)(deadline|due|eta|milestone|schedule|meeting|review|approve|submit|기한|마감|납기|일정|회의|미팅|방문|실사|검토|승인|제출|회신|확인)`)
	projectMoneySignalRe    = regexp.MustCompile(`(?i)(₩|￦|\bkrw\b|\busd\b|견적|금액|단가|청구|입금|세금계산서|invoice|payment|quote|estimate|[0-9][0-9,\.]*\s*(원|만원|억원|달러))`)
)

func rankProjectMessages(query string, msgs []ContextMessage) []ContextMessage {
	if len(msgs) == 0 {
		return msgs
	}
	query = strings.ToLower(strings.TrimSpace(query))
	terms := rankTerms(query)
	now := time.Now()
	out := append([]ContextMessage(nil), msgs...)
	for i := range out {
		msg := &out[i]
		score := msg.Score
		if score <= 0 {
			score = 1
		}
		subject := strings.ToLower(msg.Subject)
		participants := strings.ToLower(strings.Join(contextParticipants(*msg), " "))
		body := strings.ToLower(msg.Snippet + "\n" + msg.Body)
		hay := subject + "\n" + participants + "\n" + body

		if query != "" {
			if strings.Contains(subject, query) {
				score += 8
				msg.RankReasons = appendRankReason(msg.RankReasons, "subject")
			} else if allRankTermsMatch(subject, terms) {
				score += 5
				msg.RankReasons = appendRankReason(msg.RankReasons, "subject_terms")
			}
			if strings.Contains(participants, query) {
				score += 3
				msg.RankReasons = appendRankReason(msg.RankReasons, "participant")
			}
			if strings.Contains(body, query) {
				score += 2
				msg.RankReasons = appendRankReason(msg.RankReasons, "body")
			}
		}
		if len(msg.Attachments) > 0 {
			score += 1.2
			msg.RankReasons = appendRankReason(msg.RankReasons, "attachment")
			if attachmentMatchesQuery(*msg, query, terms) {
				score += 1.5
				msg.RankReasons = appendRankReason(msg.RankReasons, "attachment_match")
			}
		}
		if projectDeadlineSignalRe.MatchString(hay) {
			score += 1.4
			msg.RankReasons = appendRankReason(msg.RankReasons, "deadline_or_action")
		}
		if projectMoneySignalRe.MatchString(hay) {
			score += 1.4
			msg.RankReasons = appendRankReason(msg.RankReasons, "money")
		}
		if len(msg.References) > 0 || strings.TrimSpace(msg.MessageID) != "" {
			score += 0.4
			msg.RankReasons = appendRankReason(msg.RankReasons, "thread_headers")
		}
		if !msg.when.IsZero() {
			boost := recencyBoost(now, msg.when)
			if boost > 0 {
				score += boost
				msg.RankReasons = appendRankReason(msg.RankReasons, "recent")
			}
		}
		msg.Score = score
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		if !out[i].when.Equal(out[j].when) {
			return out[i].when.After(out[j].when)
		}
		return out[i].Locator < out[j].Locator
	})
	return out
}

func rankTerms(query string) []string {
	fields := strings.Fields(strings.ToLower(query))
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		f = strings.Trim(f, `"'()[]{}<>:;,.!?`)
		if len([]rune(f)) < 2 {
			continue
		}
		out = append(out, f)
	}
	if len(out) == 0 && strings.TrimSpace(query) != "" {
		out = append(out, strings.TrimSpace(query))
	}
	return out
}

func allRankTermsMatch(hay string, terms []string) bool {
	if len(terms) == 0 {
		return false
	}
	for _, term := range terms {
		if !strings.Contains(hay, term) {
			return false
		}
	}
	return true
}

func attachmentMatchesQuery(msg ContextMessage, query string, terms []string) bool {
	if query == "" && len(terms) == 0 {
		return false
	}
	var b strings.Builder
	for _, att := range msg.Attachments {
		b.WriteString(" ")
		b.WriteString(strings.ToLower(att.Filename))
		b.WriteString(" ")
		b.WriteString(strings.ToLower(att.MimeType))
	}
	hay := b.String()
	if query != "" && strings.Contains(hay, query) {
		return true
	}
	return allRankTermsMatch(hay, terms)
}

func recencyBoost(now, when time.Time) float64 {
	if now.IsZero() || when.IsZero() || when.After(now.Add(24*time.Hour)) {
		return 0
	}
	age := now.Sub(when)
	switch {
	case age <= 14*24*time.Hour:
		return 2
	case age <= 45*24*time.Hour:
		return 1.2
	case age <= 120*24*time.Hour:
		return 0.6
	default:
		return 0
	}
}

func appendRankReason(reasons []string, reason string) []string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return reasons
	}
	for _, existing := range reasons {
		if existing == reason {
			return reasons
		}
	}
	return append(reasons, reason)
}

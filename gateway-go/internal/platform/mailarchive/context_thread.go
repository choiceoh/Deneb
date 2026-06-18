package mailarchive

import (
	"context"
	"strings"
	"time"
)

func searchThreadFallbackMatches(ctx context.Context, c *imapConn, cfg Config, seed ContextMessage, opts ContextOptions, limit int) []ContextMessage {
	if limit <= 0 {
		return nil
	}
	query := threadFallbackQuery(seed.Subject)
	if query == "" {
		return nil
	}
	scanLimit := maxInt(20, limit*8)
	if scanLimit > maxContextLimit {
		scanLimit = maxContextLimit
	}
	searchOpts := opts
	searchOpts.Limit = scanLimit
	msgs, err := searchContextMessagesLimited(ctx, c, cfg, archiveTextCriteria(query), searchOpts, true, scanLimit)
	if err != nil {
		return nil
	}
	out := make([]ContextMessage, 0, minInt(limit, len(msgs)))
	seedKey := contextMessageDedupeKey(seed)
	for _, msg := range msgs {
		if contextMessageDedupeKey(msg) == seedKey {
			continue
		}
		if !threadFallbackRelated(seed, msg) {
			continue
		}
		out = append(out, msg)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func threadFallbackQuery(subject string) string {
	subject = strings.TrimSpace(normalizeProjectSubject(subject))
	if subject == "" {
		return ""
	}
	runes := []rune(subject)
	if len(runes) > 120 {
		subject = string(runes[:120])
	}
	return subject
}

func threadFallbackRelated(seed, candidate ContextMessage) bool {
	seedSubject := normalizeProjectSubject(seed.Subject)
	candidateSubject := normalizeProjectSubject(candidate.Subject)
	if seedSubject == "" || candidateSubject == "" {
		return false
	}
	if seedSubject != candidateSubject &&
		!strings.Contains(seedSubject, candidateSubject) &&
		!strings.Contains(candidateSubject, seedSubject) {
		return false
	}
	if participantOverlap(seed, candidate) {
		return true
	}
	return datesWithin(seed.when, candidate.when, 75*24*time.Hour)
}

func participantOverlap(a, b ContextMessage) bool {
	seen := map[string]bool{}
	for _, p := range contextParticipants(a) {
		key := strings.ToLower(strings.TrimSpace(p))
		if key == "" {
			continue
		}
		seen[key] = true
		if domain := addressDomain(key); domain != "" {
			seen["@"+domain] = true
		}
	}
	for _, p := range contextParticipants(b) {
		key := strings.ToLower(strings.TrimSpace(p))
		if key == "" {
			continue
		}
		if seen[key] {
			return true
		}
		if domain := addressDomain(key); domain != "" && seen["@"+domain] {
			return true
		}
	}
	return false
}

func datesWithin(a, b time.Time, d time.Duration) bool {
	if a.IsZero() || b.IsZero() {
		return false
	}
	if a.After(b) {
		return a.Sub(b) <= d
	}
	return b.Sub(a) <= d
}

func addressDomain(addr string) string {
	addr = strings.ToLower(strings.TrimSpace(addr))
	at := strings.LastIndexByte(addr, '@')
	if at < 0 || at == len(addr)-1 {
		return ""
	}
	domain := strings.Trim(addr[at+1:], " >)")
	if domain == "" || domain == "gmail.com" || domain == "googlemail.com" || domain == "example.com" {
		return ""
	}
	return domain
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

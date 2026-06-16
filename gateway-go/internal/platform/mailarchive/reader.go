package mailarchive

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/lmtpd"
)

// Summary is a compact view of an archived message for the agent-facing tool.
type Summary struct {
	From    string `json:"from"`
	Subject string `json:"subject"`
	Date    string `json:"date"`
	Snippet string `json:"snippet"` // leading text of the body, for triage
}

const summarySnippetRunes = 1200

// ListSince returns messages in mailbox received on/after since, most-recent
// first, capped at limit. Used by the daily-digest agent to read the day's mail
// from the archive instead of Gmail.
func ListSince(ctx context.Context, cfg Config, mailbox string, since time.Time, limit int) ([]Summary, error) {
	return readSummaries(ctx, cfg, mailbox, fmt.Sprintf("SINCE %s", imapSinceDate(since)), limit)
}

// Search returns messages in mailbox matching a free-text query (matched against
// From, Subject and body text), most-recent first, capped at limit.
func Search(ctx context.Context, cfg Config, mailbox, query string, limit int) ([]Summary, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, fmt.Errorf("empty query")
	}
	// OR across From / Subject / body text so a keyword (sender, company,
	// project) matches however it appears.
	criteria := fmt.Sprintf(`OR OR FROM %s SUBJECT %s TEXT %s`, quote(q), quote(q), quote(q))
	return readSummaries(ctx, cfg, mailbox, criteria, limit)
}

func readSummaries(ctx context.Context, cfg Config, mailbox, criteria string, limit int) ([]Summary, error) {
	if mailbox == "" {
		mailbox = "INBOX"
	}
	if limit <= 0 || limit > 200 {
		limit = 60
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultTimeout
	}

	c, err := dialIMAP(ctx, cfg.Addr, cfg.Timeout)
	if err != nil {
		return nil, err
	}
	defer c.close()
	if err := c.login(cfg.User, cfg.Pass); err != nil {
		return nil, err
	}
	defer c.logout()
	if err := c.examine(mailbox); err != nil {
		return nil, fmt.Errorf("examine %q: %w", mailbox, err)
	}

	uids, err := c.uidSearch(criteria)
	if err != nil {
		return nil, err
	}
	if len(uids) == 0 {
		return nil, nil
	}
	// Highest UIDs are the most-recent arrivals; take the last `limit`.
	if len(uids) > limit {
		uids = uids[len(uids)-limit:]
	}

	bodies, err := c.uidFetchBodies(strings.Join(uids, ","))
	if err != nil {
		return nil, err
	}
	out := make([]Summary, 0, len(bodies))
	for _, b := range bodies {
		d, perr := lmtpd.ParseDetail(b)
		if perr != nil {
			continue
		}
		out = append(out, Summary{
			From:    d.From,
			Subject: d.Subject,
			Date:    d.Date,
			Snippet: clampRunes(strings.TrimSpace(d.Body), summarySnippetRunes),
		})
	}
	// Reverse to most-recent-first (UID order is ascending).
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

func clampRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

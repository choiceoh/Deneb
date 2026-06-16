package mailarchive

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/lmtpd"
)

// Default tuning. These mirror the Gmail-path caps in gmailpoll so the LLM thread
// extractor sees a comparable amount of context regardless of source.
const (
	defaultMaxThread    = 10
	defaultMaxSender    = 5
	defaultMaxFetch     = 18 // hard cap on bodies fetched per incoming message
	defaultSenderWindow = 30 * 24 * time.Hour
	defaultTimeout      = 15 * time.Second
)

// Config configures a Source. Mailboxes are searched in order; INBOX (ongoing
// auto-archived mail) then Gmail (historical backfill) is the natural order.
type Config struct {
	Addr      string // host:port of the archive IMAP server
	User      string
	Pass      string
	Mailboxes []string
	Timeout   time.Duration
}

// Source reads related mail from the on-box archive IMAP store. It is the local
// replacement for the Gmail thread/search the analysis pipeline used to do, so
// the LMTP ingest path gets thread context with no Gmail dependency.
type Source struct {
	cfg          Config
	maxThread    int
	maxSender    int
	maxFetch     int
	senderWindow time.Duration
}

// New builds a Source. Returns nil if no address is configured (the pipeline then
// proceeds without archive thread context — graceful no-op).
func New(cfg Config) *Source {
	if strings.TrimSpace(cfg.Addr) == "" {
		return nil
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultTimeout
	}
	if len(cfg.Mailboxes) == 0 {
		cfg.Mailboxes = []string{"INBOX", "Gmail"}
	}
	return &Source{
		cfg:          cfg,
		maxThread:    defaultMaxThread,
		maxSender:    defaultMaxSender,
		maxFetch:     defaultMaxFetch,
		senderWindow: defaultSenderWindow,
	}
}

// RelatedMessages returns prior emails related to msg — the thread ancestors
// (matched by the References/In-Reply-To Message-IDs) and the sender's recent
// history — parsed from the archive. The message itself is excluded by
// Message-ID. Best-effort: a connection/auth failure returns an error and the
// caller proceeds without thread context.
func (s *Source) RelatedMessages(ctx context.Context, msg *gmail.MessageDetail) ([]*gmail.MessageDetail, error) {
	c, err := dialIMAP(ctx, s.cfg.Addr, s.cfg.Timeout)
	if err != nil {
		return nil, err
	}
	defer c.close()
	if err := c.login(s.cfg.User, s.cfg.Pass); err != nil {
		return nil, err
	}
	defer c.logout()

	selfID := normalizeMsgID(msg.MessageIDHeader)
	sender := extractAddr(msg.From)
	since := imapSinceDate(time.Now().Add(-s.senderWindow))

	limit := s.maxThread + s.maxSender
	seen := map[string]bool{}
	var out []*gmail.MessageDetail

	for _, mbox := range s.cfg.Mailboxes {
		if ctx.Err() != nil || len(out) >= limit {
			break
		}
		if err := c.examine(mbox); err != nil {
			continue // mailbox may not exist on this account; skip
		}

		var threadUIDs, senderUIDs []string
		// 1) Thread ancestors: one search per referenced Message-ID.
		for _, ref := range msg.References {
			found, serr := c.uidSearch(fmt.Sprintf(`HEADER "Message-ID" %s`, quote(ref)))
			if serr == nil {
				threadUIDs = append(threadUIDs, found...)
			}
		}
		// 2) Recent history from the same sender.
		if sender != "" {
			found, serr := c.uidSearch(fmt.Sprintf(`FROM %s SINCE %s`, quote(sender), since))
			if serr == nil {
				senderUIDs = append(senderUIDs, found...)
			}
		}

		uids := pickRelatedUIDs(threadUIDs, senderUIDs, s.maxThread, s.maxSender, s.maxFetch)
		if len(uids) == 0 {
			continue
		}

		bodies, ferr := c.uidFetchBodies(strings.Join(uids, ","))
		if ferr != nil {
			continue
		}
		for _, b := range bodies {
			d, perr := lmtpd.ParseDetail(b)
			if perr != nil {
				continue
			}
			id := normalizeMsgID(d.MessageIDHeader)
			if id != "" && id == selfID {
				continue // exclude the message being analyzed
			}
			if id != "" {
				if seen[id] {
					continue
				}
				seen[id] = true
			}
			out = append(out, d)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

// pickRelatedUIDs combines thread-ancestor and sender-history UIDs with SEPARATE
// caps so a chatty sender can't crowd out the actual thread (the earlier code
// concatenated both then tail-truncated, which dropped the thread ancestors —
// the most relevant context — whenever sender history was large). Thread
// ancestors keep the front (parseRefIDs puts the direct parent / In-Reply-To
// first); sender history keeps the most recent (UID SEARCH returns ascending).
// maxFetch is a final safety bound that preserves the thread-first order.
func pickRelatedUIDs(threadUIDs, senderUIDs []string, maxThread, maxSender, maxFetch int) []string {
	threadUIDs = dedupStrings(threadUIDs)
	senderUIDs = dedupStrings(senderUIDs)
	if maxThread > 0 && len(threadUIDs) > maxThread {
		threadUIDs = threadUIDs[:maxThread] // nearest ancestors first
	}
	if maxSender > 0 && len(senderUIDs) > maxSender {
		senderUIDs = senderUIDs[len(senderUIDs)-maxSender:] // most-recent sender mail
	}
	combined := make([]string, 0, len(threadUIDs)+len(senderUIDs))
	combined = append(combined, threadUIDs...)
	combined = append(combined, senderUIDs...)
	uids := dedupStrings(combined)
	if maxFetch > 0 && len(uids) > maxFetch {
		uids = uids[:maxFetch] // keep the front (thread ancestors), not the tail
	}
	return uids
}

// --- helpers ---

func normalizeMsgID(id string) string {
	return strings.ToLower(strings.TrimSpace(id))
}

var addrRe = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)

// extractAddr pulls the bare email address out of a From header value.
func extractAddr(from string) string {
	return addrRe.FindString(from)
}

// imapSinceDate formats a time as the IMAP date form "02-Jan-2006".
func imapSinceDate(t time.Time) string {
	return t.Format("02-Jan-2006")
}

func dedupStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

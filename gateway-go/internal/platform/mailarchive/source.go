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
	defaultMaxThread     = 10
	defaultMaxSender     = 5
	defaultMaxFetch      = 18 // hard cap on bodies fetched per incoming message
	defaultMaxReferences = 20 // bound per-message HEADER searches on long threads
	defaultSenderWindow  = 30 * 24 * time.Hour
	defaultTimeout       = 15 * time.Second
)

// Config configures a Source. Mailboxes are searched in order; INBOX is ongoing
// auto-archived mail, and the second mailbox is the historical backfill
// (legacy name: Gmail, neutral target name: Archive).
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
	cfg           Config
	maxThread     int
	maxSender     int
	maxFetch      int
	maxReferences int
	senderWindow  time.Duration
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
		cfg.Mailboxes = DefaultMailboxes()
	}
	return &Source{
		cfg:           cfg,
		maxThread:     defaultMaxThread,
		maxSender:     defaultMaxSender,
		maxFetch:      defaultMaxFetch,
		maxReferences: defaultMaxReferences,
		senderWindow:  defaultSenderWindow,
	}
}

// RelatedMessages returns prior emails related to msg — the thread ancestors
// (matched by the References/In-Reply-To Message-IDs) and the sender's recent
// history — parsed from the archive. The message itself is excluded by
// Message-ID, or by a conservative fallback for malformed mail without
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
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			c.close()
		case <-done:
		}
	}()
	defer close(done)

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

		var threadUIDs []string
		// 1) Thread ancestors: one search per referenced Message-ID.
		for _, ref := range capFirstStrings(msg.References, s.maxReferences) {
			if ctx.Err() != nil {
				break
			}
			found, serr := c.uidSearch(fmt.Sprintf(`HEADER "Message-ID" %s`, quote(ref)))
			if serr == nil {
				threadUIDs = append(threadUIDs, found...)
			}
		}
		// 2) Recent history from the same sender.
		var senderUIDs []string
		if sender != "" && ctx.Err() == nil {
			found, serr := c.uidSearch(fmt.Sprintf(`FROM %s SINCE %s`, quote(sender), since))
			if serr == nil {
				senderUIDs = append(senderUIDs, found...)
			}
		}

		uidGroups := prioritizedArchiveUIDGroups(threadUIDs, senderUIDs, s.maxThread, s.maxSender, s.maxFetch)
		if len(uidGroups) == 0 {
			continue
		}

		for _, uids := range uidGroups {
			if len(uids) == 0 || len(out) >= limit || ctx.Err() != nil {
				break
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
				if sameArchivedMessage(msg, d, selfID) {
					continue // exclude the message being analyzed
				}
				if key := archivedMessageDedupeKey(d); key != "" {
					if seen[key] {
						continue
					}
					seen[key] = true
				}
				out = append(out, d)
				if len(out) >= limit {
					break
				}
			}
		}
	}
	return out, nil
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

func prioritizedArchiveUIDGroups(threadUIDs, senderUIDs []string, maxThread, maxSender, maxFetch int) [][]string {
	thread := capFirstStrings(dedupStrings(threadUIDs), maxThread)
	sender := subtractStrings(dedupStrings(senderUIDs), thread)
	sender = capLastStrings(sender, maxSender)
	if maxFetch > 0 && len(thread)+len(sender) > maxFetch {
		if len(thread) >= maxFetch {
			thread = capFirstStrings(thread, maxFetch)
			sender = nil
		} else {
			sender = capLastStrings(sender, maxFetch-len(thread))
		}
	}
	var groups [][]string
	if len(thread) > 0 {
		groups = append(groups, thread)
	}
	if len(sender) > 0 {
		groups = append(groups, sender)
	}
	return groups
}

func capFirstStrings(in []string, n int) []string {
	if n <= 0 {
		return nil
	}
	if len(in) <= n {
		return in
	}
	return in[:n]
}

func capLastStrings(in []string, n int) []string {
	if n <= 0 {
		return nil
	}
	if len(in) <= n {
		return in
	}
	return in[len(in)-n:]
}

func subtractStrings(in, remove []string) []string {
	if len(in) == 0 || len(remove) == 0 {
		return in
	}
	blocked := map[string]struct{}{}
	for _, value := range remove {
		blocked[value] = struct{}{}
	}
	out := make([]string, 0, len(in))
	for _, value := range in {
		if _, ok := blocked[value]; ok {
			continue
		}
		out = append(out, value)
	}
	return out
}

func sameArchivedMessage(current, archived *gmail.MessageDetail, currentMsgID string) bool {
	if current == nil || archived == nil {
		return false
	}
	if currentMsgID == "" {
		currentMsgID = normalizeMsgID(current.MessageIDHeader)
	}
	archivedID := normalizeMsgID(archived.MessageIDHeader)
	if currentMsgID != "" || archivedID != "" {
		return currentMsgID != "" && archivedID != "" && currentMsgID == archivedID
	}
	// Some real-world mail lacks Message-ID. If the archive has already received
	// the same delivery, same-sender history can otherwise feed the current mail
	// back into its own "previous context". Require Date plus body equality so a
	// repeated subject from the same sender is not accidentally removed.
	currentKey := fallbackArchivedMessageKey(current)
	return currentKey != "" && currentKey == fallbackArchivedMessageKey(archived)
}

func archivedMessageDedupeKey(msg *gmail.MessageDetail) string {
	if msg == nil {
		return ""
	}
	if id := normalizeMsgID(msg.MessageIDHeader); id != "" {
		return "message-id:" + id
	}
	if key := fallbackArchivedMessageKey(msg); key != "" {
		return "fallback:" + key
	}
	return ""
}

func fallbackArchivedMessageKey(msg *gmail.MessageDetail) string {
	from := comparableHeader(msg.From)
	date := strings.TrimSpace(msg.Date)
	body := comparableBody(msg.Body)
	if from == "" || date == "" || body == "" {
		return ""
	}
	return strings.Join([]string{
		from,
		comparableHeader(msg.Subject),
		date,
		body,
	}, "\x00")
}

func comparableHeader(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func comparableBody(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

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

// Default tuning. These mirror the Gmail-path caps in mailanalysis so the LLM thread
// extractor sees a comparable amount of context regardless of source.
const (
	defaultMaxThread = 10
	// Sender history: 10 messages over 90 days (operator decision, 2026-08-29).
	// A 30-day window is shorter than the deals it has to explain — an EPC
	// thread runs for months, so the exchange that frames a new message often
	// sits outside a month.
	defaultMaxSender = 10
	// Must cover maxThread + maxSender or prioritizedArchiveUIDGroups silently
	// trims the SENDER group to fit (thread wins the tie): at 18 a 10+10 config
	// would deliver 8 sender messages while reporting 10.
	defaultMaxFetch      = 20 // hard cap on bodies fetched per incoming message
	defaultMaxReferences = 20 // bound per-message HEADER searches on long threads
	defaultSenderWindow  = 90 * 24 * time.Hour
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
	c, closeSession, err := s.openArchiveSession(ctx)
	if err != nil {
		return nil, err
	}
	defer closeSession()

	return s.collectRelatedMessages(ctx, c, msg), nil
}

// openArchiveSession owns the IMAP connection lifecycle for a related-message
// lookup. Closing the socket interrupts an in-flight IMAP command when the
// caller cancels its context; the returned cleanup still attempts LOGOUT before
// finally closing the connection.
func (s *Source) openArchiveSession(ctx context.Context) (*imapConn, func(), error) {
	c, err := dialIMAP(ctx, s.cfg.Addr, s.cfg.Timeout)
	if err != nil {
		return nil, nil, err
	}
	if err := c.login(s.cfg.User, s.cfg.Pass); err != nil {
		c.close()
		return nil, nil, err
	}
	stopCancellationClose := context.AfterFunc(ctx, c.close)
	closeSession := func() {
		stopCancellationClose()
		c.logout()
		c.close()
	}
	return c, closeSession, nil
}

func (s *Source) collectRelatedMessages(ctx context.Context, c *imapConn, msg *gmail.MessageDetail) []*gmail.MessageDetail {
	selfID := normalizeMsgID(msg.MessageIDHeader)
	sender := extractAddr(msg.From)
	// Date-header window (SENTSINCE), not INTERNALDATE — same skew as the
	// list path; see archiveSentSinceCriteria. Under-delivering here silently
	// starves the analysis prompt's "이전 메일 맥락".
	sinceCriteria := archiveSentSinceCriteria(time.Now().Add(-s.senderWindow))

	limit := s.maxThread + s.maxSender
	collector := newRelatedMessageCollector(msg, selfID, limit)

	for _, mbox := range s.cfg.Mailboxes {
		if ctx.Err() != nil || collector.full() {
			break
		}
		if err := c.examine(mbox); err != nil {
			continue // mailbox may not exist on this account; skip
		}
		uidGroups := s.findRelatedUIDGroups(ctx, c, msg.References, sender, sinceCriteria)
		collector.fetchAndAppend(ctx, c, uidGroups)
	}
	return collector.messages
}

func (s *Source) findRelatedUIDGroups(
	ctx context.Context,
	c *imapConn,
	references []string,
	sender string,
	sinceCriteria string,
) [][]string {
	threadUIDs := s.findThreadUIDs(ctx, c, references)
	senderUIDs := findSenderUIDs(ctx, c, sender, sinceCriteria)
	return prioritizedArchiveUIDGroups(threadUIDs, senderUIDs, s.maxThread, s.maxSender, s.maxFetch)
}

func (s *Source) findThreadUIDs(ctx context.Context, c *imapConn, references []string) []string {
	var threadUIDs []string
	for _, ref := range capFirstStrings(references, s.maxReferences) {
		if ctx.Err() != nil {
			break
		}
		found, err := c.uidSearch(fmt.Sprintf(`HEADER "Message-ID" %s`, quote(ref)))
		if err == nil {
			threadUIDs = append(threadUIDs, found...)
		}
	}
	return threadUIDs
}

func findSenderUIDs(ctx context.Context, c *imapConn, sender, sinceCriteria string) []string {
	if sender == "" || ctx.Err() != nil {
		return nil
	}
	found, err := c.uidSearchSentAware(fmt.Sprintf(`FROM %s %s`, quote(sender), sinceCriteria))
	if err != nil {
		return nil
	}
	return found
}

type relatedMessageCollector struct {
	current  *gmail.MessageDetail
	selfID   string
	limit    int
	seen     map[string]bool
	messages []*gmail.MessageDetail
}

func newRelatedMessageCollector(current *gmail.MessageDetail, selfID string, limit int) *relatedMessageCollector {
	return &relatedMessageCollector{
		current: current,
		selfID:  selfID,
		limit:   limit,
		seen:    map[string]bool{},
	}
}

func (c *relatedMessageCollector) full() bool {
	return len(c.messages) >= c.limit
}

func (c *relatedMessageCollector) fetchAndAppend(ctx context.Context, conn *imapConn, uidGroups [][]string) {
	for _, uids := range uidGroups {
		if len(uids) == 0 || c.full() || ctx.Err() != nil {
			break
		}
		bodies, err := conn.uidFetchBodies(strings.Join(uids, ","))
		if err != nil {
			continue
		}
		c.appendBodies(bodies)
	}
}

func (c *relatedMessageCollector) appendBodies(bodies [][]byte) {
	for _, body := range bodies {
		detail, err := lmtpd.ParseDetail(body)
		if err != nil {
			continue
		}
		if c.append(detail) && c.full() {
			break
		}
	}
}

func (c *relatedMessageCollector) append(detail *gmail.MessageDetail) bool {
	if sameArchivedMessage(c.current, detail, c.selfID) {
		return false // exclude the message being analyzed
	}
	if key := archivedMessageDedupeKey(detail); key != "" {
		if c.seen[key] {
			return false
		}
		c.seen[key] = true
	}
	c.messages = append(c.messages, detail)
	return true
}

// --- helpers ---

func normalizeMsgID(id string) string {
	id = strings.TrimSpace(id)
	// References commonly carry RFC 5322 angle brackets while callers often
	// paste the bare Message-ID. Treat both spellings as the same identity.
	if len(id) >= 2 && id[0] == '<' && id[len(id)-1] == '>' {
		id = strings.TrimSpace(id[1 : len(id)-1])
	}
	return strings.ToLower(id)
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

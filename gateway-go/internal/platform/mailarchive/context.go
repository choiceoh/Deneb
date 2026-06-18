package mailarchive

import (
	"context"
	"fmt"
	"net/mail"
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/lmtpd"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailbody"
)

const (
	defaultContextLimit      = 40
	maxContextLimit          = 200
	defaultContextIndexLimit = 500
	maxContextIndexLimit     = 1000
	defaultBodyRunes         = 2400
)

// ContextOptions tunes agent-facing archive context reads.
type ContextOptions struct {
	Mailboxes []string
	Since     time.Time
	Limit     int
	// IndexLimit is the bounded recent-message window used for local full-text
	// project lookups. It may be larger than the returned Limit.
	IndexLimit int
	BodyRunes  int
}

// ContextMessage is the stable, agent-facing view of an archived message.
// Locator is always fetchable from the native archive even when Message-ID is
// absent or awkward for a model to copy.
type ContextMessage struct {
	ID          string                 `json:"id"`
	Locator     string                 `json:"locator"`
	Mailbox     string                 `json:"mailbox"`
	UID         string                 `json:"uid"`
	MessageID   string                 `json:"message_id,omitempty"`
	References  []string               `json:"references,omitempty"`
	From        string                 `json:"from,omitempty"`
	To          string                 `json:"to,omitempty"`
	CC          string                 `json:"cc,omitempty"`
	Subject     string                 `json:"subject,omitempty"`
	Date        string                 `json:"date,omitempty"`
	Body        string                 `json:"body,omitempty"`
	Snippet     string                 `json:"snippet,omitempty"`
	Attachments []gmail.AttachmentInfo `json:"attachments,omitempty"`
	Score       float64                `json:"score,omitempty"`
	RankReasons []string               `json:"rank_reasons,omitempty"`

	when time.Time
}

// ProjectHistory is a timeline plus coarse thread clusters for a project query.
type ProjectHistory struct {
	Query     string           `json:"query"`
	Messages  []ContextMessage `json:"messages"`
	Threads   []ProjectThread  `json:"threads"`
	IndexUsed bool             `json:"index_used,omitempty"`
}

// ProjectThread groups project-history hits that share the same normalized
// subject, giving the agent handles for drilling into a full thread.
type ProjectThread struct {
	Key          string   `json:"key"`
	Subject      string   `json:"subject"`
	Count        int      `json:"count"`
	FirstDate    string   `json:"first_date,omitempty"`
	LastDate     string   `json:"last_date,omitempty"`
	Participants []string `json:"participants,omitempty"`
	Locators     []string `json:"locators,omitempty"`
}

// ReadContextMessage resolves an archive locator, Message-ID-derived id, or
// descriptive query into one cleaned message for agent consumption.
func ReadContextMessage(ctx context.Context, cfg Config, messageID, query string, opts ContextOptions) (ContextMessage, error) {
	c, err := connectArchive(ctx, cfg)
	if err != nil {
		return ContextMessage{}, err
	}
	defer c.close()
	defer c.logout()
	return resolveContextSeed(ctx, c, cfg, strings.TrimSpace(messageID), strings.TrimSpace(query), opts)
}

// ListContextMessages returns recent archive messages from the configured
// mailboxes with stable locators, newest first.
func ListContextMessages(ctx context.Context, cfg Config, since time.Time, opts ContextOptions) ([]ContextMessage, error) {
	criteria := "ALL"
	if !since.IsZero() {
		criteria = "SINCE " + imapSinceDate(since)
	}
	c, err := connectArchive(ctx, cfg)
	if err != nil {
		return nil, err
	}
	defer c.close()
	defer c.logout()
	return searchContextMessages(ctx, c, cfg, criteria, opts, true)
}

// SearchContextMessages searches archive messages with stable locators, newest
// first.
func SearchContextMessages(ctx context.Context, cfg Config, query string, opts ContextOptions) ([]ContextMessage, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("search query is required")
	}
	c, err := connectArchive(ctx, cfg)
	if err != nil {
		return nil, err
	}
	defer c.close()
	defer c.logout()
	return searchContextMessages(ctx, c, cfg, archiveTextCriteria(query), opts, true)
}

// ThreadContext reconstructs the whole available archive thread around one
// message by following Message-ID, References, and In-Reply-To headers across
// the configured mailboxes. Returned messages are chronological.
func ThreadContext(ctx context.Context, cfg Config, messageID, query string, opts ContextOptions) ([]ContextMessage, error) {
	limit := clampContextLimit(opts.Limit)
	c, err := connectArchive(ctx, cfg)
	if err != nil {
		return nil, err
	}
	defer c.close()
	defer c.logout()

	seed, err := resolveContextSeed(ctx, c, cfg, strings.TrimSpace(messageID), strings.TrimSpace(query), opts)
	if err != nil {
		return nil, err
	}

	var out []ContextMessage
	seenMessages := map[string]bool{}
	queuedIDs := map[string]bool{}
	var queue []string
	enqueue := func(id string) {
		id = strings.TrimSpace(id)
		key := normalizeMsgID(id)
		if key == "" || queuedIDs[key] {
			return
		}
		queuedIDs[key] = true
		queue = append(queue, id)
	}
	add := func(msg ContextMessage) {
		key := contextMessageDedupeKey(msg)
		if key != "" && !seenMessages[key] {
			seenMessages[key] = true
			out = append(out, msg)
		}
		enqueue(msg.MessageID)
		for _, ref := range msg.References {
			enqueue(ref)
		}
	}
	add(seed)

	for len(queue) > 0 && len(out) < limit {
		id := queue[0]
		queue = queue[1:]
		for _, match := range searchThreadHeaderMatches(ctx, c, cfg, id, opts) {
			if len(out) >= limit {
				break
			}
			add(match)
		}
	}

	if len(out) < limit {
		for _, match := range searchThreadFallbackMatches(ctx, c, cfg, seed, opts, limit-len(out)) {
			if len(out) >= limit {
				break
			}
			add(match)
		}
	}

	sortContextMessages(out, true)
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

// ProjectHistoryContext searches a project/company/person keyword across the
// native archive and returns a chronological timeline plus thread clusters.
func ProjectHistoryContext(ctx context.Context, cfg Config, query string, opts ContextOptions) (ProjectHistory, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return ProjectHistory{}, fmt.Errorf("project history query is required")
	}
	c, err := connectArchive(ctx, cfg)
	if err != nil {
		return ProjectHistory{}, err
	}
	defer c.close()
	defer c.logout()

	limit := clampContextLimit(opts.Limit)
	candidateLimit := projectHistoryCandidateLimit(limit, opts.IndexLimit)
	indexOpts := opts
	indexOpts.IndexLimit = candidateLimit
	indexOpts.Limit = candidateLimit

	indexUsed := false
	idx, indexErr := buildContextIndex(ctx, c, cfg, indexOpts)
	var msgs []ContextMessage
	if indexErr == nil && idx != nil {
		msgs = idx.Search(query, candidateLimit)
		indexUsed = len(msgs) > 0
	}
	if len(msgs) == 0 {
		criteria := archiveTextCriteria(query)
		if !opts.Since.IsZero() {
			criteria = "SINCE " + imapSinceDate(opts.Since) + " " + criteria
		}
		fallbackOpts := opts
		fallbackOpts.Limit = candidateLimit
		msgs, err = searchContextMessagesLimited(ctx, c, cfg, criteria, fallbackOpts, false, candidateLimit)
		if err != nil {
			return ProjectHistory{}, err
		}
	}
	msgs = rankProjectMessages(query, msgs)
	if len(msgs) > limit {
		msgs = msgs[:limit]
	}
	sortContextMessages(msgs, true)
	return ProjectHistory{Query: query, Messages: msgs, Threads: clusterProjectThreads(msgs), IndexUsed: indexUsed}, nil
}

func connectArchive(ctx context.Context, cfg Config) (*imapConn, error) {
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultTimeout
	}
	c, err := dialIMAP(ctx, cfg.Addr, cfg.Timeout)
	if err != nil {
		return nil, err
	}
	if err := c.login(cfg.User, cfg.Pass); err != nil {
		c.close()
		return nil, err
	}
	return c, nil
}

func resolveContextSeed(ctx context.Context, c *imapConn, cfg Config, messageID, query string, opts ContextOptions) (ContextMessage, error) {
	if messageID != "" {
		if mailbox, uid, ok := archiveLocatorParts(messageID); ok {
			return fetchContextUID(ctx, c, mailbox, uid, opts)
		}
		if msg, ok := searchContextByMessageID(ctx, c, cfg, messageID, opts); ok {
			return msg, nil
		}
		return ContextMessage{}, ErrArchiveNotFound
	}
	if query == "" {
		return ContextMessage{}, fmt.Errorf("message_id or query is required")
	}
	msgs, err := searchContextMessages(ctx, c, cfg, archiveTextCriteria(query), ContextOptions{
		Mailboxes: opts.Mailboxes,
		Limit:     1,
		BodyRunes: opts.BodyRunes,
	}, true)
	if err != nil {
		return ContextMessage{}, err
	}
	if len(msgs) == 0 {
		return ContextMessage{}, ErrArchiveNotFound
	}
	return msgs[0], nil
}

func searchContextByMessageID(ctx context.Context, c *imapConn, cfg Config, messageID string, opts ContextOptions) (ContextMessage, bool) {
	candidates := []string{messageID}
	if !strings.HasPrefix(messageID, "<") {
		candidates = append(candidates, "<"+messageID+">")
	}
	for _, candidate := range candidates {
		for _, mailbox := range contextMailboxes(cfg, opts.Mailboxes) {
			if ctx.Err() != nil {
				return ContextMessage{}, false
			}
			if err := c.examine(mailbox); err != nil {
				continue
			}
			uids, err := c.uidSearch(`HEADER "Message-ID" ` + quote(candidate))
			if err != nil || len(uids) == 0 {
				continue
			}
			msg, err := fetchContextUIDAfterExamine(ctx, c, mailbox, uids[len(uids)-1], opts)
			if err == nil {
				return msg, true
			}
		}
	}
	return ContextMessage{}, false
}

func searchThreadHeaderMatches(ctx context.Context, c *imapConn, cfg Config, messageID string, opts ContextOptions) []ContextMessage {
	var out []ContextMessage
	headers := []string{"Message-ID", "References", "In-Reply-To"}
	for _, mailbox := range contextMailboxes(cfg, opts.Mailboxes) {
		if ctx.Err() != nil {
			return out
		}
		if err := c.examine(mailbox); err != nil {
			continue
		}
		for _, header := range headers {
			uids, err := c.uidSearch(fmt.Sprintf(`HEADER %s %s`, quote(header), quote(messageID)))
			if err != nil || len(uids) == 0 {
				continue
			}
			msgs, err := fetchContextUIDsAfterExamine(ctx, c, mailbox, uids, opts)
			if err == nil {
				out = append(out, msgs...)
			}
		}
	}
	return out
}

func searchContextMessages(ctx context.Context, c *imapConn, cfg Config, criteria string, opts ContextOptions, newestFirst bool) ([]ContextMessage, error) {
	limit := clampContextLimit(opts.Limit)
	return searchContextMessagesLimited(ctx, c, cfg, criteria, opts, newestFirst, limit)
}

func searchContextMessagesLimited(ctx context.Context, c *imapConn, cfg Config, criteria string, opts ContextOptions, newestFirst bool, limit int) ([]ContextMessage, error) {
	if limit <= 0 {
		limit = defaultContextLimit
	}
	seen := map[string]bool{}
	var out []ContextMessage
	for _, mailbox := range contextMailboxes(cfg, opts.Mailboxes) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err := c.examine(mailbox); err != nil {
			continue
		}
		uids, err := c.uidSearch(criteria)
		if err != nil {
			continue
		}
		if len(uids) > limit {
			uids = uids[len(uids)-limit:]
		}
		msgs, err := fetchContextUIDsAfterExamine(ctx, c, mailbox, uids, opts)
		if err != nil {
			continue
		}
		for _, msg := range msgs {
			key := contextMessageDedupeKey(msg)
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, msg)
		}
	}
	sortContextMessages(out, true)
	if newestFirst {
		reverseContextMessages(out)
	}
	if len(out) > limit {
		if newestFirst {
			out = out[:limit]
		} else {
			out = out[len(out)-limit:]
		}
	}
	return out, nil
}

func fetchContextUID(ctx context.Context, c *imapConn, mailbox, uid string, opts ContextOptions) (ContextMessage, error) {
	for _, candidate := range lookupMailboxCandidates(mailbox) {
		if ctx.Err() != nil {
			return ContextMessage{}, ctx.Err()
		}
		if err := c.examine(candidate); err != nil {
			continue
		}
		msg, err := fetchContextUIDAfterExamine(ctx, c, candidate, uid, opts)
		if err == nil {
			return msg, nil
		}
	}
	return ContextMessage{}, ErrArchiveNotFound
}

func fetchContextUIDAfterExamine(ctx context.Context, c *imapConn, mailbox, uid string, opts ContextOptions) (ContextMessage, error) {
	msgs, err := fetchContextUIDsAfterExamine(ctx, c, mailbox, []string{uid}, opts)
	if err != nil {
		return ContextMessage{}, err
	}
	if len(msgs) == 0 {
		return ContextMessage{}, ErrArchiveNotFound
	}
	return msgs[0], nil
}

func fetchContextUIDsAfterExamine(ctx context.Context, c *imapConn, mailbox string, uids []string, opts ContextOptions) ([]ContextMessage, error) {
	if len(uids) == 0 {
		return nil, nil
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	msgs, err := c.uidFetchMessages(strings.Join(uids, ","))
	if err != nil {
		return nil, err
	}
	out := make([]ContextMessage, 0, len(msgs))
	for _, msg := range msgs {
		uid := strings.TrimSpace(msg.UID)
		if uid == "" {
			continue
		}
		parsed, err := lmtpd.ParseMessage(msg.Raw, archiveLocator(mailbox, uid))
		if err != nil || parsed == nil || parsed.Detail == nil {
			continue
		}
		out = append(out, contextMessageFromDetail(mailbox, uid, parsed.Detail, opts.BodyRunes))
	}
	return out, nil
}

func contextMessageFromDetail(mailbox, uid string, detail *gmail.MessageDetail, bodyRunes int) ContextMessage {
	body := strings.TrimSpace(mailbody.CleanForAnalysis(detail.Body))
	if bodyRunes <= 0 {
		bodyRunes = defaultBodyRunes
	}
	body = clampRunes(body, bodyRunes)
	locator := archiveLocator(mailbox, uid)
	id := strings.TrimSpace(detail.ID)
	if id == "" {
		id = locator
	}
	return ContextMessage{
		ID:          id,
		Locator:     locator,
		Mailbox:     mailbox,
		UID:         uid,
		MessageID:   strings.TrimSpace(detail.MessageIDHeader),
		References:  append([]string(nil), detail.References...),
		From:        detail.From,
		To:          detail.To,
		CC:          detail.CC,
		Subject:     detail.Subject,
		Date:        detail.Date,
		Body:        body,
		Snippet:     snippetFromBody(detail.Body),
		Attachments: append([]gmail.AttachmentInfo(nil), detail.Attachments...),
		when:        parseMailDate(detail.Date),
	}
}

func contextMailboxes(cfg Config, override []string) []string {
	src := override
	if len(src) == 0 {
		src = cfg.Mailboxes
	}
	if len(src) == 0 {
		src = DefaultMailboxes()
	}
	out := make([]string, 0, len(src))
	seen := map[string]bool{}
	for _, mailbox := range src {
		mailbox = strings.TrimSpace(mailbox)
		if mailbox == "" {
			continue
		}
		key := strings.ToLower(mailbox)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, mailbox)
	}
	return out
}

func archiveTextCriteria(query string) string {
	return fmt.Sprintf(`OR OR FROM %s SUBJECT %s TEXT %s`, quote(query), quote(query), quote(query))
}

func clampContextLimit(limit int) int {
	if limit <= 0 {
		return defaultContextLimit
	}
	if limit > maxContextLimit {
		return maxContextLimit
	}
	return limit
}

func clampContextIndexLimit(limit int) int {
	if limit <= 0 {
		return defaultContextIndexLimit
	}
	if limit > maxContextIndexLimit {
		return maxContextIndexLimit
	}
	return limit
}

func projectHistoryCandidateLimit(returnLimit, indexLimit int) int {
	if indexLimit > 0 {
		return clampContextIndexLimit(indexLimit)
	}
	limit := returnLimit * 8
	if limit < 200 {
		limit = 200
	}
	return clampContextIndexLimit(limit)
}

func contextMessageDedupeKey(msg ContextMessage) string {
	if id := normalizeMsgID(msg.MessageID); id != "" {
		return "mid:" + id
	}
	if id := strings.TrimSpace(msg.ID); id != "" {
		return "id:" + id
	}
	return "loc:" + msg.Locator
}

func sortContextMessages(msgs []ContextMessage, chronological bool) {
	sort.SliceStable(msgs, func(i, j int) bool {
		a, b := msgs[i], msgs[j]
		if !a.when.Equal(b.when) {
			if chronological {
				return a.when.Before(b.when)
			}
			return a.when.After(b.when)
		}
		if a.Mailbox != b.Mailbox {
			return a.Mailbox < b.Mailbox
		}
		return parseUID(a.UID) < parseUID(b.UID)
	})
}

func reverseContextMessages(msgs []ContextMessage) {
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
}

func clusterProjectThreads(msgs []ContextMessage) []ProjectThread {
	type acc struct {
		thread       ProjectThread
		participants map[string]bool
	}
	groups := map[string]*acc{}
	var order []string
	for _, msg := range msgs {
		key := normalizeProjectSubject(msg.Subject)
		if key == "" {
			key = strings.ToLower(strings.TrimSpace(msg.Subject))
		}
		if key == "" {
			key = msg.Locator
		}
		g := groups[key]
		if g == nil {
			g = &acc{thread: ProjectThread{Key: key, Subject: msg.Subject}, participants: map[string]bool{}}
			groups[key] = g
			order = append(order, key)
		}
		g.thread.Count++
		if g.thread.FirstDate == "" {
			g.thread.FirstDate = msg.Date
		}
		g.thread.LastDate = msg.Date
		if len(g.thread.Locators) < 5 {
			g.thread.Locators = append(g.thread.Locators, msg.Locator)
		}
		for _, participant := range contextParticipants(msg) {
			if !g.participants[participant] {
				g.participants[participant] = true
				g.thread.Participants = append(g.thread.Participants, participant)
			}
		}
	}
	out := make([]ProjectThread, 0, len(groups))
	for _, key := range order {
		out = append(out, groups[key].thread)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].LastDate > out[j].LastDate
	})
	return out
}

func normalizeProjectSubject(subject string) string {
	s := strings.TrimSpace(subject)
	for {
		lower := strings.ToLower(s)
		next := s
		for _, prefix := range []string{"re:", "fw:", "fwd:", "re：", "fw：", "[외부메일]", "[외부 메일]", "[external]"} {
			if strings.HasPrefix(lower, strings.ToLower(prefix)) {
				next = strings.TrimSpace(s[len(prefix):])
				break
			}
		}
		if next == s {
			break
		}
		s = next
	}
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

func contextParticipants(msg ContextMessage) []string {
	var out []string
	seen := map[string]bool{}
	add := func(raw string) {
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			addr := extractParticipantAddress(part)
			if addr == "" {
				addr = part
			}
			key := strings.ToLower(addr)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, addr)
		}
	}
	add(msg.From)
	add(msg.To)
	add(msg.CC)
	if len(out) > 8 {
		return out[:8]
	}
	return out
}

func extractParticipantAddress(raw string) string {
	if addr, err := mail.ParseAddress(raw); err == nil && addr.Address != "" {
		return addr.Address
	}
	return extractAddr(raw)
}

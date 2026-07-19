package workfeed

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/embedindex"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

const (
	SourceProactive       = "proactive"
	SourceMailReport      = "mail_report"    // proactive mail analysis — gets the envelope card icon
	SourceMeetingReport   = "meeting_report" // proactive Plaud recording analysis (plaud_recordings.go)
	SourceDream           = "dream"          // wiki dream-cycle result card (pages created/updated)
	SourceCaptureImage    = "capture_image"
	SourceCaptureAudio    = "capture_audio"
	SourceCaptureDocument = "capture_document"
	SourceCaptureContacts = "capture_contacts"
	// SourceDocAnalysis is an agent-published deliverable card: the result of a
	// user-requested document/contract analysis, posted to the feed via the
	// workfeed(action="publish") tool so the deliverable is a trackable card, not
	// only a wiki page + an ephemeral chat summary. Renders with the generic file
	// glyph (native sourcePainter else-branch — no icon map change needed).
	SourceDocAnalysis = "doc_analysis"
	// SourceGroupwareApproval is an Amaranth e-approval card with 승인/반려 chips.
	SourceGroupwareApproval = "groupware-approval" //nolint:gosec // feed-source kind label, not a credential
	// SourceGroupwareBoard is an important Amaranth notice surfaced read-only.
	SourceGroupwareBoard = "groupware-board"

	StatusUnread  = "unread"
	StatusAcked   = "acked"
	StatusSnoozed = "snoozed"

	ActionOpen     = "open"
	ActionFollowUp = "followup"
	ActionSnooze   = "snooze"
	ActionAck      = "ack"
	// ActionAnswer settles a question card AND returns the chosen option as a
	// prompt the native sends to the asking session — the chip path for proactive
	// choice questions. (deal_question keeps ActionAck: it records server-side via
	// OnAnswer and wants no extra chat turn.)
	ActionAnswer = "answer"
	// ActionTrash permanently deletes a card. It is a UNIVERSAL action handled in
	// RunAction before the per-item action lookup, so it works on every card —
	// including legacy items and captures whose stored action list predates it —
	// without a feed-wide migration. The native client renders it as 휴지통.
	ActionTrash = "trash"
	// ActionMark runs an action's side effect WITHOUT settling the card, so a
	// card offering several marks (a morning letter's per-deadline "완료" long-
	// press) lets the operator handle each without the whole card vanishing.
	// The tapped action is stamped done; the card stays for the rest.
	ActionMark = "mark"

	// Priority levels — higher surfaces first in the feed. Inferred from the
	// item's urgency markers/keywords when the caller doesn't set one, so the
	// feed reads like a chief-of-staff briefing (what's urgent first) instead of
	// a reverse-chronological log.
	PriorityLow    = 1
	PriorityNormal = 2
	PriorityHigh   = 3
	PriorityUrgent = 4
)

var (
	ErrNotFound          = errors.New("workfeed item not found")
	ErrActionNotFound    = errors.New("workfeed action not found")
	ErrInvalidEscalation = errors.New("invalid workfeed escalation")
)

// snoozeDuration is how long a snoozed work-feed item stays hidden before it
// re-surfaces. "나중에" (snooze) defers an item for "later today" rather than
// dismissing it like "완료" (ack); List brings it back near the top once this
// window elapses, restoring the distinction between the two actions.
const snoozeDuration = 3 * time.Hour

// Action describes an operator action available on a work-feed item.
type Action struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Label  string `json:"label"`
	Status string `json:"status,omitempty"`
	Prompt string `json:"prompt,omitempty"`
}

// Item is one durable work-feed entry and its acknowledgement state.
type Item struct {
	ID         string `json:"id"`
	Source     string `json:"source"`
	Title      string `json:"title"`
	Summary    string `json:"summary,omitempty"`
	Body       string `json:"body,omitempty"`
	SessionKey string `json:"sessionKey,omitempty"`
	RefType    string `json:"refType,omitempty"`
	RefID      string `json:"refId,omitempty"`
	// Metadata carries source-specific machine-readable action context. It
	// keeps stable identifiers and measurements out of localized display prose.
	Metadata map[string]string `json:"metadata,omitempty"`
	// ClusterID and RelatedIDs are advisory semantic grouping metadata. Every
	// card remains independently persisted/actionable; grouping never dedupes,
	// acknowledges, or hides an item.
	ClusterID  string   `json:"clusterId,omitempty"`
	RelatedIDs []string `json:"relatedIds,omitempty"`
	Status     string   `json:"status"`
	Priority   int      `json:"priority,omitempty"`
	// Question marks a card the agent is asking the user to answer (a deal-team
	// question, or a proactive turn that posed a question / offered ```choices).
	// The native renders such a card with inline answer chips (from Actions) plus a
	// free-text reply: chips run as work-feed actions (ActionAnswer/ActionAck); the
	// reply field sends to the card's SessionKey and acks the card.
	Question    bool     `json:"question,omitempty"`
	Actions     []Action `json:"actions,omitempty"`
	CreatedAtMs int64    `json:"createdAtMs"`
	UpdatedAtMs int64    `json:"updatedAtMs"`
	// SnoozedUntilMs, when set, is the wall-clock time a snoozed item re-surfaces.
	SnoozedUntilMs int64 `json:"snoozedUntilMs,omitempty"`
	// ReadAtMs, when set, is when the user first opened (read) the card. Read is a
	// SOFTER signal than 완료(Ack): the card stays in the feed (Status unchanged) but
	// the clients render it de-emphasized. 0 = unread.
	ReadAtMs int64 `json:"readAtMs,omitempty"`
}

// ActionResult records the outcome of running an item action.
type ActionResult struct {
	Item           Item   `json:"item"`
	Action         Action `json:"action"`
	SessionKey     string `json:"sessionKey,omitempty"`
	Prompt         string `json:"prompt,omitempty"`
	Message        string `json:"message,omitempty"`
	RemoveFromFeed bool   `json:"removeFromFeed,omitempty"`
}

// ActionEffect runs a source-specific durable side effect before a terminal
// action is persisted. Returning an error leaves the card unsettled so the
// operator can retry instead of losing the decision.
type ActionEffect func(item Item, action Action) error

// Store persists work-feed items and action outcomes.
//
// Lock hierarchy (acquire in this order; never reverse):
//
//	appendMu -> mu
//
// The semantic index has its own independent lock. No Store lock is held while
// embedding or while closing the index.
type Store struct {
	path     string
	appendMu sync.Mutex
	mu       sync.Mutex
	semantic *embedindex.Index
}

// NewStore opens a work-feed store backed by path.
func NewStore(path string) *Store {
	return &Store{path: path}
}

// SetEmbedder enables non-destructive semantic grouping for newly appended
// cards. Existing cards are embedded lazily on the next append and cached next
// to the feed file. A nil/unhealthy embedder preserves the exact old behavior.
func (s *Store) SetEmbedder(embedder embedindex.Embedder, opts ...embedindex.Option) {
	if s == nil {
		return
	}
	s.appendMu.Lock()
	defer s.appendMu.Unlock()
	var next *embedindex.Index
	if embedder != nil {
		opts = append(opts, embedindex.WithPreprocessingFingerprint(workFeedSemanticPreprocessingVersion))
		next = embedindex.New("workfeed", embedder, workFeedSemanticCachePath(s.path), opts...)
	}
	s.mu.Lock()
	previous := s.semantic
	s.semantic = next
	s.mu.Unlock()
	if previous != nil {
		previous.Close()
	}
}

// Close stops an optional semantic refresh. Feed mutations are synchronously
// persisted and need no additional flush.
func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	s.appendMu.Lock()
	defer s.appendMu.Unlock()
	s.mu.Lock()
	semantic := s.semantic
	s.semantic = nil
	s.mu.Unlock()
	if semantic != nil {
		semantic.Close()
	}
	return nil
}

func workFeedSemanticCachePath(feedPath string) string {
	if strings.TrimSpace(feedPath) == "" {
		return ""
	}
	ext := filepath.Ext(feedPath)
	return strings.TrimSuffix(feedPath, ext) + ".semantic.json"
}

// Append adds item to the feed and returns the stored item. Thin wrapper over
// AppendIfNew for callers that don't need the created flag.
func (s *Store) Append(item Item) (Item, error) {
	out, _, err := s.AppendIfNew(item)
	return out, err
}

// AppendIfNew adds item unless it duplicates the most recent card (same source +
// same body fingerprint). This guards against the same proactive analysis being
// re-emitted — e.g. by a restart catch-up — and piling up as a duplicate card.
// On a duplicate it writes nothing and returns the existing card with
// created=false; callers (native sync) then skip the "created" event. Otherwise
// it returns the stored item with created=true.
func (s *Store) AppendIfNew(item Item) (Item, bool, error) {
	s.appendMu.Lock()
	defer s.appendMu.Unlock()

	item = normalizeNew(item)
	s.mu.Lock()
	// Load + rewrite so retention can bound the file. The feed is appended to
	// infrequently (once per proactive report / capture), so the O(n) rewrite is
	// cheap and keeps the file — and every List — from growing without bound as
	// the feed ages. Fall back to a plain append if the file can't be read, so a
	// transient read error never drops the new item (dedup is skipped on that
	// rare path — losing a card is worse than an occasional duplicate).
	items, err := jsonlstore.Load[Item](s.path)
	if err != nil {
		if aerr := jsonlstore.Append(s.path, item); aerr != nil {
			s.mu.Unlock()
			return Item{}, false, aerr
		}
		s.mu.Unlock()
		return item, true, nil
	}
	// Groupware cards are idempotent by durable Amaranth reference across their
	// independent ingestion paths. Scan all retained cards, not only the recent
	// body-fingerprint window; an acked card deliberately does not block a
	// genuinely reopened or republished entity.
	hasGroupwareRef := isGroupwareRefSource(item.Source) && item.RefID != ""
	if hasGroupwareRef {
		if existing, ok := findActiveBySourceRef(items, item.Source, item.RefID); ok {
			s.mu.Unlock()
			return existing, false, nil
		}
	}
	if !hasGroupwareRef {
		for i := len(items) - 1; i >= 0 && i >= len(items)-30; i-- {
			if isDuplicateCard(items[i], item) || isMeetingNearDuplicate(items[i], item) {
				s.mu.Unlock()
				return items[i], false, nil
			}
		}
	}
	semantic := s.semantic
	if semantic == nil || !semantic.Enabled() {
		items = append(items, item)
		items = pruneRetention(items)
		if err := jsonlstore.Snapshot(s.path, items); err != nil {
			s.mu.Unlock()
			return Item{}, false, err
		}
		s.mu.Unlock()
		return item, true, nil
	}
	semanticSnapshot := append([]Item(nil), items...)
	s.mu.Unlock()

	// Append has no caller context, so bound this advisory request explicitly.
	// A timeout only omits grouping; the card is still persisted below.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	relatedIDs := workFeedSemanticMatches(ctx, semantic, item, semanticSnapshot)
	cancel()

	// Actions/read state may have changed while embedding. Reload under mu so
	// the final snapshot preserves those mutations; appendMu prevents another
	// append from introducing a new duplicate in this window.
	s.mu.Lock()
	items, err = jsonlstore.Load[Item](s.path)
	if err != nil {
		if aerr := jsonlstore.Append(s.path, item); aerr != nil {
			s.mu.Unlock()
			return Item{}, false, aerr
		}
		s.mu.Unlock()
		return item, true, nil
	}
	item = applySemanticGroup(items, item, relatedIDs)
	items = append(items, item)
	items = pruneRetention(items)
	if err := jsonlstore.Snapshot(s.path, items); err != nil {
		s.mu.Unlock()
		return Item{}, false, err
	}
	s.mu.Unlock()
	return item, true, nil
}

func isGroupwareRefSource(source string) bool {
	return source == SourceGroupwareApproval || source == SourceGroupwareBoard
}

// isDuplicateCard reports whether cur duplicates prev: same source and the same
// non-empty body fingerprint. Only the body matters (title/priority ignored), so
// a re-emitted analysis dedupes even if its priority was re-inferred. An empty
// body never dedupes — distinct cards with no body (e.g. a capture whose OCR
// came back empty) must not collapse into one.
func isDuplicateCard(prev, cur Item) bool {
	if prev.Source != cur.Source {
		return false
	}
	fp := fingerprint(cur.Body)
	if fp == "" {
		return false
	}
	return fingerprint(prev.Body) == fp
}

// fingerprint normalizes a body for duplicate comparison: leading/trailing and
// internal whitespace runs collapse to single spaces, so newline/trailing-space
// differences don't defeat dedup while any real content difference is kept.
func fingerprint(body string) string {
	return strings.Join(strings.Fields(body), " ")
}

// FindActiveBySourceRef returns the newest retained non-acked card matching the
// source and stable reference identifier. Snoozed cards remain active.
func (s *Store) FindActiveBySourceRef(source, refID string) (Item, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	source = strings.TrimSpace(source)
	refID = strings.TrimSpace(refID)
	if source == "" || refID == "" {
		return Item{}, false, nil
	}
	items, err := jsonlstore.Load[Item](s.path)
	if err != nil {
		return Item{}, false, err
	}
	item, ok := findActiveBySourceRef(items, source, refID)
	return item, ok, nil
}

func findActiveBySourceRef(items []Item, source, refID string) (Item, bool) {
	for i := len(items) - 1; i >= 0; i-- {
		item := normalizeExisting(items[i])
		if item.Source == source && item.RefID == refID && item.Status != StatusAcked {
			return item, true
		}
	}
	return Item{}, false
}

// isMeetingNearDuplicate collapses AutoFlow mail_report and Plaud meeting_report
// cards that describe the same meeting (shared title + KST date fingerprint).
func isMeetingNearDuplicate(prev, cur Item) bool {
	if !meetingLikeSource(prev.Source) || !meetingLikeSource(cur.Source) {
		return false
	}
	a, ok1 := meetingCardFingerprint(prev)
	b, ok2 := meetingCardFingerprint(cur)
	return ok1 && ok2 && a == b
}

func meetingLikeSource(src string) bool {
	return src == SourceMeetingReport || src == SourceMailReport
}

func meetingCardFingerprint(it Item) (string, bool) {
	body := it.Body
	if body == "" {
		body = it.Title + "\n" + it.Summary
	}
	if i := strings.Index(body, "Plaud `"); i >= 0 {
		rest := body[i+len("Plaud `"):]
		if j := strings.Index(rest, "`"); j > 0 {
			return "plaud:" + rest[:j], true
		}
	}
	if i := strings.Index(body, "plaud:"); i >= 0 {
		rest := body[i+len("plaud:"):]
		end := 0
		for end < len(rest) {
			c := rest[end]
			if (c >= 'a' && c <= 'f') || (c >= '0' && c <= '9') || (c >= 'A' && c <= 'F') {
				end++
				continue
			}
			break
		}
		if end >= 8 {
			return "plaud:" + strings.ToLower(rest[:end]), true
		}
	}
	title := strings.TrimSpace(it.Title)
	if title == "" {
		for _, line := range strings.Split(body, "\n") {
			line = strings.TrimSpace(line)
			line = strings.TrimPrefix(line, "🎙")
			line = strings.TrimSpace(line)
			line = strings.TrimPrefix(line, "회의 분석:")
			line = strings.TrimSpace(line)
			if line != "" {
				title = line
				break
			}
		}
	}
	if title == "" {
		return "", false
	}
	date := ""
	for _, s := range []string{body, title} {
		for i := 0; i+10 <= len(s); i++ {
			chunk := s[i : i+10]
			if chunk[4] == '-' && chunk[7] == '-' && chunk[0] >= '0' && chunk[0] <= '9' {
				date = chunk
				break
			}
		}
		if date != "" {
			break
		}
	}
	key := strings.ToLower(strings.Join(strings.Fields(title), " "))
	if date != "" {
		return date + "|" + key, true
	}
	return key, true
}

// ListOptions controls work-feed filtering, ordering, and pagination.
type ListOptions struct {
	Limit        int
	IncludeAcked bool
	SinceMs      int64
	BeforeMs     int64
}

// List returns the newest items and total count under the acknowledgement filter.
func (s *Store) List(limit int, includeAcked bool) ([]Item, int, error) {
	return s.ListFiltered(ListOptions{Limit: limit, IncludeAcked: includeAcked})
}

// ListRange returns items within the requested timestamp window.
func (s *Store) ListRange(limit int, includeAcked bool, sinceMs, beforeMs int64) ([]Item, int, error) {
	return s.ListFiltered(ListOptions{Limit: limit, IncludeAcked: includeAcked, SinceMs: sinceMs, BeforeMs: beforeMs})
}

// ListFiltered returns items matching opts plus the count before limit truncation.
func (s *Store) ListFiltered(opts ListOptions) ([]Item, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	items, err := jsonlstore.Load[Item](s.path)
	if err != nil {
		return nil, 0, err
	}
	now := time.Now().UnixMilli()
	for i := range items {
		items[i] = normalizeExisting(items[i])
	}
	// A snoozed item whose window has elapsed sorts by its wake time, so it
	// re-surfaces near the top (fresh) instead of buried at its original slot.
	effectiveTime := func(it Item) int64 {
		if it.Status == StatusSnoozed && it.SnoozedUntilMs > 0 && it.SnoozedUntilMs <= now {
			return it.SnoozedUntilMs
		}
		return it.CreatedAtMs
	}
	// Priority first (urgent stays on top until handled), then recency / wake time.
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Priority != items[j].Priority {
			return items[i].Priority > items[j].Priority
		}
		ti, tj := effectiveTime(items[i]), effectiveTime(items[j])
		if ti == tj {
			return items[i].ID > items[j].ID
		}
		return ti > tj
	})

	filtered := make([]Item, 0, len(items))
	for _, item := range items {
		if !opts.IncludeAcked {
			if item.Status == StatusAcked {
				continue
			}
			if item.Status == StatusSnoozed {
				if item.SnoozedUntilMs > now {
					continue // still snoozed — hidden until the window elapses
				}
				item.Status = StatusUnread // snooze elapsed — re-surface as actionable
			}
		}
		if opts.SinceMs > 0 && item.CreatedAtMs < opts.SinceMs {
			continue
		}
		if opts.BeforeMs > 0 && item.CreatedAtMs >= opts.BeforeMs {
			continue
		}
		filtered = append(filtered, item)
	}
	total := len(filtered)
	if opts.Limit > 0 && len(filtered) > opts.Limit {
		filtered = filtered[:opts.Limit]
	}
	return filtered, total, nil
}

// EngagementStat summarizes how delivered proactive cards fared. A card the user
// acked or snoozed counts as engaged (they interacted — snooze is "later", not
// dismissed); a card still unread past the stale window counts as ignored
// (delivered, no engagement — the over-intervention / interruption-cost signal);
// a fresh unread card is pending (too new to judge). FTR is the over-intervention
// proxy from the proactive-agent literature (ProAgentBench precision = interruption
// cost): the fraction of judged cards that were ignored.
type EngagementStat struct {
	Total    int            `json:"total"`
	Engaged  int            `json:"engaged"`
	Ignored  int            `json:"ignored"`
	Pending  int            `json:"pending"`
	BySource map[string]int `json:"ignoredBySource"` // ignored count per card source
}

// FTR is the fraction of judged (non-pending) cards that were ignored. 0 when
// nothing has been judged yet.
func (e EngagementStat) FTR() float64 {
	judged := e.Engaged + e.Ignored
	if judged == 0 {
		return 0
	}
	return float64(e.Ignored) / float64(judged)
}

// Engagement rolls up the retained cards' engagement as of `now`, treating an
// unread card older than staleWindowMs as ignored. It reads raw stored status
// (snoozed stays engaged rather than re-surfacing as unread), so it conservatively
// under-counts ignored rather than flagging a just-resurfaced snooze. Reflects
// retained history only (old cards are pruned), so it is a recent-engagement view.
func (s *Store) Engagement(now, staleWindowMs int64) (EngagementStat, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	items, err := jsonlstore.Load[Item](s.path)
	if err != nil {
		return EngagementStat{BySource: map[string]int{}}, err
	}
	stat := EngagementStat{BySource: map[string]int{}}
	for _, it := range items {
		stat.Total++
		switch it.Status {
		case StatusAcked, StatusSnoozed:
			stat.Engaged++
		default: // unread (incl. legacy empty status)
			if staleWindowMs > 0 && now-it.CreatedAtMs > staleWindowMs {
				stat.Ignored++
				src := it.Source
				if src == "" {
					src = "unknown"
				}
				stat.BySource[src]++
			} else {
				stat.Pending++
			}
		}
	}
	return stat, nil
}

// Ack marks id acknowledged and returns its updated item.
func (s *Store) Ack(id string) (Item, error) {
	return s.mutateItem(id, func(item *Item, now int64) bool {
		item.Status = StatusAcked
		item.UpdatedAtMs = now
		return true
	})
}

// MarkRead stamps the card identified by id as read (ReadAtMs) WITHOUT settling it:
// the card stays in the feed and keeps its Status (unread), so "read" is a softer
// signal than 완료(Ack). Idempotent — once stamped, the first-read time is kept and
// the file is not rewritten on a repeat read. Applies to every item sharing the id
// (legacy twins), mirroring Ack. ReadAtMs flows to the clients (List + native sync),
// which render read cards de-emphasized.
func (s *Store) MarkRead(id string) (Item, error) {
	return s.mutateItem(id, func(item *Item, now int64) bool {
		if item.ReadAtMs != 0 {
			return false
		}
		item.ReadAtMs = now
		return true
	})
}

// Correct annotates the card identified by id with a user correction, appending
// note to the body as a dated "사용자 정정" erratum block and bumping UpdatedAtMs.
// The card stays in the feed, now visibly carrying the correction so a wrong
// analysis is never shown unqualified. Applies to every item sharing the id
// (legacy id twins), mirroring Ack. The durable knowledge fix (wiki) is the
// caller's separate agent turn; this is only the on-card erratum.
func (s *Store) Correct(id, note string) (Item, error) {
	note = strings.TrimSpace(note)
	return s.mutateItem(id, func(item *Item, now int64) bool {
		if note != "" {
			item.Body = strings.TrimRight(item.Body, "\n") + formatCorrection(note, now)
		}
		item.UpdatedAtMs = now
		return true
	})
}

// formatCorrection renders a user correction as a dated block appended to a card
// body, kept visually distinct from the original analysis by a rule + marker.
func formatCorrection(note string, atMs int64) string {
	date := time.UnixMilli(atMs).Format("2006-01-02")
	return "\n\n---\n\n✏️ **사용자 정정** (" + date + ")\n" + note
}

var approvalStaleNoteMarker = "\n\n---\n\n**결재 Radar:**"

// EscalateApproval raises an active approval card's priority and replaces its
// single bounded radar note. Repeated calls at the same/lower level are no-ops.
func (s *Store) EscalateApproval(id string, level int, ageLabel string) (Item, error) {
	id = strings.TrimSpace(id)
	ageLabel = strings.TrimSpace(ageLabel)
	if id == "" || level < 1 || level > 2 || ageLabel == "" {
		return Item{}, ErrInvalidEscalation
	}
	return s.mutateItem(id, func(item *Item, now int64) bool {
		if item.Source != SourceGroupwareApproval || item.Status == StatusAcked {
			return false
		}
		if item.Metadata == nil {
			item.Metadata = make(map[string]string)
		}
		previous, _ := strconv.Atoi(item.Metadata["groupwareEscalationLevel"])
		if previous >= level {
			return false
		}
		originalSummary := item.Metadata["groupwareOriginalSummary"]
		if originalSummary == "" {
			originalSummary = strings.TrimSpace(item.Summary)
			item.Metadata["groupwareOriginalSummary"] = originalSummary
		}
		item.Metadata["groupwareEscalationLevel"] = strconv.Itoa(level)
		item.Metadata["groupwareEscalationAge"] = ageLabel
		prefix := ageLabel + " 미결"
		if originalSummary != "" {
			item.Summary = prefix + " · " + originalSummary
		} else {
			item.Summary = prefix
		}
		if i := strings.Index(item.Body, approvalStaleNoteMarker); i >= 0 {
			item.Body = strings.TrimRight(item.Body[:i], "\n")
		}
		item.Body += approvalStaleNoteMarker + " " + prefix + ". 확인이 필요합니다."
		if level >= 2 {
			item.Priority = PriorityUrgent
		} else {
			item.Priority = PriorityHigh
		}
		item.UpdatedAtMs = now
		return true
	})
}

// Rewrite replaces the body of the card identified by id with newBody (a freshly
// regenerated analysis), re-derives the glance priority from the new content, and
// bumps UpdatedAtMs. Title and summary are left intact so the row preview stays
// stable; the regenerated analysis shows when the card is expanded. Applies to
// every item sharing the id (legacy twins), mirroring Ack/Correct. The native
// "다시 작성" path: the agent rewrites the analysis and the result lands here. A
// blank newBody is rejected so a failed regeneration never wipes the card.
func (s *Store) Rewrite(id, newBody string) (Item, error) {
	id = strings.TrimSpace(id)
	newBody = strings.TrimSpace(newBody)
	if id == "" || newBody == "" {
		return Item{}, ErrNotFound
	}
	return s.mutateItem(id, func(item *Item, now int64) bool {
		item.Body = newBody
		item.Priority = inferPriority(*item)
		item.UpdatedAtMs = now
		return true
	})
}

// mutateItem serializes the common load-normalize-update-snapshot lifecycle
// for id-scoped card changes. The callback reports whether persistence is
// needed, allowing idempotent reads to avoid rewriting the feed.
func (s *Store) mutateItem(id string, update func(*Item, int64) bool) (Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id = strings.TrimSpace(id)
	if id == "" {
		return Item{}, ErrNotFound
	}
	items, err := jsonlstore.Load[Item](s.path)
	if err != nil {
		return Item{}, err
	}
	now := time.Now().UnixMilli()
	var out Item
	found := false
	changed := false
	for i := range items {
		items[i] = normalizeExisting(items[i])
		if items[i].ID == id {
			changed = update(&items[i], now) || changed
			out = items[i]
			found = true
		}
	}
	if !found {
		return Item{}, ErrNotFound
	}
	if changed {
		if err := jsonlstore.Snapshot(s.path, items); err != nil {
			return Item{}, err
		}
	}
	return out, nil
}

// RunAction executes an allowed item action and records its result.
func (s *Store) RunAction(itemID, actionID string) (ActionResult, error) {
	return s.RunActionWithEffect(itemID, actionID, nil)
}

// RunActionWithEffect executes an allowed item action, requiring effect to
// succeed before the resulting state is snapshotted. Effects must be
// idempotent because a later snapshot failure leaves the action retryable.
func (s *Store) RunActionWithEffect(itemID, actionID string, effect ActionEffect) (ActionResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	itemID = strings.TrimSpace(itemID)
	actionID = strings.TrimSpace(actionID)
	if itemID == "" {
		return ActionResult{}, ErrNotFound
	}
	if actionID == "" {
		return ActionResult{}, ErrActionNotFound
	}
	items, err := jsonlstore.Load[Item](s.path)
	if err != nil {
		return ActionResult{}, err
	}
	now := time.Now().UnixMilli()
	// Normalize every item, then locate the first one carrying itemID. The ack /
	// snooze status changes below are applied to ALL items sharing the id, not
	// just the first match. Legacy feeds minted by the old restart-resetting id
	// counter can hold duplicate ids; settling only the first twin left the rest
	// unread, so the card "came back" on the next List (a zombie work-feed item).
	// Ack/snooze are id-scoped state changes, so resolve the whole id at once —
	// this mirrors Ack(), which already settles every item with the id.
	first := -1
	for i := range items {
		items[i] = normalizeExisting(items[i])
		if items[i].ID == itemID && first < 0 {
			first = i
		}
	}
	if first < 0 {
		return ActionResult{}, ErrNotFound
	}
	// 휴지통 — universal hard delete. Handled before the per-item action lookup so it
	// works on every card regardless of its stored action list: drop every item
	// carrying itemID (legacy feeds can hold id twins) and persist.
	if actionID == ActionTrash {
		deleted := items[first]
		kept := items[:0]
		for _, it := range items {
			if it.ID != itemID {
				kept = append(kept, it)
			}
		}
		if err := jsonlstore.Snapshot(s.path, kept); err != nil {
			return ActionResult{}, err
		}
		return ActionResult{
			Item:           deleted,
			Action:         Action{ID: ActionTrash, Kind: ActionTrash, Label: "휴지통"},
			SessionKey:     deleted.SessionKey,
			Message:        "deleted",
			RemoveFromFeed: true,
		}, nil
	}
	action, ok := findAction(items[first], actionID)
	if !ok {
		return ActionResult{}, ErrActionNotFound
	}
	result := ActionResult{
		Item:       items[first],
		Action:     action,
		SessionKey: items[first].SessionKey,
	}
	if effect != nil {
		if err := effect(items[first], action); err != nil {
			return ActionResult{}, err
		}
	}
	switch action.Kind {
	case ActionOpen:
		// Read-only: surface the item's context as a prompt, no state change.
		result.Prompt = actionPrompt(action, openPrompt(items[first]))
		result.Message = "opened"
		return result, nil
	case ActionFollowUp:
		result.Prompt = actionPrompt(action, followUpPrompt(items[first]))
		result.Message = "prompt_created"
		return result, nil
	case ActionSnooze:
		for i := range items {
			if items[i].ID != itemID {
				continue
			}
			items[i].Status = StatusSnoozed
			items[i].UpdatedAtMs = now
			items[i].SnoozedUntilMs = now + snoozeDuration.Milliseconds()
		}
		// Snooze is non-terminal — leave the action available so the user can
		// snooze again after the item re-surfaces (unlike ack, which is done).
		result.Item = items[first]
		result.Message = "snoozed"
		result.RemoveFromFeed = true
	case ActionAck:
		settleAction(items, itemID, action.ID, now)
		result.Item = items[first]
		result.Message = "acked"
		result.RemoveFromFeed = true
	case ActionAnswer:
		// Settle the card like ack, but also surface the chosen option as a prompt
		// the native sends to the asking session so the agent reacts to the answer.
		settleAction(items, itemID, action.ID, now)
		result.Item = items[first]
		result.Prompt = actionPrompt(action, "")
		result.Message = "answered"
		result.RemoveFromFeed = true
	case ActionMark:
		// Non-settling: mark just this action done and keep the card so its
		// sibling marks stay actionable. The durable "done" is the effect's
		// side channel (e.g. wiki due_done); the card refreshes on next cycle.
		for i := range items {
			if items[i].ID == itemID {
				markActionDone(&items[i], action.ID)
				items[i].UpdatedAtMs = now
			}
		}
		result.Item = items[first]
		result.Message = "marked"
		result.RemoveFromFeed = false
	default:
		return ActionResult{}, ErrActionNotFound
	}
	if err := jsonlstore.Snapshot(s.path, items); err != nil {
		return ActionResult{}, err
	}
	return result, nil
}

func settleAction(items []Item, itemID, actionID string, now int64) {
	for i := range items {
		if items[i].ID != itemID {
			continue
		}
		items[i].Status = StatusAcked
		items[i].UpdatedAtMs = now
		markActionDone(&items[i], actionID)
	}
}

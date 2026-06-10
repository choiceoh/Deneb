// people.go — miniapp.people.* RPC handlers.
//
// Aggregates Gmail senders over a recent window into a "who am I in
// contact with" directory, then folds in the wiki's curated 인물 pages
// so the native client has ONE people surface instead of two (the
// drawer's "사람" Gmail view and the 인물 wiki category used to coexist).
// A Gmail sender whose address or normalized name matches an 인물 page
// carries that page's path/summary inline; 인물 pages with no recent
// mail are appended as wiki-only rows. This is the secretary-style
// counterparty awareness surface — sorted by frequency, not
// chronology, so the user can see "who's been writing me a lot this
// month" at a glance. The existing sender_context handler covers the
// drill-in for a single person; this handler is the index.
//
// Implementation: one Gmail Search call into the existing client,
// followed by an in-memory group-by-sender pass. We deliberately do
// NOT recursively page — Gmail's `metadata.get` fan-out is 5 quota
// units per message, and 100 messages already covers a month of
// active correspondence for most operators while staying inside the
// per-user-per-second quota. If the user needs deeper history they
// can ask Deneb in chat ("이번 분기에 누가 메일 많이 보냈어").
//
// Calendar-attendee folding is intentionally out of scope for the
// initial cut. Mail traffic is the highest-signal proxy for
// "counterparties in motion right now"; calendar adds breadth but
// also noise (one-off invitees, large meetings). Punted to a follow-up.

package handlerminiapp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// PeopleClient is the slim Gmail interface the people handler needs.
// Mirrors GmailContextDeps.Client to keep the two handlers fungible
// in tests; intentionally a separate interface so future expansion
// (e.g. calendar.Client too) doesn't bloat one signature.
type PeopleClient interface {
	Search(ctx context.Context, query string, maxResults int) ([]gmail.MessageSummary, error)
}

// PeopleDeps holds the lazy Gmail client factory. Same UNAVAILABLE
// fallback pattern as crons / memory: an unconfigured Gmail surfaces
// the right error per call instead of crashing the gateway at boot.
//
// WikiStore is optional and best-effort: when wired, the handler folds
// 인물 wiki pages into the directory (matched senders get wikiPath/
// wikiSummary, unmatched pages become wiki-only tail rows). A nil
// factory, a factory error, or a listing error all degrade to the
// Gmail-only behavior — the wiki must never break the people list.
type PeopleDeps struct {
	Client    func() (PeopleClient, error)
	WikiStore func() (MemorySearcher, error)
}

const (
	defaultPeopleLimit      = 30
	maxPeopleLimit          = 100
	defaultPeopleWindowDays = 30
	maxPeopleWindowDays     = 365
	maxPeopleScanMessages   = 100 // Gmail Search fan-out cap; see file header
	maxPeopleSubjectPreview = 80  // runes

	// peopleWikiCategory is the wiki directory that holds person pages —
	// the same one contacts sync (wiki.EnrichContacts) maintains.
	peopleWikiCategory = "인물"
	// maxPeopleWikiRows bounds the wiki-only tail so a runaway 인물
	// directory can't bloat the response (mirrors maxMemoryListLimit).
	maxPeopleWikiRows = 200
)

// PeopleMethods returns the miniapp.people.* handler map. Returns nil
// when no client factory is provided so method_registry can register
// conditionally.
func PeopleMethods(deps PeopleDeps) map[string]rpcutil.HandlerFunc {
	if deps.Client == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"miniapp.people.list": peopleList(deps),
	}
}

// PersonRow is one row in the people directory. Three shapes share it:
// a plain Gmail sender (wiki fields empty), a sender matched to an 인물
// page (wiki fields set), and a wiki-only person with no recent mail
// (email empty, messageCount 0, wiki fields set).
//
//deneb:wire
type PersonRow struct {
	Email        string `json:"email"`
	Name         string `json:"name,omitempty"`
	MessageCount int    `json:"messageCount"`
	LastSeen     string `json:"lastSeen,omitempty"`    // ISO 8601, from the most recent message
	LastSubject  string `json:"lastSubject,omitempty"` // truncated
	WikiPath     string `json:"wikiPath,omitempty"`    // 인물 page path when this person is in the wiki
	WikiSummary  string `json:"wikiSummary,omitempty"` // that page's one-line summary
}

func peopleList(deps PeopleDeps) rpcutil.HandlerFunc {
	type params struct {
		Limit      int `json:"limit,omitempty"`
		WindowDays int `json:"windowDays,omitempty"`
	}
	type out struct {
		People       []PersonRow `json:"people"`
		WindowDays   int         `json:"windowDays"`
		ScannedCount int         `json:"scannedCount"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		var p params
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return rpcerr.InvalidParams(err).Response(req.ID)
			}
		}
		limit := p.Limit
		if limit <= 0 {
			limit = defaultPeopleLimit
		}
		if limit > maxPeopleLimit {
			limit = maxPeopleLimit
		}
		window := p.WindowDays
		if window <= 0 {
			window = defaultPeopleWindowDays
		}
		if window > maxPeopleWindowDays {
			window = maxPeopleWindowDays
		}

		client, err := deps.Client()
		if err != nil {
			return rpcerr.WrapUnavailable("gmail unavailable", err).Response(req.ID)
		}

		// `-from:me` excludes the operator's own sent mail so the list
		// is *counterparties*. Without it the operator appears as the
		// most frequent sender in their own directory — useless.
		// `category:primary` could filter promo / list noise further;
		// not used here because it changes semantics depending on
		// whether the user has Gmail Categories enabled at all. The
		// front-end can filter by message-count threshold instead.
		query := fmt.Sprintf("newer_than:%dd -from:me", window)
		msgs, err := client.Search(ctx, query, maxPeopleScanMessages)
		if err != nil {
			return rpcerr.WrapUnavailable("gmail search failed", err).Response(req.ID)
		}

		people := aggregatePeople(msgs)
		if len(people) > limit {
			people = people[:limit]
		}
		people = mergeWikiPeople(people, loadWikiPeople(deps.WikiStore))
		return rpcutil.RespondOK(req.ID, out{
			People:       people,
			WindowDays:   window,
			ScannedCount: len(msgs),
		})
	}
}

// aggregatePeople groups messages by sender email, picking the most
// recent message's subject + date as the "last" for that person. Sort
// order is messageCount desc, then lastSeen desc as tiebreaker, then
// email asc for full determinism (matters for snapshot tests).
//
// Senders without a parseable email are dropped — the row would be
// confusing without a way to identify the counterparty. The
// pre-existing parseSender (in gmail_context.go) handles the common
// "Display <addr@host>" and bare-email forms.
func aggregatePeople(msgs []gmail.MessageSummary) []PersonRow {
	type acc struct {
		email        string
		name         string
		messageCount int
		lastSeen     time.Time
		lastSubject  string
	}
	byEmail := make(map[string]*acc)

	for _, m := range msgs {
		email, displayName := parseSender(m.From)
		if email == "" {
			continue
		}
		key := strings.ToLower(email)
		entry, ok := byEmail[key]
		if !ok {
			entry = &acc{email: email, name: displayName}
			byEmail[key] = entry
		} else if entry.name == "" && displayName != "" {
			// First time we saw this sender they were anonymous;
			// fill the display name as soon as we see it.
			entry.name = displayName
		}
		entry.messageCount++

		when := parseMessageTime(m.Date)
		if when.After(entry.lastSeen) {
			entry.lastSeen = when
			entry.lastSubject = m.Subject
		}
	}

	rows := make([]PersonRow, 0, len(byEmail))
	for _, a := range byEmail {
		row := PersonRow{
			Email:        a.email,
			Name:         a.name,
			MessageCount: a.messageCount,
			LastSubject:  truncateRunes(a.lastSubject, maxPeopleSubjectPreview),
		}
		if !a.lastSeen.IsZero() {
			row.LastSeen = a.lastSeen.UTC().Format(time.RFC3339)
		}
		rows = append(rows, row)
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].MessageCount != rows[j].MessageCount {
			return rows[i].MessageCount > rows[j].MessageCount
		}
		if rows[i].LastSeen != rows[j].LastSeen {
			return rows[i].LastSeen > rows[j].LastSeen
		}
		return rows[i].Email < rows[j].Email
	})
	return rows
}

// wikiPerson is one 인물 page prepared for merging: identity (title →
// normalized name key, 연락처 emails) plus the row fields the client
// renders (path, summary) and the sort key (updated).
type wikiPerson struct {
	path    string
	title   string
	summary string
	updated string   // YYYY-MM-DD from frontmatter; "" sorts last
	emails  []string // lowercased, from the "## 연락처" section
}

// loadWikiPeople lists the 인물 wiki pages via the (optional) store
// factory. Every failure path — nil factory, factory error, listing
// error, unreadable page — degrades to "no wiki people" so the Gmail
// directory keeps working when the wiki is disabled or broken.
func loadWikiPeople(storeFn func() (MemorySearcher, error)) []wikiPerson {
	if storeFn == nil {
		return nil
	}
	store, err := storeFn()
	if err != nil || store == nil {
		return nil
	}
	relPaths, err := store.ListPages(peopleWikiCategory)
	if err != nil {
		return nil
	}
	people := make([]wikiPerson, 0, len(relPaths))
	for _, rel := range relPaths {
		if len(people) >= maxPeopleWikiRows {
			break
		}
		page, perr := store.ReadPage(rel)
		if perr != nil || page == nil {
			continue
		}
		// Same defensive check as wiki's contacts sync: a stray
		// non-person .md under 인물/ shouldn't surface as a person.
		if page.Meta.Category != "" && page.Meta.Category != peopleWikiCategory {
			continue
		}
		title := strings.TrimSpace(page.Meta.Title)
		if title == "" {
			continue
		}
		people = append(people, wikiPerson{
			path:    rel,
			title:   title,
			summary: strings.TrimSpace(page.Meta.Summary),
			updated: page.Meta.Updated,
			emails:  contactSectionEmails(page),
		})
	}
	return people
}

// contactSectionEmails extracts the person's own addresses from the
// "## 연락처" section (the exact `- 이메일:` bullet contacts sync writes).
// Scanning only that section — not the whole body — keeps a note that
// merely *mentions* someone else's address from mis-pairing pages.
func contactSectionEmails(page *wiki.Page) []string {
	_, sections := page.SplitByH2()
	for _, sec := range sections {
		if !strings.EqualFold(strings.TrimSpace(sec.Heading), "연락처") {
			continue
		}
		var emails []string
		for _, line := range strings.Split(sec.Content, "\n") {
			rest, ok := strings.CutPrefix(strings.TrimSpace(line), "- 이메일:")
			if !ok {
				continue
			}
			for _, e := range strings.Split(rest, ",") {
				e = strings.ToLower(strings.TrimSpace(e))
				if looksLikeEmail(e) {
					emails = append(emails, e)
				}
			}
		}
		return emails
	}
	return nil
}

// mergeWikiPeople folds 인물 pages into the Gmail sender rows: a row
// whose address appears in a page's 연락처 section (strong signal) or
// whose normalized display name equals the page title (honorifics
// stripped — "김민준 부장" matches page "김민준") carries that page's
// path/summary. Pages left unmatched append as wiki-only rows after
// the Gmail block, updated desc then title asc — the client renders
// them as the "no recent mail, but curated" section.
func mergeWikiPeople(rows []PersonRow, people []wikiPerson) []PersonRow {
	if len(people) == 0 {
		return rows
	}
	byEmail := make(map[string]int)
	byName := make(map[string]int)
	for i, wp := range people {
		for _, e := range wp.emails {
			if _, ok := byEmail[e]; !ok {
				byEmail[e] = i
			}
		}
		// First page wins a contested key, same as contacts sync's
		// listPeopleByName; ≥2 runes so a degenerate title can't match.
		key := wiki.NormalizePersonName(wp.title)
		if len([]rune(key)) >= 2 {
			if _, ok := byName[key]; !ok {
				byName[key] = i
			}
		}
	}

	matched := make([]bool, len(people))
	for ri := range rows {
		idx, ok := byEmail[strings.ToLower(rows[ri].Email)]
		if !ok {
			key := wiki.NormalizePersonName(rows[ri].Name)
			if len([]rune(key)) < 2 {
				continue
			}
			idx, ok = byName[key]
			if !ok {
				continue
			}
		}
		rows[ri].WikiPath = people[idx].path
		rows[ri].WikiSummary = people[idx].summary
		matched[idx] = true
	}

	rest := make([]wikiPerson, 0, len(people))
	for i, wp := range people {
		if !matched[i] {
			rest = append(rest, wp)
		}
	}
	sort.Slice(rest, func(i, j int) bool {
		if rest[i].updated != rest[j].updated {
			// Empty updated sinks last (same rule as list_in_category).
			if rest[i].updated == "" {
				return false
			}
			if rest[j].updated == "" {
				return true
			}
			return rest[i].updated > rest[j].updated
		}
		return rest[i].title < rest[j].title
	})
	for _, wp := range rest {
		rows = append(rows, PersonRow{
			Name:        wp.title,
			WikiPath:    wp.path,
			WikiSummary: wp.summary,
		})
	}
	return rows
}

// parseMessageTime accepts the Gmail-normalized ISO 8601 the rest of
// the Mini App backend uses. Falls back to RFC 2822 (the Gmail Date
// header before our normalization) so we don't drop messages just
// because the normalization layer didn't recognize their date format.
// Returns the zero time on total failure — the row still aggregates
// but won't compete for "most recent".
func parseMessageTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC1123Z, s); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC1123, s); err == nil {
		return t
	}
	return time.Time{}
}

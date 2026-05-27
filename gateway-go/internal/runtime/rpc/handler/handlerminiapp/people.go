// people.go — miniapp.people.* RPC handlers.
//
// Aggregates Gmail senders over a recent window into a "who am I in
// contact with" directory. This is the secretary-style counterparty
// awareness surface — sorted by frequency, not chronology, so the user
// can see "who's been writing me a lot this month" at a glance. The
// existing sender_context handler covers the drill-in for a single
// person; this handler is the index.
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
type PeopleDeps struct {
	Client func() (PeopleClient, error)
}

const (
	defaultPeopleLimit      = 30
	maxPeopleLimit          = 100
	defaultPeopleWindowDays = 30
	maxPeopleWindowDays     = 365
	maxPeopleScanMessages   = 100 // Gmail Search fan-out cap; see file header
	maxPeopleSubjectPreview = 80  // runes
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

// PersonRow is one row in the people directory.
type PersonRow struct {
	Email        string `json:"email"`
	Name         string `json:"name,omitempty"`
	MessageCount int    `json:"messageCount"`
	LastSeen     string `json:"lastSeen,omitempty"`    // ISO 8601, from the most recent message
	LastSubject  string `json:"lastSubject,omitempty"` // truncated
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

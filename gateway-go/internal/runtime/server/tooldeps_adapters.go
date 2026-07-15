package server

import (
	"context"
	"log/slog"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/domainbind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/infrabind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/pipebind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/platbind"
)

// workFeedRWAdapter projects the native-sync work-feed store onto the
// pipebind.WorkFeedRW port (DTO boundary — tools never import domain/workfeed).
type workFeedRWAdapter struct {
	inner *nativeWorkFeedStore
}

func (a workFeedRWAdapter) List(limit int, includeAcked bool) ([]pipebind.WorkFeedItem, int, error) {
	items, n, err := a.inner.List(limit, includeAcked)
	return mapWorkFeedItems(items), n, err
}

func (a workFeedRWAdapter) MarkRead(id string) (pipebind.WorkFeedItem, error) {
	item, err := a.inner.MarkRead(id)
	return mapWorkFeedItem(item), err
}

func (a workFeedRWAdapter) Ack(id string) (pipebind.WorkFeedItem, error) {
	item, err := a.inner.Ack(id)
	return mapWorkFeedItem(item), err
}

func (a workFeedRWAdapter) Append(item pipebind.WorkFeedItem) (pipebind.WorkFeedItem, error) {
	out, err := a.inner.Append(unmapWorkFeedItem(item))
	return mapWorkFeedItem(out), err
}

func mapWorkFeedItems(in []domainbind.Item) []pipebind.WorkFeedItem {
	out := make([]pipebind.WorkFeedItem, len(in))
	for i := range in {
		out[i] = mapWorkFeedItem(in[i])
	}
	return out
}

func mapWorkFeedItem(in domainbind.Item) pipebind.WorkFeedItem {
	actions := make([]pipebind.WorkFeedAction, len(in.Actions))
	for i, a := range in.Actions {
		actions[i] = pipebind.WorkFeedAction{
			ID: a.ID, Kind: a.Kind, Label: a.Label, Status: a.Status, Prompt: a.Prompt,
		}
	}
	return pipebind.WorkFeedItem{
		ID: in.ID, Source: in.Source, Title: in.Title, Summary: in.Summary, Body: in.Body,
		SessionKey: in.SessionKey, RefType: in.RefType, RefID: in.RefID, Metadata: in.Metadata,
		Status: in.Status, Priority: in.Priority, Question: in.Question, Actions: actions,
		CreatedAtMs: in.CreatedAtMs, UpdatedAtMs: in.UpdatedAtMs,
		SnoozedUntilMs: in.SnoozedUntilMs, ReadAtMs: in.ReadAtMs,
	}
}

func unmapWorkFeedItem(in pipebind.WorkFeedItem) domainbind.Item {
	actions := make([]domainbind.Action, len(in.Actions))
	for i, a := range in.Actions {
		actions[i] = domainbind.Action{
			ID: a.ID, Kind: a.Kind, Label: a.Label, Status: a.Status, Prompt: a.Prompt,
		}
	}
	return domainbind.Item{
		ID: in.ID, Source: in.Source, Title: in.Title, Summary: in.Summary, Body: in.Body,
		SessionKey: in.SessionKey, RefType: in.RefType, RefID: in.RefID, Metadata: in.Metadata,
		Status: in.Status, Priority: in.Priority, Question: in.Question, Actions: actions,
		CreatedAtMs: in.CreatedAtMs, UpdatedAtMs: in.UpdatedAtMs,
		SnoozedUntilMs: in.SnoozedUntilMs, ReadAtMs: in.ReadAtMs,
	}
}

func adaptMarketSummary(fn func(ctx context.Context) ([]domainbind.Quote, int64, bool, error)) func(ctx context.Context) ([]pipebind.MarketQuote, int64, bool, error) {
	if fn == nil {
		return nil
	}
	return func(ctx context.Context) ([]pipebind.MarketQuote, int64, bool, error) {
		quotes, asOf, stale, err := fn(ctx)
		if err != nil {
			return nil, asOf, stale, err
		}
		out := make([]pipebind.MarketQuote, len(quotes))
		for i, q := range quotes {
			out[i] = pipebind.MarketQuote{
				Symbol: q.Symbol, Label: q.Label, Currency: q.Currency,
				Price: q.Price, PrevClose: q.PrevClose, AsOf: q.AsOf,
			}
		}
		return out, asOf, stale, nil
	}
}

func adaptFilesSemanticSearch(fn func(ctx context.Context, query string, max int) ([]domainbind.ScoredEntry, error)) func(ctx context.Context, query string, max int) ([]pipebind.FileHit, error) {
	if fn == nil {
		return nil
	}
	return func(ctx context.Context, query string, max int) ([]pipebind.FileHit, error) {
		hits, err := fn(ctx, query, max)
		if err != nil {
			return nil, err
		}
		out := make([]pipebind.FileHit, len(hits))
		for i, h := range hits {
			out[i] = pipebind.FileHit{
				Path: h.Entry.PathDisplay, Name: h.Entry.Name, Score: h.Score, Snippet: h.Snippet,
			}
		}
		return out, nil
	}
}

// --- contacts / agentlog / calendar ports ------------------------------------

func adaptContactsBook(store *domainbind.ContactsStore) pipebind.ContactsBook {
	if store == nil {
		return nil
	}
	return contactsBookAdapter{inner: store}
}

type contactsBookAdapter struct {
	inner *domainbind.ContactsStore
}

func (a contactsBookAdapter) Count() int { return a.inner.Count() }

func (a contactsBookAdapter) LookupPhone(query string) []pipebind.Contact {
	return mapTooldepsContacts(a.inner.LookupPhone(query))
}

func (a contactsBookAdapter) Search(query string, limit int) []pipebind.Contact {
	return mapTooldepsContacts(a.inner.Search(query, limit))
}

func (a contactsBookAdapter) All() []pipebind.Contact {
	return mapTooldepsContacts(a.inner.All())
}

func mapTooldepsContacts(in []domainbind.Contact) []pipebind.Contact {
	out := make([]pipebind.Contact, len(in))
	for i, c := range in {
		out[i] = pipebind.Contact{Name: c.Name, Phones: c.Phones, Emails: c.Emails, Org: c.Org}
	}
	return out
}

func adaptAgentLogStats(w *infrabind.Writer) pipebind.AgentLogStats {
	if w == nil {
		return nil
	}
	return agentLogStatsAdapter{w: w}
}

type agentLogStatsAdapter struct {
	w *infrabind.Writer
}

func (a agentLogStatsAdapter) Aggregate(sinceMs int64) pipebind.AgentLogAggregate {
	r := a.w.Aggregate(sinceMs)
	return pipebind.AgentLogAggregate{
		Runs: r.Runs, ProactiveRuns: r.ProactiveRuns,
		TotalInputTokens: r.TotalInputTokens, TotalOutputTokens: r.TotalOutputTokens,
		CacheReadTokens: r.CacheReadTokens,
	}
}

func (a agentLogStatsAdapter) AggregateBySession(sinceMs int64) []pipebind.AgentLogSessionStat {
	in := a.w.AggregateBySession(sinceMs)
	out := make([]pipebind.AgentLogSessionStat, len(in))
	for i, st := range in {
		out[i] = pipebind.AgentLogSessionStat{
			Session: st.Session, Runs: st.Runs, Errors: st.Errors,
			InputTokens: st.InputTokens, OutputTokens: st.OutputTokens,
			ToolCalls: st.ToolCalls, LastTs: st.LastTs,
		}
	}
	return out
}

func adaptCalendarReaderFactory(fn func() (*platbind.CalendarClient, error)) func() (pipebind.CalendarReader, error) {
	if fn == nil {
		return nil
	}
	return func() (pipebind.CalendarReader, error) {
		c, err := fn()
		if err != nil {
			return nil, err
		}
		return calendarReaderAdapter{inner: c}, nil
	}
}

type calendarReaderAdapter struct {
	inner interface {
		ListUpcoming(ctx context.Context, from, to time.Time, maxResults int) ([]platbind.Event, error)
		Get(ctx context.Context, eventID string) (*platbind.Event, error)
	}
}

func (a calendarReaderAdapter) ListUpcoming(ctx context.Context, from, to time.Time, maxResults int) ([]pipebind.CalendarEvent, error) {
	events, err := a.inner.ListUpcoming(ctx, from, to, maxResults)
	if err != nil {
		return nil, err
	}
	return mapCalendarEvents(events), nil
}

func (a calendarReaderAdapter) Get(ctx context.Context, eventID string) (*pipebind.CalendarEvent, error) {
	ev, err := a.inner.Get(ctx, eventID)
	if err != nil {
		return nil, err
	}
	if ev == nil {
		return nil, nil
	}
	out := mapCalendarEvent(*ev)
	return &out, nil
}

func adaptLocalCalendar(store *platbind.LocalCalStore) pipebind.LocalCalendar {
	if store == nil {
		return nil
	}
	return localCalendarAdapter{inner: store}
}

type localCalendarAdapter struct {
	inner *platbind.LocalCalStore
}

func (a localCalendarAdapter) ListRange(from, to time.Time) []pipebind.CalendarEvent {
	return mapCalendarEvents(a.inner.ListRange(from, to))
}

func (a localCalendarAdapter) Get(id string) *pipebind.CalendarEvent {
	ev := a.inner.Get(id)
	if ev == nil {
		return nil
	}
	out := mapCalendarEvent(*ev)
	return &out
}

func (a localCalendarAdapter) Create(in pipebind.CalendarCreateInput) (pipebind.CalendarEvent, error) {
	ev, err := a.inner.Create(unmapCreateInput(in))
	if err != nil {
		return pipebind.CalendarEvent{}, err
	}
	return mapCalendarEvent(ev), nil
}

func (a localCalendarAdapter) Update(id string, in pipebind.CalendarCreateInput) (*pipebind.CalendarEvent, error) {
	ev, err := a.inner.Update(id, unmapCreateInput(in))
	if err != nil {
		return nil, err
	}
	if ev == nil {
		return nil, nil
	}
	out := mapCalendarEvent(*ev)
	return &out, nil
}

func (a localCalendarAdapter) Delete(id string) error { return a.inner.Delete(id) }

func mapCalendarEvents(in []platbind.Event) []pipebind.CalendarEvent {
	out := make([]pipebind.CalendarEvent, len(in))
	for i := range in {
		out[i] = mapCalendarEvent(in[i])
	}
	return out
}

func mapCalendarEvent(in platbind.Event) pipebind.CalendarEvent {
	out := pipebind.CalendarEvent{
		ID: in.ID, Summary: in.Summary, Description: in.Description, Location: in.Location,
		Start: in.Start, End: in.End, AllDay: in.AllDay, HTMLLink: in.HTMLLink, Status: in.Status,
		Organizer: mapCalendarAttendee(in.Organizer),
		Source:    in.Source, SourceLabel: in.SourceLabel, Kind: in.Kind, Docs: in.Docs,
	}
	if in.Conference != nil {
		out.Conference = &pipebind.CalendarConference{Solution: in.Conference.Solution, URI: in.Conference.URI}
	}
	if len(in.Attendees) > 0 {
		out.Attendees = make([]pipebind.CalendarAttendee, len(in.Attendees))
		for i, a := range in.Attendees {
			out.Attendees[i] = mapCalendarAttendee(a)
		}
	}
	return out
}

func mapCalendarAttendee(in platbind.Attendee) pipebind.CalendarAttendee {
	return pipebind.CalendarAttendee{
		Email: in.Email, DisplayName: in.DisplayName, ResponseStatus: in.ResponseStatus,
		Self: in.Self, Organizer: in.Organizer,
	}
}

func unmapCreateInput(in pipebind.CalendarCreateInput) platbind.CreateInput {
	return platbind.CreateInput{
		Summary: in.Summary, Description: in.Description, Location: in.Location,
		Start: in.Start, End: in.End, AllDay: in.AllDay,
		Source: in.Source, SourceLabel: in.SourceLabel, Kind: in.Kind, Docs: in.Docs,
	}
}

// resolveToolLocalCalendar wraps the process-wide localcal store as a tooldeps
// LocalCalendar (DTO boundary). Typed-nil safe.
func resolveToolLocalCalendar(logger *slog.Logger) pipebind.LocalCalendar {
	store, err := platbind.LocalCalDefault()
	if err != nil {
		if logger != nil {
			logger.Error("local calendar store unavailable — add/edit/delete disabled", "error", err)
		}
		return nil
	}
	return adaptLocalCalendar(store)
}

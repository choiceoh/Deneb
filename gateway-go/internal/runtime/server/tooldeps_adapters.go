package server

import (
	"context"
	"log/slog"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/core/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/contacts"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/filestore"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/market"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/calwrite"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/localcal"
)

// workFeedRWAdapter projects the native-sync work-feed store onto the
// tooldeps.WorkFeedRW port (DTO boundary — tools never import domain/workfeed).
type workFeedRWAdapter struct {
	inner *nativeWorkFeedStore
}

func (a workFeedRWAdapter) List(limit int, includeAcked bool) ([]tooldeps.WorkFeedItem, int, error) {
	items, n, err := a.inner.List(limit, includeAcked)
	return mapWorkFeedItems(items), n, err
}

func (a workFeedRWAdapter) MarkRead(id string) (tooldeps.WorkFeedItem, error) {
	item, err := a.inner.MarkRead(id)
	return mapWorkFeedItem(item), err
}

func (a workFeedRWAdapter) Ack(id string) (tooldeps.WorkFeedItem, error) {
	item, err := a.inner.Ack(id)
	return mapWorkFeedItem(item), err
}

func (a workFeedRWAdapter) Append(item tooldeps.WorkFeedItem) (tooldeps.WorkFeedItem, error) {
	out, err := a.inner.Append(unmapWorkFeedItem(item))
	return mapWorkFeedItem(out), err
}

func mapWorkFeedItems(in []workfeed.Item) []tooldeps.WorkFeedItem {
	out := make([]tooldeps.WorkFeedItem, len(in))
	for i := range in {
		out[i] = mapWorkFeedItem(in[i])
	}
	return out
}

func mapWorkFeedItem(in workfeed.Item) tooldeps.WorkFeedItem {
	actions := make([]tooldeps.WorkFeedAction, len(in.Actions))
	for i, a := range in.Actions {
		actions[i] = tooldeps.WorkFeedAction{
			ID: a.ID, Kind: a.Kind, Label: a.Label, Status: a.Status, Prompt: a.Prompt,
		}
	}
	return tooldeps.WorkFeedItem{
		ID: in.ID, Source: in.Source, Title: in.Title, Summary: in.Summary, Body: in.Body,
		SessionKey: in.SessionKey, RefType: in.RefType, RefID: in.RefID, Metadata: in.Metadata,
		ClusterID: in.ClusterID, RelatedIDs: append([]string(nil), in.RelatedIDs...),
		Status: in.Status, Priority: in.Priority, Question: in.Question, Actions: actions,
		CreatedAtMs: in.CreatedAtMs, UpdatedAtMs: in.UpdatedAtMs,
		SnoozedUntilMs: in.SnoozedUntilMs, ReadAtMs: in.ReadAtMs,
	}
}

func unmapWorkFeedItem(in tooldeps.WorkFeedItem) workfeed.Item {
	actions := make([]workfeed.Action, len(in.Actions))
	for i, a := range in.Actions {
		actions[i] = workfeed.Action{
			ID: a.ID, Kind: a.Kind, Label: a.Label, Status: a.Status, Prompt: a.Prompt,
		}
	}
	return workfeed.Item{
		ID: in.ID, Source: in.Source, Title: in.Title, Summary: in.Summary, Body: in.Body,
		SessionKey: in.SessionKey, RefType: in.RefType, RefID: in.RefID, Metadata: in.Metadata,
		ClusterID: in.ClusterID, RelatedIDs: append([]string(nil), in.RelatedIDs...),
		Status: in.Status, Priority: in.Priority, Question: in.Question, Actions: actions,
		CreatedAtMs: in.CreatedAtMs, UpdatedAtMs: in.UpdatedAtMs,
		SnoozedUntilMs: in.SnoozedUntilMs, ReadAtMs: in.ReadAtMs,
	}
}

func adaptMarketSummary(fn func(ctx context.Context) ([]market.Quote, int64, bool, error)) func(ctx context.Context) ([]tooldeps.MarketQuote, int64, bool, error) {
	if fn == nil {
		return nil
	}
	return func(ctx context.Context) ([]tooldeps.MarketQuote, int64, bool, error) {
		quotes, asOf, stale, err := fn(ctx)
		if err != nil {
			return nil, asOf, stale, err
		}
		out := make([]tooldeps.MarketQuote, len(quotes))
		for i, q := range quotes {
			out[i] = tooldeps.MarketQuote{
				Symbol: q.Symbol, Label: q.Label, Currency: q.Currency,
				Price: q.Price, PrevClose: q.PrevClose, AsOf: q.AsOf,
			}
		}
		return out, asOf, stale, nil
	}
}

func adaptFilesSemanticSearch(fn func(ctx context.Context, query string, max int) ([]filestore.ScoredEntry, error)) func(ctx context.Context, query string, max int) ([]tooldeps.FileHit, error) {
	if fn == nil {
		return nil
	}
	return func(ctx context.Context, query string, max int) ([]tooldeps.FileHit, error) {
		hits, err := fn(ctx, query, max)
		if err != nil {
			return nil, err
		}
		out := make([]tooldeps.FileHit, len(hits))
		for i, h := range hits {
			out[i] = tooldeps.FileHit{
				Path: h.Entry.PathDisplay, Name: h.Entry.Name, Score: h.Score, Snippet: h.Snippet,
			}
		}
		return out, nil
	}
}

// --- contacts / agentlog / calendar ports ------------------------------------

func adaptContactsBook(store *contacts.Store) tooldeps.ContactsBook {
	if store == nil {
		return nil
	}
	return contactsBookAdapter{inner: store}
}

type contactsBookAdapter struct {
	inner *contacts.Store
}

func (a contactsBookAdapter) Count() int { return a.inner.Count() }

func (a contactsBookAdapter) LookupPhone(query string) []tooldeps.Contact {
	return mapTooldepsContacts(a.inner.LookupPhone(query))
}

func (a contactsBookAdapter) Search(query string, limit int) []tooldeps.Contact {
	return mapTooldepsContacts(a.inner.Search(query, limit))
}

func (a contactsBookAdapter) All() []tooldeps.Contact {
	return mapTooldepsContacts(a.inner.All())
}

func mapTooldepsContacts(in []contacts.Contact) []tooldeps.Contact {
	out := make([]tooldeps.Contact, len(in))
	for i, c := range in {
		out[i] = tooldeps.Contact{Name: c.Name, Phones: c.Phones, Emails: c.Emails, Org: c.Org}
	}
	return out
}

func adaptAgentLogStats(w *agentlog.Writer) tooldeps.AgentLogStats {
	if w == nil {
		return nil
	}
	return agentLogStatsAdapter{w: w}
}

type agentLogStatsAdapter struct {
	w *agentlog.Writer
}

func (a agentLogStatsAdapter) Aggregate(sinceMs int64) tooldeps.AgentLogAggregate {
	r := a.w.Aggregate(sinceMs)
	return tooldeps.AgentLogAggregate{
		Runs: r.Runs, ProactiveRuns: r.ProactiveRuns,
		TotalInputTokens: r.TotalInputTokens, TotalOutputTokens: r.TotalOutputTokens,
		CacheReadTokens: r.CacheReadTokens,
	}
}

func (a agentLogStatsAdapter) AggregateBySession(sinceMs int64) []tooldeps.AgentLogSessionStat {
	in := a.w.AggregateBySession(sinceMs)
	out := make([]tooldeps.AgentLogSessionStat, len(in))
	for i, st := range in {
		out[i] = tooldeps.AgentLogSessionStat{
			Session: st.Session, Runs: st.Runs, Errors: st.Errors,
			InputTokens: st.InputTokens, OutputTokens: st.OutputTokens,
			ToolCalls: st.ToolCalls, LastTs: st.LastTs,
		}
	}
	return out
}

func adaptCalendarReaderFactory(fn func() (*calendar.Client, error)) func() (tooldeps.CalendarReader, error) {
	if fn == nil {
		return nil
	}
	return func() (tooldeps.CalendarReader, error) {
		c, err := fn()
		if err != nil {
			return nil, err
		}
		return calendarReaderAdapter{inner: c}, nil
	}
}

type calendarReaderAdapter struct {
	inner interface {
		ListUpcoming(ctx context.Context, from, to time.Time, maxResults int) ([]calendar.Event, error)
		Get(ctx context.Context, eventID string) (*calendar.Event, error)
	}
}

func (a calendarReaderAdapter) ListUpcoming(ctx context.Context, from, to time.Time, maxResults int) ([]tooldeps.CalendarEvent, error) {
	events, err := a.inner.ListUpcoming(ctx, from, to, maxResults)
	if err != nil {
		return nil, err
	}
	return mapCalendarEvents(events), nil
}

func (a calendarReaderAdapter) Get(ctx context.Context, eventID string) (*tooldeps.CalendarEvent, error) {
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

func adaptLocalCalendar(store *localcal.Store) tooldeps.LocalCalendar {
	if store == nil {
		return nil
	}
	return localCalendarAdapter{inner: store}
}

type localCalendarAdapter struct {
	inner *localcal.Store
}

func (a localCalendarAdapter) ListRange(from, to time.Time) []tooldeps.CalendarEvent {
	return mapCalendarEvents(a.inner.ListRange(from, to))
}

func (a localCalendarAdapter) Get(id string) *tooldeps.CalendarEvent {
	ev := a.inner.Get(id)
	if ev == nil {
		return nil
	}
	out := mapCalendarEvent(*ev)
	return &out
}

func (a localCalendarAdapter) Create(in tooldeps.CalendarCreateInput) (tooldeps.CalendarEvent, error) {
	ev, err := a.inner.Create(unmapCreateInput(in))
	if err != nil {
		return tooldeps.CalendarEvent{}, err
	}
	return mapCalendarEvent(ev), nil
}

func (a localCalendarAdapter) Update(id string, in tooldeps.CalendarCreateInput) (*tooldeps.CalendarEvent, error) {
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

func mapCalendarEvents(in []calendar.Event) []tooldeps.CalendarEvent {
	out := make([]tooldeps.CalendarEvent, len(in))
	for i := range in {
		out[i] = mapCalendarEvent(in[i])
	}
	return out
}

func mapCalendarEvent(in calendar.Event) tooldeps.CalendarEvent {
	out := tooldeps.CalendarEvent{
		ID: in.ID, Summary: in.Summary, Description: in.Description, Location: in.Location,
		Start: in.Start, End: in.End, AllDay: in.AllDay, HTMLLink: in.HTMLLink, Status: in.Status,
		Organizer: mapCalendarAttendee(in.Organizer),
		Source:    in.Source, SourceLabel: in.SourceLabel, Kind: in.Kind, Docs: in.Docs,
	}
	if in.Conference != nil {
		out.Conference = &tooldeps.CalendarConference{Solution: in.Conference.Solution, URI: in.Conference.URI}
	}
	if len(in.Attendees) > 0 {
		out.Attendees = make([]tooldeps.CalendarAttendee, len(in.Attendees))
		for i, a := range in.Attendees {
			out.Attendees[i] = mapCalendarAttendee(a)
		}
	}
	return out
}

func mapCalendarAttendee(in calendar.Attendee) tooldeps.CalendarAttendee {
	return tooldeps.CalendarAttendee{
		Email: in.Email, DisplayName: in.DisplayName, ResponseStatus: in.ResponseStatus,
		Self: in.Self, Organizer: in.Organizer,
	}
}

// adaptCalendarWriterFactory exposes the calwrite syncer to the chat calendar
// tool through the tooldeps DTO boundary, so a chat-created event reaches Google
// through the SAME mirror the miniapp RPC uses.
func adaptCalendarWriterFactory(fn func() (*calwrite.Syncer, error)) func() (tooldeps.CalendarWriter, error) {
	if fn == nil {
		return nil
	}
	return func() (tooldeps.CalendarWriter, error) {
		s, err := fn()
		if err != nil {
			return nil, err
		}
		if s == nil {
			return nil, nil
		}
		return calendarWriterAdapter{inner: s}, nil
	}
}

type calendarWriterAdapter struct {
	inner interface {
		Push(ctx context.Context, localID string, ev calendar.Event) error
		Remove(ctx context.Context, localID string) error
	}
}

func (a calendarWriterAdapter) Push(ctx context.Context, localID string, ev tooldeps.CalendarEvent) error {
	return a.inner.Push(ctx, localID, unmapCalendarEvent(ev))
}

func (a calendarWriterAdapter) Remove(ctx context.Context, localID string) error {
	return a.inner.Remove(ctx, localID)
}

// unmapCalendarEvent is the DTO→platform direction of mapCalendarEvent, carrying
// the fields the Google write body actually uses (calwrite.toGoogleEvent reads
// summary/description/location/start/end/allDay).
func unmapCalendarEvent(in tooldeps.CalendarEvent) calendar.Event {
	return calendar.Event{
		ID: in.ID, Summary: in.Summary, Description: in.Description, Location: in.Location,
		Start: in.Start, End: in.End, AllDay: in.AllDay, Status: in.Status,
		Source: in.Source, SourceLabel: in.SourceLabel, Kind: in.Kind, Docs: in.Docs,
	}
}

func unmapCreateInput(in tooldeps.CalendarCreateInput) localcal.CreateInput {
	return localcal.CreateInput{
		Summary: in.Summary, Description: in.Description, Location: in.Location,
		Start: in.Start, End: in.End, AllDay: in.AllDay,
		Source: in.Source, SourceLabel: in.SourceLabel, Kind: in.Kind, Docs: in.Docs,
	}
}

// resolveToolLocalCalendar wraps the process-wide localcal store as a tooldeps
// LocalCalendar (DTO boundary). Typed-nil safe.
func resolveToolLocalCalendar(logger *slog.Logger) tooldeps.LocalCalendar {
	store, err := localcal.Default()
	if err != nil {
		if logger != nil {
			logger.Error("local calendar store unavailable — add/edit/delete disabled", "error", err)
		}
		return nil
	}
	return adaptLocalCalendar(store)
}

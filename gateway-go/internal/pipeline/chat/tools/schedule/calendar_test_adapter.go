package schedule

import (
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/localcal"
)

// wrapTestLocalCal projects *localcal.Store onto tooldeps.LocalCalendar for tests.
func wrapTestLocalCal(store *localcal.Store) tooldeps.LocalCalendar {
	if store == nil {
		return nil
	}
	return testLocalCalAdapter{inner: store}
}

type testLocalCalAdapter struct {
	inner *localcal.Store
}

func (a testLocalCalAdapter) ListRange(from, to time.Time) []tooldeps.CalendarEvent {
	return mapTestCalEvents(a.inner.ListRange(from, to))
}

func (a testLocalCalAdapter) Get(id string) *tooldeps.CalendarEvent {
	ev := a.inner.Get(id)
	if ev == nil {
		return nil
	}
	out := mapTestCalEvent(*ev)
	return &out
}

func (a testLocalCalAdapter) Create(in tooldeps.CalendarCreateInput) (tooldeps.CalendarEvent, error) {
	ev, err := a.inner.Create(localcal.CreateInput{
		Summary: in.Summary, Description: in.Description, Location: in.Location,
		Start: in.Start, End: in.End, AllDay: in.AllDay,
		Source: in.Source, SourceLabel: in.SourceLabel, Kind: in.Kind, Docs: in.Docs,
	})
	if err != nil {
		return tooldeps.CalendarEvent{}, err
	}
	return mapTestCalEvent(ev), nil
}

func (a testLocalCalAdapter) Update(id string, in tooldeps.CalendarCreateInput) (*tooldeps.CalendarEvent, error) {
	ev, err := a.inner.Update(id, localcal.CreateInput{
		Summary: in.Summary, Description: in.Description, Location: in.Location,
		Start: in.Start, End: in.End, AllDay: in.AllDay,
		Source: in.Source, SourceLabel: in.SourceLabel, Kind: in.Kind, Docs: in.Docs,
	})
	if err != nil {
		return nil, err
	}
	if ev == nil {
		return nil, nil
	}
	out := mapTestCalEvent(*ev)
	return &out, nil
}

func (a testLocalCalAdapter) Delete(id string) error { return a.inner.Delete(id) }

func mapTestCalEvents(in []calendar.Event) []tooldeps.CalendarEvent {
	out := make([]tooldeps.CalendarEvent, len(in))
	for i := range in {
		out[i] = mapTestCalEvent(in[i])
	}
	return out
}

func mapTestCalEvent(in calendar.Event) tooldeps.CalendarEvent {
	out := tooldeps.CalendarEvent{
		ID: in.ID, Summary: in.Summary, Description: in.Description, Location: in.Location,
		Start: in.Start, End: in.End, AllDay: in.AllDay, HTMLLink: in.HTMLLink, Status: in.Status,
		Organizer: tooldeps.CalendarAttendee{
			Email: in.Organizer.Email, DisplayName: in.Organizer.DisplayName,
			ResponseStatus: in.Organizer.ResponseStatus, Self: in.Organizer.Self, Organizer: in.Organizer.Organizer,
		},
		Source: in.Source, SourceLabel: in.SourceLabel, Kind: in.Kind, Docs: in.Docs,
	}
	if in.Conference != nil {
		out.Conference = &tooldeps.CalendarConference{Solution: in.Conference.Solution, URI: in.Conference.URI}
	}
	if len(in.Attendees) > 0 {
		out.Attendees = make([]tooldeps.CalendarAttendee, len(in.Attendees))
		for i, a := range in.Attendees {
			out.Attendees[i] = tooldeps.CalendarAttendee{
				Email: a.Email, DisplayName: a.DisplayName, ResponseStatus: a.ResponseStatus,
				Self: a.Self, Organizer: a.Organizer,
			}
		}
	}
	return out
}

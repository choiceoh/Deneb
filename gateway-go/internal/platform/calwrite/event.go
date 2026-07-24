package calwrite

import (
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
)

// localIDProperty is the Google extended-property key that tags a Google event as
// a mirror of a specific Deneb local event. It survives on the Google side so a
// future read-dedup (or a manual audit in Google Calendar) can tell which events
// Deneb authored, independent of the local id-map file.
const localIDProperty = "denebLocalId"

// googleTime is Calendar v3's start/end shape: a timed event carries dateTime
// (RFC3339), an all-day event carries date (YYYY-MM-DD) with an EXCLUSIVE end.
type googleTime struct {
	DateTime string `json:"dateTime,omitempty"`
	Date     string `json:"date,omitempty"`
}

// googleEvent is the minimal Calendar v3 event body Deneb writes. We only send
// fields the local store owns; Google fills the rest (id, etag, htmlLink).
type googleEvent struct {
	Summary            string         `json:"summary"`
	Description        string         `json:"description,omitempty"`
	Location           string         `json:"location,omitempty"`
	Start              googleTime     `json:"start"`
	End                googleTime     `json:"end"`
	ExtendedProperties *extendedProps `json:"extendedProperties,omitempty"`
}

type extendedProps struct {
	Private map[string]string `json:"private,omitempty"`
}

// toGoogleEvent maps a local calendar.Event to the Calendar v3 write body,
// tagging it with the local id so the pair is traceable on the Google side.
// All-day events use date-only bounds; the local store already stores an all-day
// end of start+1day, which is exactly Google's exclusive end for a one-day event.
func toGoogleEvent(localID string, ev calendar.Event) googleEvent {
	ge := googleEvent{
		Summary:     ev.Summary,
		Description: ev.Description,
		Location:    ev.Location,
	}
	if ev.AllDay {
		ge.Start = googleTime{Date: ev.Start.Format("2006-01-02")}
		ge.End = googleTime{Date: endDate(ev).Format("2006-01-02")}
	} else {
		ge.Start = googleTime{DateTime: ev.Start.Format(time.RFC3339)}
		ge.End = googleTime{DateTime: endDate(ev).Format(time.RFC3339)}
	}
	if localID != "" {
		ge.ExtendedProperties = &extendedProps{Private: map[string]string{localIDProperty: localID}}
	}
	return ge
}

// endDate returns a usable end instant: the event's own end when it is after
// start, else a sensible default (+1 day for all-day, +1 hour otherwise) so a
// malformed/zero end never produces an invalid Google range.
func endDate(ev calendar.Event) time.Time {
	if ev.End.After(ev.Start) {
		return ev.End
	}
	if ev.AllDay {
		return ev.Start.AddDate(0, 0, 1)
	}
	return ev.Start.Add(time.Hour)
}

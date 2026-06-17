// Package calendar implements a native Go client for the Google Calendar
// REST API. Mirrors the platform/gmail package: same OAuth refresh
// machinery, same atomic token persistence, same custom HTTP shape (no
// google.golang.org/api SDK to avoid build-time bloat).
package calendar

import "time"

// Event describes one Google Calendar event in the shape the Mini App
// cares about. The full Google response is much wider; we project down
// to keep payloads small and the parsing surface obvious.
type Event struct {
	ID          string
	Summary     string
	Description string
	Location    string
	Start       time.Time // local clock time of event start (in Location's tz)
	End         time.Time
	AllDay      bool            // true when Google returned date-only (no time)
	HTMLLink    string          // shareable web link
	Status      string          // "confirmed" / "tentative" / "cancelled"
	Organizer   Attendee        // empty if Google omitted it
	Attendees   []Attendee      // empty when nobody is invited
	Conference  *ConferenceInfo // nil when no Meet/Zoom is attached

	// Deneb annotations — empty for Google events and plain manual ones. They make
	// a locally-created event a first-class linked object so the agent can brief
	// and prep over it and follow it back to its origin. Source is a machine link
	// ("mail:<msgID>"), SourceLabel a human one (the mail subject), Kind the event
	// type ("meeting" | "deadline").
	Source      string
	SourceLabel string
	Kind        string
}

// Attendee is a calendar participant. Email is normalized to lowercase.
type Attendee struct {
	Email          string
	DisplayName    string
	ResponseStatus string // "needsAction" / "declined" / "tentative" / "accepted"
	Self           bool   // true when this attendee is the authenticated user
	Organizer      bool
}

// ConferenceInfo describes an attached video conference (Meet, etc.).
type ConferenceInfo struct {
	Solution string // "hangoutsMeet", "addOn", etc.
	URI      string // primary join URL
}

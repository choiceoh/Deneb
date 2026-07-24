// Package calwrite mirrors locally-authored calendar events out to the user's
// Google Calendar (one-way: Deneb → Google). It is the WRITE counterpart to the
// read-only platform/calendar client, kept a separate package on purpose: the
// calendar client's invariant is "works without a Google write scope", so its
// read surface must not grow POST/PATCH/DELETE. localcal (the local store) is
// the source of truth and must not import the Google API; the handler layer
// orchestrates the mirror by calling this package after a successful local write.
//
// The mirror is ON by default (the calendar token carries the full auth/calendar
// read+write scope): DefaultSyncer builds a syncer whenever calendar credentials
// are present, and returns an error — so the handler degrades to local-only,
// exactly as before — only when DENEB_CALENDAR_GOOGLE_WRITE=0 or the credentials
// are missing. Writes are best-effort: a Google outage logs a warning but never
// fails the local write.
package calwrite
